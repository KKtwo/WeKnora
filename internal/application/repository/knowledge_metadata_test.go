package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFindByMetadataMatchesCompositeDataSourceIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge_metadata?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))

	for _, item := range []*types.Knowledge{
		{
			ID:              "knowledge-a",
			TenantID:        1,
			KnowledgeBaseID: "kb-1",
			Metadata:        types.JSON(`{"external_id":"docs/readme.md","datasource_id":"source-a"}`),
		},
		{
			ID:              "knowledge-b",
			TenantID:        1,
			KnowledgeBaseID: "kb-1",
			Metadata:        types.JSON(`{"external_id":"docs/readme.md","datasource_id":"source-b"}`),
		},
	} {
		require.NoError(t, db.Create(item).Error)
		require.NoError(t, db.Create(&types.Document{
			ID: item.ID, TenantID: 1, KnowledgeBaseID: "kb-1",
			ExternalKey: item.ID, CurrentKnowledgeID: item.ID,
		}).Error)
	}

	repo := NewKnowledgeRepository(db)
	found, err := repo.FindByMetadata(
		context.Background(),
		1,
		"kb-1",
		map[string]string{
			"datasource_id": "source-b",
			"external_id":   "docs/readme.md",
		},
	)

	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "knowledge-b", found.ID)
}

func TestFindByMetadataOnlyMatchesCurrentDocumentVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge_metadata_current?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))

	// 同一 (datasource_id, external_id) 的新旧两个世代同时存在且都未软删
	// （世代切换的中间态），同步删除/更新必须命中 current 世代，否则会删错行。
	for _, item := range []*types.Knowledge{
		{
			ID:              "knowledge-v1",
			TenantID:        1,
			KnowledgeBaseID: "kb-1",
			DocumentID:      "doc-1",
			Metadata:        types.JSON(`{"external_id":"docs/a.md","datasource_id":"source-1"}`),
		},
		{
			ID:              "knowledge-v2",
			TenantID:        1,
			KnowledgeBaseID: "kb-1",
			DocumentID:      "doc-1",
			Metadata:        types.JSON(`{"external_id":"docs/a.md","datasource_id":"source-1"}`),
		},
	} {
		require.NoError(t, db.Create(item).Error)
	}
	require.NoError(t, db.Create(&types.Document{
		ID: "doc-1", TenantID: 1, KnowledgeBaseID: "kb-1",
		ExternalKey: "docs/a.md", CurrentKnowledgeID: "knowledge-v2",
	}).Error)

	repo := NewKnowledgeRepository(db)
	found, err := repo.FindByMetadata(
		context.Background(),
		1,
		"kb-1",
		map[string]string{"datasource_id": "source-1", "external_id": "docs/a.md"},
	)

	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "knowledge-v2", found.ID)
}
