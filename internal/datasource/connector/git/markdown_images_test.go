package gitconnector

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorEmbedsDocumentRelativeAndRepositoryRootImages(t *testing.T) {
	allowTestGitHost(t)
	relativeImage := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3})
	// The real help-doc repository contains PNG bytes under some .jpeg paths.
	rootImage := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 4, 5, 6})
	runner := &fakeGitRunner{
		commit: "commit-images",
		files: map[string]fakeGitFile{
			"chatbi_versioned_docs/version-1.0.0/01-产品介绍/01-产品简介.md": {
				blob: "blob-doc",
				content: "![相对图](images/detail.png \"明细\")\n" +
					"![产品架构](img/chatbi/architecture.jpeg)\n" +
					"![远程图](https://example.test/remote.png)",
			},
			"chatbi_versioned_docs/version-1.0.0/01-产品介绍/images/detail.png": {
				blob:    "blob-detail",
				content: relativeImage,
			},
			"img/chatbi/architecture.jpeg": {
				blob:    "blob-architecture",
				content: rootImage,
			},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
		"path":     "chatbi_versioned_docs/version-1.0.0",
	})

	items, err := connector.FetchAll(context.Background(), cfg, nil)

	require.NoError(t, err)
	require.Len(t, items, 1)
	content := string(items[0].Content)
	require.Contains(t, content, gitImageDataURI(
		"image/png",
		"chatbi_versioned_docs/version-1.0.0/01-产品介绍/images/detail.png",
		[]byte(relativeImage),
	))
	require.Contains(t, content, ` "明细")`)
	require.Contains(t, content, gitImageDataURI(
		"image/png",
		"img/chatbi/architecture.jpeg",
		[]byte(rootImage),
	))
	require.Contains(t, content, "![远程图](https://example.test/remote.png)")
}

func TestConnectorLeavesMarkdownExamplesUntouched(t *testing.T) {
	allowTestGitHost(t)
	image := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1})
	runner := &fakeGitRunner{
		commit: "commit-code",
		files: map[string]fakeGitFile{
			"docs/guide.md": {
				blob: "doc",
				content: "```markdown\n<!-- ![围栏示例](images/example.png)\n```\n\n" +
					"`<!-- ![行内示例](images/example.png)`\n\n" +
					"<!-- ![注释示例](images/example.png) -->\n\n" +
					"    ![缩进示例](images/example.png)\n\n" +
					"> ```markdown\n> ![引用块示例](images/example.png)\n> ```\n\n" +
					"- ```markdown\n  ![列表示例](images/example.png)\n  ```\n\n" +
					"![真实图](images/real.png)\n",
			},
			"docs/images/real.png":    {blob: "real", content: image},
			"docs/images/example.png": {blob: "example", content: image},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
		"path":     "docs",
	})

	items, err := connector.FetchAll(context.Background(), cfg, nil)

	require.NoError(t, err)
	require.Len(t, items, 1)
	content := string(items[0].Content)
	require.Equal(t, 1, strings.Count(content, "data:image/"))
	for _, example := range []string{
		"![围栏示例](images/example.png)",
		"![行内示例](images/example.png)",
		"![注释示例](images/example.png)",
		"![缩进示例](images/example.png)",
		"![引用块示例](images/example.png)",
		"![列表示例](images/example.png)",
	} {
		require.Contains(t, content, example)
	}
}

func TestConnectorRecordsDependenciesForEveryEmbeddedImageAtLimit(t *testing.T) {
	allowTestGitHost(t)
	imageA := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'a'})
	imageB := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'b'})
	var markdown strings.Builder
	markdown.WriteString("![真实图](images/real.png)\n")
	for index := 0; index < maxGitImagesPerDocument; index++ {
		markdown.WriteString("![缺失图](images/missing-")
		markdown.WriteString(string(rune('a' + index)))
		markdown.WriteString(".png)\n")
	}
	runner := &fakeGitRunner{
		commit: "commit-limit-a",
		files: map[string]fakeGitFile{
			"docs/guide.md":        {blob: "doc", content: markdown.String()},
			"docs/images/real.png": {blob: "real-a", content: imageA},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
		"path":     "docs",
	})

	first, cursor, err := connector.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Contains(t, string(first[0].Content), base64.StdEncoding.EncodeToString([]byte(imageA)))

	runner.commit = "commit-limit-b"
	runner.files["docs/images/real.png"] = fakeGitFile{blob: "real-b", content: imageB}
	second, _, err := connector.FetchIncremental(context.Background(), cfg, cursor)

	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Contains(t, string(second[0].Content), base64.StdEncoding.EncodeToString([]byte(imageB)))
}

