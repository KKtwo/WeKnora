package gitconnector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestConnectorFetchIncrementalDetectsCreateUpdateAndDelete(t *testing.T) {
	allowTestGitHost(t)
	runner := &fakeGitRunner{
		commit: "commit-a",
		files: map[string]fakeGitFile{
			"docs/a.md":   {blob: "blob-a1", content: "# A1"},
			"docs/b.md":   {blob: "blob-b1", content: "# B1"},
			"src/main.go": {blob: "blob-go1", content: "package main"},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url":           "ssh://git@example.test/team/docs.git",
		"branch":             "main",
		"path":               "docs",
		"include_extensions": ".md,.pdf",
	})

	first, cursor, err := connector.FetchIncremental(context.Background(), cfg, nil)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, itemIDs(first))
	require.NotNil(t, cursor)

	runner.commit = "commit-b"
	runner.files = map[string]fakeGitFile{
		"docs/a.md":  {blob: "blob-a2", content: "# A2"},
		"docs/c.pdf": {blob: "blob-c1", content: "%PDF-test"},
	}

	second, nextCursor, err := connector.FetchIncremental(context.Background(), cfg, cursor)
	require.NoError(t, err)
	require.Len(t, second, 3)
	require.Equal(t, "commit-b", nextCursor.LastSchemaHash)

	byID := make(map[string]types.FetchedItem, len(second))
	for _, item := range second {
		byID[item.ExternalID] = item
	}
	require.Equal(t, "# A2", string(byID["docs/a.md"].Content))
	require.Equal(t, "%PDF-test", string(byID["docs/c.pdf"].Content))
	require.True(t, byID["docs/b.md"].IsDeleted)
}

func TestConnectorLatestSelectorUsesNaturalVersionOrder(t *testing.T) {
	allowTestGitHost(t)
	runner := &fakeGitRunner{
		commit: "commit-versioned",
		files: map[string]fakeGitFile{
			"versions/version-9.8/guide.md":  {blob: "old", content: "old"},
			"versions/version-10.1/guide.md": {blob: "new", content: "new"},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url":       "https://example.test/team/docs.git",
		"branch":         "main",
		"latest_parent":  "versions",
		"latest_pattern": "version-*",
	})

	items, err := connector.FetchAll(context.Background(), cfg, nil)

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "versions/version-10.1/guide.md", items[0].ExternalID)
	require.Equal(t, "new", string(items[0].Content))
}

func TestConnectorRejectsUnsafeRepositoryURLsAndPaths(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	connector := newConnector(&fakeGitRunner{})

	for _, tc := range []struct {
		name     string
		settings map[string]interface{}
	}{
		{name: "local path", settings: map[string]interface{}{"repo_url": "/tmp/repo", "branch": "main"}},
		{name: "file URL", settings: map[string]interface{}{"repo_url": "file:///tmp/repo", "branch": "main"}},
		{name: "insecure HTTP", settings: map[string]interface{}{"repo_url": "http://example.test/repo.git", "branch": "main"}},
		{name: "embedded HTTPS credentials", settings: map[string]interface{}{"repo_url": "https://token@example.test/repo.git", "branch": "main"}},
		{name: "SSH password", settings: map[string]interface{}{"repo_url": "ssh://git:secret@example.test/repo.git", "branch": "main"}},
		{name: "branch option injection", settings: map[string]interface{}{"repo_url": "https://example.test/repo.git", "branch": "--upload-pack=evil"}},
		{name: "path escape", settings: map[string]interface{}{"repo_url": "https://example.test/repo.git", "branch": "main", "path": "../secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := connector.Validate(context.Background(), gitDataSourceConfig(tc.settings))
			require.Error(t, err)
		})
	}
}

func TestConnectorListsRepositoryTreeLazily(t *testing.T) {
	allowTestGitHost(t)
	runner := &fakeGitRunner{
		commit: "commit-tree",
		files: map[string]fakeGitFile{
			"docs/a.md":        {blob: "a", content: "A"},
			"docs/nested/b.md": {blob: "b", content: "B"},
			"README.md":        {blob: "readme", content: "readme"},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/docs.git",
		"branch":   "main",
	})

	root, err := connector.ListResources(context.Background(), cfg, "")
	require.NoError(t, err)
	require.Equal(t, []string{"README.md", "docs"}, resourceIDs(root))
	require.True(t, root[1].HasChildren)

	children, err := connector.ListResources(context.Background(), cfg, "docs")
	require.NoError(t, err)
	require.Equal(t, []string{"docs/a.md", "docs/nested"}, resourceIDs(children))
}

