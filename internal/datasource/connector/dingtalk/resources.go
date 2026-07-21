package dingtalk

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func documentResources(result map[string]interface{}, parentID string) []types.Resource {
	items := findList(result, "documents", "nodes", "items", "list")
	resources := make([]types.Resource, 0, len(items))
	for _, item := range items {
		id := firstString(item, "nodeId", "node_id", "id", "docId")
		if id == "" {
			continue
		}
		name := firstString(item, "name", "title", "docName", "fileName")
		docType := strings.ToLower(firstString(item, "docType", "nodeType", "type", "extension"))
		prefix, resourceType, children := "doc:", "document", false
		if strings.Contains(docType, "folder") {
			prefix, resourceType, children = "folder:", "folder", true
		} else if strings.Contains(docType, "sheet") || strings.Contains(docType, "axls") {
			prefix, resourceType = "sheet:", "sheet"
		}
		resources = append(resources, types.Resource{
			ExternalID:  prefix + id,
			Name:        name,
			Type:        resourceType,
			URL:         firstString(item, "url", "docUrl", "nodeUrl", "webUrl"),
			ParentID:    parentID,
			HasChildren: children,
			Metadata:    map[string]interface{}{"node_id": id, "doc_type": docType},
		})
	}
	return resources
}

func baseResources(result map[string]interface{}) []types.Resource {
	items := findList(result, "bases", "items", "list")
	resources := make([]types.Resource, 0, len(items))
	for _, item := range items {
		id := firstString(item, "baseId", "base_id", "id")
		if id == "" {
			continue
		}
		resources = append(resources, types.Resource{
			ExternalID:  "aitable:" + id,
			Name:        firstString(item, "baseName", "base_name", "name", "title"),
			Type:        "ai_table_base",
			HasChildren: true,
			Metadata:    map[string]interface{}{"base_id": id},
		})
	}
	return resources
}

func tableResources(result map[string]interface{}, baseID, parentID string) []types.Resource {
	items := findList(result, "tables", "items", "list")
	resources := make([]types.Resource, 0, len(items))
	for _, item := range items {
		id := firstString(item, "tableId", "table_id", "id")
		if id == "" {
			continue
		}
		resources = append(resources, types.Resource{
			ExternalID: "aitable-table:" + baseID + ":" + id,
			Name:       firstString(item, "tableName", "table_name", "name", "title"),
			Type:       "ai_table",
			ParentID:   parentID,
			Metadata:   map[string]interface{}{"base_id": baseID, "table_id": id},
		})
	}
	return resources
}

func driveResources(result map[string]interface{}, parentID string) []types.Resource {
	items := findList(result, "files", "dentries", "entries", "nodes", "items", "list")
	resources := make([]types.Resource, 0, len(items))
	for _, item := range items {
		id := firstString(item, "dentryId", "dentryUuid", "id", "fileId", "nodeId")
		if id == "" {
			continue
		}
		kind := strings.ToLower(firstString(item, "type", "dentryType", "fileType"))
		isFolder := strings.Contains(kind, "folder")
		prefix, resourceType := "drive-file:", "file"
		if isFolder {
			prefix, resourceType = "drive-folder:", "folder"
		}
		resources = append(resources, types.Resource{
			ExternalID:  prefix + id,
			Name:        firstString(item, "name", "fileName", "dentryName", "title"),
			Type:        resourceType,
			URL:         firstString(item, "url", "webUrl"),
			ParentID:    parentID,
			HasChildren: isFolder,
			Metadata: map[string]interface{}{
				"file_id":  id,
				"space_id": firstString(item, "spaceId", "space_id"),
			},
		})
	}
	return resources
}

func findList(result map[string]interface{}, keys ...string) []map[string]interface{} {
	if result == nil {
		return nil
	}
	containers := []map[string]interface{}{result}
	for _, wrapper := range []string{"data", "result"} {
		if nested, ok := result[wrapper].(map[string]interface{}); ok {
			containers = append(containers, nested)
		}
	}
	for _, container := range containers {
		for _, key := range keys {
			if raw, ok := container[key].([]interface{}); ok {
				items := make([]map[string]interface{}, 0, len(raw))
				for _, value := range raw {
					if item, ok := value.(map[string]interface{}); ok {
						items = append(items, item)
					}
				}
				return items
			}
		}
	}
	return nil
}

func firstString(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func parseTime(item map[string]interface{}) time.Time {
	raw := firstString(item, "modifiedTime", "modifyTime", "updateTime", "gmtModified")
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	return time.Time{}
}
