package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateKnowledgeKeepsStableDocumentIDForDatasourceItem(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:stable_document?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	first := &types.Knowledge{
		ID:              "knowledge-v1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), first))
	require.NotEmpty(t, first.DocumentID)

	second := &types.Knowledge{
		ID:              "knowledge-v2",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), second))
	require.Equal(t, first.DocumentID, second.DocumentID)

	var documents []types.Document
	require.NoError(t, db.Find(&documents).Error)
	require.Len(t, documents, 1)
	require.Equal(t, "knowledge-v2", documents[0].CurrentKnowledgeID)
}

func TestResolveCurrentKnowledgeIDsIsExplicitlyKnowledgeBaseScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:document_scope?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Document{}))
	require.NoError(t, db.Create(&types.Document{
		ID: "doc-1", TenantID: 1, KnowledgeBaseID: "kb-1", ExternalKey: "a", CurrentKnowledgeID: "k-1",
	}).Error)
	require.NoError(t, db.Create(&types.Document{
		ID: "doc-2", TenantID: 1, KnowledgeBaseID: "kb-2", ExternalKey: "b", CurrentKnowledgeID: "k-2",
	}).Error)

	repo := NewKnowledgeBaseRepository(db)
	ids, err := repo.(*knowledgeBaseRepository).ResolveCurrentKnowledgeIDs(
		context.Background(), 1, []string{"kb-1"}, []string{"doc-1", "doc-2"},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"k-1"}, ids)

	empty, err := repo.(*knowledgeBaseRepository).ResolveCurrentKnowledgeIDs(
		context.Background(), 1, nil, []string{"doc-1"},
	)
	require.NoError(t, err)
	require.Empty(t, empty)
}
