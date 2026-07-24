package service

import (
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessSyncCancelsWhenKnowledgeBaseDeleted(t *testing.T) {
	ds := &types.DataSource{
		ID:              "ds-1",
		TenantID:        1,
		KnowledgeBaseID: "kb-deleted",
		Type:            types.ConnectorTypeRSS,
		Status:          types.DataSourceStatusActive,
	}
	dsRepo := newKBDeleteDSRepo("kb-deleted", ds)
	syncLog := &types.SyncLog{
		ID:           "log-1",
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	syncLogRepo := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{syncLog.ID: syncLog}}

	svc := &DataSourceService{
		dsRepo:      dsRepo,
		syncLogRepo: syncLogRepo,
		kbService:   &processSyncKBService{getErr: apprepo.ErrKnowledgeBaseNotFound},
	}

	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		SyncLogID:    syncLog.ID,
	})
	require.NoError(t, err)

	err = svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))
	require.NoError(t, err)

	updated := syncLogRepo.logs[syncLog.ID]
	require.NotNil(t, updated)
	assert.Equal(t, types.SyncLogStatusCanceled, updated.Status)
	assert.Equal(t, "knowledge base has been deleted", updated.ErrorMessage)
	require.NotNil(t, updated.FinishedAt)
}

type processSyncKBService struct {
	getErr error
}

func (s *processSyncKBService) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, s.getErr
}
func (s *processSyncKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, s.getErr
}
func (s *processSyncKBService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}
func (s *processSyncKBService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) UpdateKnowledgeBase(
	context.Context, string, string, string, *types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) DeleteKnowledgeBase(context.Context, string) error { return nil }
func (s *processSyncKBService) TogglePinKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return nil, nil
}
func (s *processSyncKBService) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, nil
}
func (s *processSyncKBService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return nil
}
func (s *processSyncKBService) CopyKnowledgeBase(context.Context, string, string) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	return nil, nil, nil
}
func (s *processSyncKBService) DuplicateKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) GetRepository() interfaces.KnowledgeBaseRepository { return nil }
func (s *processSyncKBService) ProcessKBDelete(context.Context, *asynq.Task) error {
	return nil
}

var _ interfaces.KnowledgeBaseService = (*processSyncKBService)(nil)

type processSyncSyncLogRepo struct {
	logs map[string]*types.SyncLog
}

func (r *processSyncSyncLogRepo) Create(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}
func (r *processSyncSyncLogRepo) FindByID(_ context.Context, id string) (*types.SyncLog, error) {
	log, ok := r.logs[id]
	if !ok {
		return nil, errors.New("sync log not found")
	}
	return log, nil
}
func (r *processSyncSyncLogRepo) FindByDataSource(context.Context, string, int, int) ([]*types.SyncLog, error) {
	return nil, nil
}
func (r *processSyncSyncLogRepo) FindLatest(context.Context, string) (*types.SyncLog, error) {
	return nil, nil
}
func (r *processSyncSyncLogRepo) HasRunningSync(context.Context, string) (bool, error) {
	return false, nil
}
func (r *processSyncSyncLogRepo) Update(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}
func (r *processSyncSyncLogRepo) UpdateResult(_ context.Context, log *types.SyncLog) error {
	return r.Update(context.Background(), log)
}
func (r *processSyncSyncLogRepo) CancelPendingByDataSource(context.Context, string) error {
	return nil
}
func (r *processSyncSyncLogRepo) CleanupOldLogs(context.Context, int) error { return nil }

func TestAllFetchedItemsFailedError(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  2,
		Failed: 2,
		Errors: []types.SyncItemError{{Message: "doc one: export failed"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all fetched items failed during sync (2/2)")
	assert.Contains(t, err.Error(), "doc one: export failed")
}

func TestAllFetchedItemsFailedErrorIgnoresPartialFailure(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Created: 1,
		Failed:  2,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorIgnoresSkippedItems(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Skipped: 3,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorTruncatesLongDetail(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  1,
		Failed: 1,
		Errors: []types.SyncItemError{{Message: strings.Repeat("x", 600)}},
	})
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 560)
	assert.Contains(t, err.Error(), "...")
}

func TestDeleteFetchedItemUsesCompositeIdentityAndDeletesKnowledge(t *testing.T) {
	repo := &metadataKnowledgeRepo{
		result: &types.Knowledge{ID: "knowledge-b"},
	}
	knowledgeService := &datasourceKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: knowledgeService}
	ds := &types.DataSource{
		ID:              "source-b",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
	}

	deleted, err := svc.deleteFetchedItem(context.Background(), ds, "docs/readme.md")

	require.NoError(t, err)
	require.True(t, deleted)
	require.Equal(t, []string{"knowledge-b"}, knowledgeService.deletedIDs)
	require.Equal(t, map[string]string{
		"datasource_id": "source-b",
		"external_id":   "docs/readme.md",
	}, repo.filters)
}

