package dingtalk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

func fetchResources(ctx context.Context, api toolAPI, resourceIDs []string) ([]types.FetchedItem, error) {
	items := make([]types.FetchedItem, 0)
	seen := make(map[string]bool)
	for _, resourceID := range resourceIDs {
		fetched, err := fetchResource(ctx, api, resourceID, seen)
		if err != nil {
			return nil, fmt.Errorf("fetch dingtalk resource %s: %w", resourceID, err)
		}
		items = append(items, fetched...)
	}
	return items, nil
}

func fetchResource(
	ctx context.Context,
	api toolAPI,
	resourceID string,
	seen map[string]bool,
) ([]types.FetchedItem, error) {
	if seen[resourceID] {
		return nil, nil
	}
	seen[resourceID] = true

	switch {
	case strings.HasPrefix(resourceID, "folder:"), strings.HasPrefix(resourceID, "drive-folder:"):
		children, err := listChildren(ctx, api, resourceID)
		if err != nil {
			return nil, err
		}
		items := make([]types.FetchedItem, 0)
		for _, child := range children {
			fetched, err := fetchResource(ctx, api, child.ExternalID, seen)
			if err != nil {
				return nil, err
			}
			items = append(items, fetched...)
		}
		return items, nil
	case strings.HasPrefix(resourceID, "doc:"):
		return fetchDocument(ctx, api, strings.TrimPrefix(resourceID, "doc:"), resourceID)
	case strings.HasPrefix(resourceID, "sheet:"):
		return fetchSheet(ctx, api, strings.TrimPrefix(resourceID, "sheet:"), resourceID)
	case strings.HasPrefix(resourceID, "aitable:"):
		baseID := strings.TrimPrefix(resourceID, "aitable:")
		result, err := api.Call(ctx, "aitable", "get_tables", map[string]interface{}{"baseId": baseID})
		if err != nil {
			return nil, err
		}
		items := make([]types.FetchedItem, 0)
		for _, table := range tableResources(result, baseID, resourceID) {
			fetched, err := fetchResource(ctx, api, table.ExternalID, seen)
			if err != nil {
				return nil, err
			}
			items = append(items, fetched...)
		}
		return items, nil
	case strings.HasPrefix(resourceID, "aitable-table:"):
		parts := strings.SplitN(strings.TrimPrefix(resourceID, "aitable-table:"), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid AI table resource id")
		}
		return fetchAITable(ctx, api, parts[0], parts[1], resourceID)
	case strings.HasPrefix(resourceID, "drive-file:"):
		return fetchDriveFile(ctx, api, strings.TrimPrefix(resourceID, "drive-file:"), resourceID)
	default:
		return nil, fmt.Errorf("unsupported dingtalk resource id")
	}
}

func fetchDocument(ctx context.Context, api toolAPI, nodeID, externalID string) ([]types.FetchedItem, error) {
	result, err := api.Call(ctx, "doc", "get_document_content", map[string]interface{}{
		"nodeId": nodeID, "contentFormat": "markdown",
	})
	if err != nil {
		return nil, err
	}
	content := findText(result, "content", "markdown", "text")
	if content == "" {
		return nil, fmt.Errorf("document content is empty")
	}
	title := findText(result, "name", "title", "fileName")
	if title == "" {
		title = nodeID
	}
	sourceURL := findText(result, "url", "webUrl")
	if sourceURL == "" {
		sourceURL = "https://alidocs.dingtalk.com/i/nodes/" + nodeID
	}
	return []types.FetchedItem{newTextItem(externalID, title, content, sourceURL)}, nil
}

func fetchSheet(ctx context.Context, api toolAPI, nodeID, externalID string) ([]types.FetchedItem, error) {
	result, err := api.Call(ctx, "sheet", "get_all_sheets", map[string]interface{}{"nodeId": nodeID})
	if err != nil {
		return nil, err
	}
	sheets := findList(result, "sheets", "items", "list", "worksheets")
	sections := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		sheetID := firstString(sheet, "sheetId", "sheet_id", "id")
		if sheetID == "" {
			continue
		}
		csvResult, err := api.Call(ctx, "sheet", "get_range_as_csv", map[string]interface{}{
			"nodeId": nodeID, "sheetId": sheetID, "maxChars": 200000,
		})
		if err != nil {
			return nil, err
		}
		name := firstString(sheet, "title", "name", "sheetName")
		sections = append(sections, "## "+name+"\n\n"+findText(csvResult, "csv", "content", "text"))
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("spreadsheet has no readable worksheets")
	}
	return []types.FetchedItem{newTextItem(
		externalID,
		nodeID,
		strings.Join(sections, "\n\n"),
		"https://alidocs.dingtalk.com/i/nodes/"+nodeID,
	)}, nil
}