func TestConnectorHonorsConfiguredMaximumFileSize(t *testing.T) {
	allowTestGitHost(t)
	runner := &fakeGitRunner{
		commit: "commit-large",
		files: map[string]fakeGitFile{
			"docs/large.md": {blob: "large", content: "12345"},
		},
	}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url":       "https://example.test/team/docs.git",
		"branch":         "main",
		"max_file_bytes": 4,
	})

	_, err := connector.FetchAll(context.Background(), cfg, nil)

	require.ErrorContains(t, err, "exceeds maximum size")
}

func TestConnectorKeepsHTTPSCredentialsOutOfGitArguments(t *testing.T) {
	allowTestGitHost(t)
	runner := &fakeGitRunner{commit: "commit-auth"}
	connector := newConnector(runner)
	cfg := gitDataSourceConfig(map[string]interface{}{
		"repo_url": "https://example.test/team/private.git",
		"branch":   "main",
	})
	cfg.Credentials["username"] = "git-user"
	cfg.Credentials["token"] = "token-must-not-appear-in-arguments"

	require.NoError(t, connector.Validate(context.Background(), cfg))
	require.NotEmpty(t, runner.calls)

	call := runner.calls[len(runner.calls)-1]
	require.NotContains(t, strings.Join(call.args, " "), "token-must-not-appear-in-arguments")
	require.Contains(t, call.env, "GIT_ASKPASS=")
	require.Contains(t, call.env, "GIT_CONFIG_VALUE_0=false")
}

func gitDataSourceConfig(settings map[string]interface{}) *types.DataSourceConfig {
	return &types.DataSourceConfig{
		Type:        types.ConnectorTypeGit,
		Settings:    settings,
		Credentials: map[string]interface{}{"known_hosts": "example.test ssh-ed25519 AAAATEST"},
	}
}

func allowTestGitHost(t *testing.T) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "example.test")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
}

func itemIDs(items []types.FetchedItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ExternalID)
	}
	return out
}

func resourceIDs(items []types.Resource) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ExternalID)
	}
	return out
}

type fakeGitFile struct {
	blob    string
	content string
}

type fakeGitRunner struct {
	commit string
	files  map[string]fakeGitFile
	calls  []gitCall
}

type gitCall struct {
	env  string
	args []string
}

func (r *fakeGitRunner) Run(_ context.Context, env []string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, gitCall{
		env:  strings.Join(env, "\n"),
		args: append([]string(nil), args...),
	})
	joined := strings.Join(args, " ")
	switch {
	case len(args) == 1 && args[0] == "--version":
		return []byte("git version test"), nil
	case len(args) > 0 && args[0] == "ls-remote":
		return []byte(r.commit + "\trefs/heads/main\n"), nil
	case len(args) > 0 && args[0] == "clone":
		return nil, os.MkdirAll(args[len(args)-1], 0o755)
	case strings.Contains(joined, "rev-parse HEAD"):
		return []byte(r.commit + "\n"), nil
	case strings.Contains(joined, "ls-tree -r -z --full-tree HEAD"):
		var b strings.Builder
		paths := make([]string, 0, len(r.files))
		for path := range r.files {
			paths = append(paths, path)
		}
		sortStrings(paths)
		for _, path := range paths {
			file := r.files[path]
			fmt.Fprintf(&b, "100644 blob %s\t%s%c", file.blob, path, byte(0))
		}
		return []byte(b.String()), nil
	case strings.Contains(joined, "show HEAD:"):
		spec := args[len(args)-1]
		path := strings.TrimPrefix(spec, "HEAD:")
		file, ok := r.files[path]
		if !ok {
			return nil, fmt.Errorf("missing fake git file %s", filepath.Clean(path))
		}
		return []byte(file.content), nil
	default:
		return nil, fmt.Errorf("unexpected git command: %s", joined)
	}
}
