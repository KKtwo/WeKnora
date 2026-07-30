package gitconnector

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

var _ datasource.Connector = (*Connector)(nil)

type Connector struct {
	runner gitRunner
}

func NewConnector() *Connector {
	return newConnector(nativeGitRunner{})
}

func newConnector(runner gitRunner) *Connector {
	return &Connector{runner: runner}
}

func (c *Connector) Type() string {
	return types.ConnectorTypeGit
}

func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	if _, err := c.runner.Run(ctx, nil, "--version"); err != nil {
		return fmt.Errorf("git executable is unavailable: %w", err)
	}
	auth, err := newAuthSession(cfg)
	if err != nil {
		return err
	}
	defer auth.cleanup()
	if _, err := c.runner.Run(
		ctx,
		auth.env,
		"ls-remote",
		"--exit-code",
		"--heads",
		cfg.RepoURL,
		"refs/heads/"+cfg.Branch,
	); err != nil {
		return fmt.Errorf("validate git repository: %w", err)
	}
	return nil
}

func (c *Connector) ResolveResourceAncestors(
	_ context.Context,
	_ *types.DataSourceConfig,
	resourceIDs []string,
) ([]string, error) {
	seen := make(map[string]struct{})
	for _, resourceID := range resourceIDs {
		cleaned, err := normalizeRepoPath(resourceID)
		if err != nil {
			return nil, err
		}
		for parent := path.Dir(cleaned); parent != "." && parent != "/"; parent = path.Dir(parent) {
			seen[parent] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Connector) ListResources(
	ctx context.Context,
	config *types.DataSourceConfig,
	parentID string,
) ([]types.Resource, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	state, err := c.openRepository(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer state.checkout.cleanup()

	parent, err := normalizeRepoPath(parentID)
	if err != nil {
		return nil, err
	}
	if parentID == "" {
		parent = "."
	}
	return resourcesAt(parent, state.files, cfg.IncludeExtensions), nil
}

func (c *Connector) FetchAll(
	ctx context.Context,
	config *types.DataSourceConfig,
	resourceIDs []string,
) ([]types.FetchedItem, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	state, err := c.openRepository(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer state.checkout.cleanup()

	files, selectedRoot, err := selectFiles(cfg, resourceIDs, state.files)
	if err != nil {
		return nil, err
	}
	items, _, err := c.materializeItems(ctx, state, files, selectedRoot, cfg.MaxFileBytes)
	return items, err
}

func (c *Connector) FetchIncremental(
	ctx context.Context,
	config *types.DataSourceConfig,
	cursor *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, nil, err
	}
	state, err := c.openRepository(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	defer state.checkout.cleanup()

	currentFiles, selectedRoot, err := selectFiles(cfg, config.ResourceIDs, state.files)
	if err != nil {
		return nil, nil, err
	}
	previous := decodeCursor(cursor)

	changed := make(map[string]string)
	if previous == nil {
		for filePath, blob := range currentFiles {
			changed[filePath] = blob
		}
	} else {
		for filePath, blob := range currentFiles {
			_, dependenciesRecorded := previous.ImageDependencies[filePath]
			if previous.Files[filePath] != blob ||
				(isMarkdownPath(filePath) && (!dependenciesRecorded ||
					imageDependenciesChanged(previous.ImageDependencies[filePath], state.files))) {
				changed[filePath] = blob
			}
		}
	}
	items, changedDependencies, err := c.materializeItems(
		ctx, state, changed, selectedRoot, cfg.MaxFileBytes,
	)
	if err != nil {
		return nil, nil, err
	}
	currentDependencies := make(map[string]map[string]string)
	if previous != nil {
		for filePath, dependencies := range previous.ImageDependencies {
			if _, exists := currentFiles[filePath]; exists {
				currentDependencies[filePath] = dependencies
			}
		}
	}
	for filePath, dependencies := range changedDependencies {
		currentDependencies[filePath] = dependencies
	}

	if previous != nil {
		deletedPaths := make([]string, 0)
		for filePath := range previous.Files {
			if _, exists := currentFiles[filePath]; !exists {
				deletedPaths = append(deletedPaths, filePath)
			}
		}
		sort.Strings(deletedPaths)
		for _, filePath := range deletedPaths {
			items = append(items, types.FetchedItem{
				ExternalID: filePath,
				Title:      strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)),
				FileName:   path.Base(filePath),
				IsDeleted:  true,
				Metadata: map[string]string{
					"git_path": filePath,
				},
			})
		}
	}

	nextCursor := encodeCursor(gitCursor{
		Commit:            state.commit,
		SelectedRoot:      selectedRoot,
		Files:             currentFiles,
		ImageDependencies: currentDependencies,
	})
	nextCursor.LastSyncTime = time.Now().UTC()
	return items, nextCursor, nil
}

type repositoryState struct {
	checkout *gitCheckout
	commit   string
	files    map[string]string
}

func (c *Connector) openRepository(ctx context.Context, cfg *Config) (*repositoryState, error) {
	checkout, err := cloneRepository(ctx, c.runner, cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*repositoryState, error) {
		checkout.cleanup()
		return nil, err
	}

	commitBytes, err := c.runner.Run(ctx, checkout.env, "-C", checkout.dir, "rev-parse", "HEAD")
	if err != nil {
		return fail(fmt.Errorf("resolve repository commit: %w", err))
	}
	tree, err := c.runner.Run(
		ctx,
		checkout.env,
		"-C", checkout.dir,
		"ls-tree", "-r", "-z", "--full-tree", "HEAD",
	)
	if err != nil {
		return fail(fmt.Errorf("list repository tree: %w", err))
	}
	files, err := parseTree(tree)
	if err != nil {
		return fail(err)
	}
	return &repositoryState{
		checkout: checkout,
		commit:   strings.TrimSpace(string(commitBytes)),
		files:    files,
	}, nil
}

func parseTree(raw []byte) (map[string]string, error) {
	files := make(map[string]string)
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		meta, filePath, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("invalid git tree record")
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		cleaned, err := normalizeRepoPath(filePath)
		if err != nil || cleaned == "." {
			return nil, fmt.Errorf("invalid path in git tree: %q", filePath)
		}
		files[cleaned] = fields[2]
	}
	return files, nil
}

func selectFiles(
	cfg *Config,
	resourceIDs []string,
	allFiles map[string]string,
) (map[string]string, string, error) {
	roots := make([]string, 0)
	selectedRoot := "."
	if len(resourceIDs) > 0 {
		for _, resourceID := range resourceIDs {
			root, err := normalizeRepoPath(resourceID)
			if err != nil {
				return nil, "", err
			}
			roots = append(roots, root)
		}
	} else if cfg.LatestParent != "" {
		root, err := resolveLatestRoot(cfg.LatestParent, cfg.LatestPattern, allFiles)
		if err != nil {
			return nil, "", err
		}
		roots = append(roots, root)
		selectedRoot = root
	} else if cfg.Path != "" {
		root, err := normalizeRepoPath(cfg.Path)
		if err != nil {
			return nil, "", err
		}
		roots = append(roots, root)
		selectedRoot = root
	} else {
		roots = append(roots, ".")
	}

	selected := make(map[string]string)
	for filePath, blob := range allFiles {
		if !hasAllowedExtension(filePath, cfg.IncludeExtensions) {
			continue
		}
		// 点开头的路径段是隐藏文件/工具伴生文件（如 .github/、
		// ".项目档案.overview.md" 摘要），不属于知识正文，与旧链路规则对齐。
		if hasHiddenPathSegment(filePath) {
			continue
		}
		for _, root := range roots {
			if pathWithinRoot(filePath, root) {
				selected[filePath] = blob
				break
			}
		}
	}
	return selected, selectedRoot, nil
}

func hasHiddenPathSegment(filePath string) bool {
	for _, segment := range strings.Split(filePath, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func resolveLatestRoot(parent, pattern string, files map[string]string) (string, error) {
	parent, err := normalizeRepoPath(parent)
	if err != nil {
		return "", err
	}
	candidates := make(map[string]struct{})
	prefix := ""
	if parent != "." {
		prefix = parent + "/"
	}
	for filePath := range files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(filePath, prefix)
		name, _, _ := strings.Cut(remainder, "/")
		matched, matchErr := path.Match(pattern, name)
		if matchErr != nil {
			return "", matchErr
		}
		if matched {
			candidates[name] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no Git directory matches %s/%s", parent, pattern)
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	if parent == "." {
		return names[len(names)-1], nil
	}
	return path.Join(parent, names[len(names)-1]), nil
}

func (c *Connector) materializeItems(
	ctx context.Context,
	state *repositoryState,
	files map[string]string,
	selectedRoot string,
	maxFileBytes int64,
) ([]types.FetchedItem, map[string]map[string]string, error) {
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)

	items := make([]types.FetchedItem, 0, len(paths))
	imageDependencies := make(map[string]map[string]string)
	for _, filePath := range paths {
		content, err := c.runner.Run(
			ctx,
			state.checkout.env,
			"-C", state.checkout.dir,
			"show", "HEAD:"+filePath,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("read Git file %s: %w", filePath, err)
		}
		if int64(len(content)) > maxFileBytes {
			return nil, nil, fmt.Errorf("Git file exceeds maximum size: %s", filePath)
		}
		if isMarkdownPath(filePath) {
			content, imageDependencies[filePath], err = c.embedMarkdownImages(
				ctx, state, filePath, content, maxFileBytes,
			)
			if err != nil {
				return nil, nil, err
			}
		}
		items = append(items, types.FetchedItem{
			ExternalID: filePath,
			Title:      strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)),
			Content:    content,
			ContentType: utils.GetContentTypeByExt(
				path.Ext(filePath),
			),
			FileName:         path.Base(filePath),
			SourceResourceID: selectedRoot,
			Metadata: map[string]string{
				"git_blob":   files[filePath],
				"git_commit": state.commit,
				"git_path":   filePath,
			},
		})
	}
	return items, imageDependencies, nil
}

func resourcesAt(
	parent string,
	files map[string]string,
	extensions map[string]struct{},
) []types.Resource {
	children := make(map[string]types.Resource)
	for filePath := range files {
		if !hasAllowedExtension(filePath, extensions) || !pathWithinRoot(filePath, parent) {
			continue
		}
		remainder := filePath
		if parent != "." {
			remainder = strings.TrimPrefix(filePath, parent+"/")
		}
		if remainder == filePath && parent != "." {
			continue
		}
		name, _, hasTail := strings.Cut(remainder, "/")
		childPath := name
		if parent != "." {
			childPath = path.Join(parent, name)
		}
		if hasTail {
			children[childPath] = types.Resource{
				ExternalID:  childPath,
				Name:        name,
				Type:        "directory",
				ParentID:    resourceParentID(parent),
				HasChildren: true,
			}
			continue
		}
		children[childPath] = types.Resource{
			ExternalID: childPath,
			Name:       name,
			Type:       "file",
			ParentID:   resourceParentID(parent),
		}
	}
	ids := make([]string, 0, len(children))
	for id := range children {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]types.Resource, 0, len(ids))
	for _, id := range ids {
		out = append(out, children[id])
	}
	return out
}

func resourceParentID(parent string) string {
	if parent == "." {
		return ""
	}
	return parent
}

func pathWithinRoot(filePath, root string) bool {
	return root == "." || filePath == root || strings.HasPrefix(filePath, root+"/")
}

func hasAllowedExtension(filePath string, extensions map[string]struct{}) bool {
	_, ok := extensions[strings.ToLower(path.Ext(filePath))]
	return ok
}
