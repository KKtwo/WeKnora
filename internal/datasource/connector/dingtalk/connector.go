package dingtalk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type Connector struct {
	newAPI func(*Config) toolAPI
}

func NewConnector() *Connector {
	return &Connector{newAPI: newMCPToolAPI}
}

func (c *Connector) Type() string { return types.ConnectorTypeDingTalk }

func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	parsed, err := parseConfig(config)
	if err != nil {
		return err
	}
	api := c.newAPI(parsed)
	defer api.Close()
	_, err = api.Call(ctx, "doc", "search_documents", map[string]interface{}{"pageSize": 1})
	if err != nil {
		return fmt.Errorf("dingtalk personal OAuth validation failed: %w", err)
	}
	return nil
}

func (c *Connector) ListResources(
	ctx context.Context,
	config *types.DataSourceConfig,
	parentID string,
) ([]types.Resource, error) {
	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	api := c.newAPI(parsed)
	defer api.Close()
	if parentID != "" {
		return listChildren(ctx, api, parentID)
	}

	resources := make([]types.Resource, 0)
	docResult, err := api.Call(ctx, "doc", "search_documents", map[string]interface{}{"pageSize": 30})
	if err != nil {
		return nil, fmt.Errorf("list dingtalk documents: %w", err)
	}
	resources = append(resources, documentResources(docResult, "")...)
	baseResult, err := api.Call(ctx, "aitable", "list_bases", map[string]interface{}{"limit": 10})
	if err != nil {
		return nil, fmt.Errorf("list dingtalk AI tables: %w", err)
	}
	resources = append(resources, baseResources(baseResult)...)
	driveResult, err := api.Call(ctx, "drive", "list_files", map[string]interface{}{"maxResults": 50})
	if err != nil {
		return nil, fmt.Errorf("list dingtalk drive: %w", err)
	}
	resources = append(resources, driveResources(driveResult, "")...)
	return resources, nil
}

func (c *Connector) ResolveResourceAncestors(
	context.Context,
	*types.DataSourceConfig,
	[]string,
) ([]string, error) {
	// DingTalk MCP does not expose a uniform parent lookup across doc, drive and
	// AI table products. The picker still supports lazy traversal from roots.
	return []string{}, nil
}

func (c *Connector) FetchAll(
	ctx context.Context,
	config *types.DataSourceConfig,
	resourceIDs []string,
) ([]types.FetchedItem, error) {
	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	api := c.newAPI(parsed)
	defer api.Close()
	return fetchResources(ctx, api, resourceIDs)
}

func (c *Connector) FetchIncremental(
	ctx context.Context,
	config *types.DataSourceConfig,
	cursor *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	items, err := c.FetchAll(ctx, config, config.ResourceIDs)
	if err != nil {
		return nil, nil, err
	}
	previous := cursorState{Hashes: map[string]string{}}
	if cursor != nil && cursor.ConnectorCursor != nil {
		body, _ := json.Marshal(cursor.ConnectorCursor)
		_ = json.Unmarshal(body, &previous)
	}
	if previous.Hashes == nil {
		previous.Hashes = map[string]string{}
	}

	nextHashes := make(map[string]string, len(items))
	changed := make([]types.FetchedItem, 0, len(items))
	for _, item := range items {
		hash := hashItem(item)
		nextHashes[item.ExternalID] = hash
		if previous.Hashes[item.ExternalID] != hash {
			changed = append(changed, item)
		}
	}
	for externalID := range previous.Hashes {
		if _, exists := nextHashes[externalID]; !exists {
			changed = append(changed, types.FetchedItem{
				ExternalID: externalID,
				Title:      externalID,
				IsDeleted:  true,
			})
		}
	}
	nextMap := map[string]interface{}{}
	body, _ := json.Marshal(cursorState{Hashes: nextHashes})
	_ = json.Unmarshal(body, &nextMap)
	return changed, &types.SyncCursor{
		LastSyncTime:    time.Now().UTC(),
		ConnectorCursor: nextMap,
	}, nil
}

func hashItem(item types.FetchedItem) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(item.ExternalID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(item.Content)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(item.URL))
	return hex.EncodeToString(hash.Sum(nil))
}

func listChildren(ctx context.Context, api toolAPI, parentID string) ([]types.Resource, error) {
	switch {
	case strings.HasPrefix(parentID, "folder:"):
		id := strings.TrimPrefix(parentID, "folder:")
		result, err := api.Call(ctx, "doc", "list_nodes", map[string]interface{}{"folderId": id, "pageSize": 50})
		return documentResources(result, parentID), err
	case strings.HasPrefix(parentID, "drive-folder:"):
		id := strings.TrimPrefix(parentID, "drive-folder:")
		result, err := api.Call(ctx, "drive", "list_files", map[string]interface{}{"parentId": id, "maxResults": 50})
		return driveResources(result, parentID), err
	case strings.HasPrefix(parentID, "aitable:"):
		baseID := strings.TrimPrefix(parentID, "aitable:")
		result, err := api.Call(ctx, "aitable", "get_tables", map[string]interface{}{"baseId": baseID})
		return tableResources(result, baseID, parentID), err
	default:
		return []types.Resource{}, nil
	}
}