func TestConnectorBoundsRejectedImageIO(t *testing.T) {
	allowTestGitHost(t)
	refs := strings.Repeat("![超大](images/large.png)\n", 15) +
		strings.Repeat("![伪图片](images/not-image.png)\n", 15)
	runner := &fakeGitRunner{
		commit: "commit-bounds",
		files: map[string]fakeGitFile{
			"docs/guide.md":             {blob: "doc", content: refs},
			"docs/images/large.png":     {blob: "large", size: maxGitImageBytes + 1},
			"docs/images/not-image.png": {blob: "not-image", content: "plain text"},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
		"path":     "docs",
	})

	items, err := connector.FetchAll(context.Background(), cfg, nil)

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotContains(t, string(items[0].Content), "data:image/")
	require.Equal(t, 1, countGitCalls(runner.calls, "cat-file -s large"))
	require.Equal(t, 0, countGitCalls(runner.calls, "show HEAD:docs/images/large.png"))
	require.Equal(t, 1, countGitCalls(runner.calls, "cat-file -s not-image"))
	require.Equal(t, 1, countGitCalls(runner.calls, "show HEAD:docs/images/not-image.png"))
}

func TestGitImageDataURIRejectsSourcePathsOutsideResolverContract(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G'}
	require.Empty(t, gitImageDataURI("image/png", "foo:bar.png", image))
	require.Empty(t, gitImageDataURI(
		"image/png",
		strings.Repeat("a", maxGitImageSourceBytes+1)+".png",
		image,
	))
}

func TestConnectorRefetchesMarkdownWhenReferencedImageChanges(t *testing.T) {
	allowTestGitHost(t)
	const documentPath = "docs/guide.md"
	const imagePath = "docs/images/architecture.png"
	imageA := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'a'})
	imageB := string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'b'})
	runner := &fakeGitRunner{
		commit: "commit-a",
		files: map[string]fakeGitFile{
			documentPath: {blob: "blob-doc", content: "![架构](images/architecture.png)"},
			imagePath:    {blob: "blob-image-a", content: imageA},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
		"path":     "docs",
	})

	legacyCursor := encodeCursor(gitCursor{
		Commit: "commit-a",
		Files:  map[string]string{documentPath: "blob-doc"},
	})
	backfill, _, err := connector.FetchIncremental(context.Background(), cfg, legacyCursor)
	require.NoError(t, err)
	require.Len(t, backfill, 1, "legacy cursors must reprocess Markdown to record image dependencies")

	first, cursor, err := connector.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)
	require.Len(t, first, 1)

	runner.commit = "commit-b"
	runner.files[imagePath] = fakeGitFile{blob: "blob-image-b", content: imageB}
	second, nextCursor, err := connector.FetchIncremental(context.Background(), cfg, cursor)

	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, documentPath, second[0].ExternalID)
	require.Contains(t, string(second[0].Content), base64.StdEncoding.EncodeToString([]byte(imageB)))

	delete(runner.files, imagePath)
	runner.commit = "commit-c"
	third, missingCursor, err := connector.FetchIncremental(context.Background(), cfg, nextCursor)

	require.NoError(t, err)
	require.Len(t, third, 1)
	require.Contains(t, string(third[0].Content), "![架构](images/architecture.png)")
	require.NotContains(t, string(third[0].Content), "data:image/")

	runner.files[imagePath] = fakeGitFile{blob: "blob-image-d", content: imageA}
	runner.commit = "commit-d"
	fourth, _, err := connector.FetchIncremental(context.Background(), cfg, missingCursor)

	require.NoError(t, err)
	require.Len(t, fourth, 1)
	require.Contains(t, string(fourth[0].Content), base64.StdEncoding.EncodeToString([]byte(imageA)))
}

func countGitCalls(calls []gitCall, fragment string) int {
	count := 0
	for _, call := range calls {
		if strings.Contains(strings.Join(call.args, " "), fragment) {
			count++
		}
	}
	return count
}
