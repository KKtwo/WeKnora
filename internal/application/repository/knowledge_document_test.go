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

func TestCheckKnowledgeExistsOnlyMatchesCurrentDocumentVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:current_document_duplicate?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	first := &types.Knowledge{
		ID:              "knowledge-v1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		FileHash:        "hash-v1",
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), first))
	second := &types.Knowledge{
		ID:              "knowledge-v2",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		FileHash:        "hash-v2",
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), second))

	exists, _, err := repo.CheckKnowledgeExists(context.Background(), 1, "kb-1", &types.KnowledgeCheckParams{
		Type: "file", FileHash: "hash-v1",
	})
	require.NoError(t, err)
	require.False(t, exists)

	exists, current, err := repo.CheckKnowledgeExists(context.Background(), 1, "kb-1", &types.KnowledgeCheckParams{
		Type: "file", FileHash: "hash-v2",
	})
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "knowledge-v2", current.ID)
}

func TestKnowledgeListAndCountsOnlyIncludeCurrentDocumentVersions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:current_document_list?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	for _, knowledge := range []*types.Knowledge{
		{
			ID: "knowledge-v1", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
		},
		{
			ID: "knowledge-v2", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
		},
		{
			ID: "knowledge-b", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/b.md"}`),
		},
	} {
		require.NoError(t, repo.CreateKnowledge(context.Background(), knowledge))
	}

	items, total, err := repo.ListPagedKnowledgeByKnowledgeBaseID(
		context.Background(), 1, "kb-1", &types.Pagination{Page: 1, PageSize: 10},
		types.KnowledgeListFilter{},
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.ElementsMatch(t, []string{"knowledge-v2", "knowledge-b"}, []string{items[0].ID, items[1].ID})

	count, err := repo.CountKnowledgeByKnowledgeBaseID(context.Background(), 1, "kb-1")
	require.NoError(t, err)
	require.EqualValues(t, 2, count)

	completed, err := repo.CountKnowledgeByStatus(
		context.Background(), 1, "kb-1", []string{types.ParseStatusCompleted},
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, completed)
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