func TestDeleteFetchedItemDoesNotDeleteAnotherDataSourceDocument(t *testing.T) {
	repo := &metadataKnowledgeRepo{}
	knowledgeService := &datasourceKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: knowledgeService}
	ds := &types.DataSource{
		ID:              "source-b",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
	}

	deleted, err := svc.deleteFetchedItem(context.Background(), ds, "docs/readme.md")

	require.NoError(t, err)
	require.False(t, deleted)
	require.Empty(t, knowledgeService.deletedIDs)
}

func TestApplyFetchedItemDeletesSourceOwnedKnowledge(t *testing.T) {
	repo := &metadataKnowledgeRepo{
		result: &types.Knowledge{ID: "knowledge-old"},
	}
	knowledgeService := &datasourceKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: knowledgeService}
	ds := &types.DataSource{
		ID:              "source-a",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		SyncDeletions:   true,
	}
	result := &types.SyncResult{}

	svc.applyFetchedItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "version-8.2.0/docs/readme.md",
		Title:      "readme",
		IsDeleted:  true,
	}, nil, result)

	require.Equal(t, []string{"knowledge-old"}, knowledgeService.deletedIDs)
	assert.Equal(t, 1, result.Deleted)
	assert.Zero(t, result.Failed)
}

func TestApplyFetchedItemPreservesDeletedSourceItemWhenSyncDeletionsDisabled(t *testing.T) {
	repo := &metadataKnowledgeRepo{
		result: &types.Knowledge{ID: "knowledge-old"},
	}
	knowledgeService := &datasourceKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: knowledgeService}
	result := &types.SyncResult{}

	svc.applyFetchedItem(context.Background(), &types.DataSource{
		ID:              "source-a",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		SyncDeletions:   false,
	}, &types.FetchedItem{
		ExternalID: "docs/readme.md",
		IsDeleted:  true,
	}, nil, result)

	assert.Empty(t, knowledgeService.deletedIDs)
	assert.Zero(t, result.Deleted)
	assert.Equal(t, 1, result.Skipped)
	assert.Zero(t, result.Failed)
}

func TestApplyFetchedItemReportsDeleteFailure(t *testing.T) {
	svc := &DataSourceService{knowledgeService: &datasourceKnowledgeService{
		repo: &metadataKnowledgeRepo{err: errors.New("lookup failed")},
	}}
	result := &types.SyncResult{}

	svc.applyFetchedItem(context.Background(), &types.DataSource{
		ID:              "source-a",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		SyncDeletions:   true,
	}, &types.FetchedItem{
		ExternalID: "docs/readme.md",
		Title:      "readme",
		IsDeleted:  true,
	}, nil, result)

	assert.Zero(t, result.Deleted)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "delete_failed", result.Errors[0].Code)
}

func TestApplyFetchedItemsDeletesBeforeCreatingReplacementPath(t *testing.T) {
	repo := &metadataKnowledgeRepo{
		results: map[string]*types.Knowledge{
			"version-8.2.0/docs/readme.md": {ID: "knowledge-old"},
		},
	}
	knowledgeService := &datasourceKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: knowledgeService}
	ds := &types.DataSource{
		ID:              "source-a",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Type:            types.ConnectorTypeGit,
		SyncDeletions:   true,
	}
	result := &types.SyncResult{}
	items := []types.FetchedItem{
		{
			ExternalID: "version-8.3.0/docs/readme.md",
			Title:      "readme",
			FileName:   "readme.md",
			Content:    []byte("unchanged content"),
		},
		{
			ExternalID: "version-8.2.0/docs/readme.md",
			Title:      "readme",
			IsDeleted:  true,
		},
	}

	svc.applyFetchedItems(context.Background(), ds, items, nil, result)

	require.Equal(t, []string{
		"delete:knowledge-old",
		"create:version-8.3.0/docs/readme.md",
	}, knowledgeService.operations)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, 1, result.Created)
	assert.Zero(t, result.Failed)
}

type metadataKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	result  *types.Knowledge
	results map[string]*types.Knowledge
	err     error
	filters map[string]string
}

func (r *metadataKnowledgeRepo) FindByMetadata(
	_ context.Context,
	_ uint64,
	_ string,
	filters map[string]string,
) (*types.Knowledge, error) {
	r.filters = filters
	if r.results != nil {
		return r.results[filters["external_id"]], r.err
	}
	return r.result, r.err
}

type datasourceKnowledgeService struct {
	interfaces.KnowledgeService
	repo       interfaces.KnowledgeRepository
	deletedIDs []string
	operations []string
}

func (s *datasourceKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

func (s *datasourceKnowledgeService) DeleteKnowledge(_ context.Context, id string) error {
	s.deletedIDs = append(s.deletedIDs, id)
	s.operations = append(s.operations, "delete:"+id)
	return nil
}

func (s *datasourceKnowledgeService) CreateKnowledgeFromFile(
	_ context.Context,
	_ string,
	_ *multipart.FileHeader,
	metadata map[string]string,
	_ *bool,
	_ string,
	_ []string,
	_ string,
	_ *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	externalID := metadata["external_id"]
	s.operations = append(s.operations, "create:"+externalID)
	return &types.Knowledge{ID: "created-" + externalID}, nil
}
