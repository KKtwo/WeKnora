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
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))

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
