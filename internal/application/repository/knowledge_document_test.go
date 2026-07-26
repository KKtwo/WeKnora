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

func TestDeleteKnowledgeSoftDeletesDocumentRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:delete_document_row?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	knowledge := &types.Knowledge{
		ID:              "knowledge-v1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), knowledge))

	require.NoError(t, repo.DeleteKnowledge(context.Background(), 1, "knowledge-v1"))

	// 最后一个内容世代删除后，逻辑文档行必须一并软删，
	// 否则默认查询仍能解析到一个没有可读内容的“幽灵文档”。
	var live []types.Document
	require.NoError(t, db.Find(&live).Error)
	require.Empty(t, live)
	var all []types.Document
	require.NoError(t, db.Unscoped().Find(&all).Error)
	require.Len(t, all, 1)
	require.True(t, all[0].DeletedAt.Valid)
}

func TestCreateKnowledgeRevivesSoftDeletedDocumentOnReupsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:revive_document_row?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	v1 := &types.Knowledge{
		ID:              "knowledge-v1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		Metadata:        types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), v1))

	// 同步器的“文档更新”是先删旧世代再以同一稳定 document_id 重建新世代。
	require.NoError(t, repo.DeleteKnowledge(context.Background(), 1, "knowledge-v1"))

	v2 := &types.Knowledge{
		ID:              "knowledge-v2",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusCompleted,
		Metadata: types.JSON(
			`{"datasource_id":"source-1","external_id":"docs/a.md","document_id":"` + v1.DocumentID + `"}`,
		),
	}
	require.NoError(t, repo.CreateKnowledge(context.Background(), v2))

	// 文档行必须复活并指向新世代，否则检索返回的 document_id 无法解析（读取 404）。
	var live []types.Document
	require.NoError(t, db.Find(&live).Error)
	require.Len(t, live, 1)
	require.Equal(t, v1.DocumentID, live[0].ID)
	require.Equal(t, "knowledge-v2", live[0].CurrentKnowledgeID)
}

func TestDeleteKnowledgeListSoftDeletesCurrentDocumentRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:delete_document_rows_batch?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Document{}))
	repo := NewKnowledgeRepository(db)

	for _, knowledge := range []*types.Knowledge{
		{
			ID: "knowledge-a", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/a.md"}`),
		},
		{
			ID: "knowledge-b", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/b.md"}`),
		},
		// docs/c.md 先建 v1 再重导出 v2：document 的 current 指向 v2。
		{
			ID: "knowledge-c-v1", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/c.md"}`),
		},
		{
			ID: "knowledge-c-v2", TenantID: 1, KnowledgeBaseID: "kb-1",
			ParseStatus: types.ParseStatusCompleted,
			Metadata:    types.JSON(`{"datasource_id":"source-1","external_id":"docs/c.md"}`),
		},
	} {
		require.NoError(t, repo.CreateKnowledge(context.Background(), knowledge))
	}

	// 批量删除 a、b 的当前世代和 c 的旧世代 v1。
	require.NoError(t, repo.DeleteKnowledgeList(
		context.Background(), 1, []string{"knowledge-a", "knowledge-b", "knowledge-c-v1"},
	))

	// a、b 已无内容世代 → 文档行软删；c 的 current 仍指向 v2 → 文档行保留。
	var live []types.Document
	require.NoError(t, db.Find(&live).Error)
	require.Len(t, live, 1)
	require.Equal(t, "knowledge-c-v2", live[0].CurrentKnowledgeID)
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