func fetchAITable(
	ctx context.Context,
	api toolAPI,
	baseID, tableID, externalID string,
) ([]types.FetchedItem, error) {
	args := map[string]interface{}{"baseId": baseID, "tableId": tableID, "limit": 100}
	records := make([]interface{}, 0)
	for page := 0; page < 100; page++ {
		result, err := api.Call(ctx, "aitable", "query_records", args)
		if err != nil {
			return nil, err
		}
		for _, record := range findList(result, "records", "items", "list") {
			records = append(records, record)
		}
		cursor := findText(result, "nextCursor", "next_cursor", "cursor")
		if cursor == "" {
			break
		}
		args["cursor"] = cursor
	}
	body, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return nil, err
	}
	return []types.FetchedItem{newTextItem(externalID, tableID, string(body), "")}, nil
}

func fetchDriveFile(ctx context.Context, api toolAPI, fileID, externalID string) ([]types.FetchedItem, error) {
	result, err := api.Call(ctx, "drive", "download_file", map[string]interface{}{"fileId": fileID})
	if err != nil {
		return nil, err
	}
	downloadURL := findDownloadURL(result)
	if downloadURL == "" {
		return nil, fmt.Errorf("download URL is empty")
	}
	if err := datasource.ValidateConnectorBaseURL(downloadURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range findStringMap(result, "headers") {
		req.Header.Set(key, value)
	}
	resp, err := datasource.NewConnectorHTTPClient(60 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024*1024))
	if err != nil {
		return nil, err
	}
	name := findText(result, "fileName", "name")
	if name == "" {
		if parsed, parseErr := url.Parse(downloadURL); parseErr == nil {
			name = path.Base(parsed.Path)
		}
	}
	sourceURL := "https://alidocs.dingtalk.com/i/nodes/" + fileID
	now := time.Now().UTC()
	return []types.FetchedItem{{
		ExternalID:  externalID,
		Title:       name,
		Content:     content,
		ContentType: resp.Header.Get("Content-Type"),
		FileName:    name,
		URL:         sourceURL,
		UpdatedAt:   now,
		Metadata:    sourceMetadata(externalID, sourceURL, contentDigest(content), now),
	}}, nil
}

func newTextItem(externalID, title, content, sourceURL string) types.FetchedItem {
	now := time.Now().UTC()
	return types.FetchedItem{
		ExternalID:  externalID,
		Title:       title,
		Content:     []byte(content),
		ContentType: "text/markdown",
		FileName:    safeName(title) + ".md",
		URL:         sourceURL,
		UpdatedAt:   now,
		Metadata:    sourceMetadata(externalID, sourceURL, contentDigest([]byte(content)), now),
	}
}

func sourceMetadata(externalID, sourceURL, revision string, updatedAt time.Time) map[string]string {
	return map[string]string{
		"datasource_type":   "dingtalk",
		"external_id":       externalID,
		"source_url":        sourceURL,
		"source_revision":   revision,
		"source_updated_at": updatedAt.Format(time.RFC3339),
	}
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func findText(result map[string]interface{}, keys ...string) string {
	containers := []map[string]interface{}{result}
	for _, wrapper := range []string{"data", "result"} {
		if nested, ok := result[wrapper].(map[string]interface{}); ok {
			containers = append(containers, nested)
		}
	}
	for _, container := range containers {
		if value := firstString(container, keys...); value != "" {
			return value
		}
	}
	return ""
}

// findDownloadURL accepts both documented DingTalk response shapes: a single
// URL string and an array whose first entry is the signed download URL.
func findDownloadURL(result map[string]interface{}) string {
	containers := []map[string]interface{}{result}
	for _, wrapper := range []string{"data", "result"} {
		if nested, ok := result[wrapper].(map[string]interface{}); ok {
			containers = append(containers, nested)
		}
	}
	for _, container := range containers {
		for _, key := range []string{"resourceUrl", "downloadUrl"} {
			switch value := container[key].(type) {
			case string:
				if value != "" {
					return value
				}
			case []interface{}:
				for _, item := range value {
					if text, ok := item.(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
}

func findStringMap(result map[string]interface{}, key string) map[string]string {
	for _, containerKey := range []string{"", "result", "data"} {
		container := result
		if containerKey != "" {
			container, _ = result[containerKey].(map[string]interface{})
		}
		if raw, ok := container[key].(map[string]interface{}); ok {
			values := make(map[string]string, len(raw))
			for name, value := range raw {
				values[name] = fmt.Sprint(value)
			}
			return values
		}
	}
	return map[string]string{}
}

func safeName(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" {
		return "dingtalk-document"
	}
	return name
}
