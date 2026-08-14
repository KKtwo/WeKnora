package chatpipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type blockingReranker struct{}

func (blockingReranker) Rerank(
	ctx context.Context, _ string, _ []string,
) ([]rerank.RankResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingReranker) GetModelName() string { return "blocking-reranker" }

func (blockingReranker) GetModelID() string { return "blocking-reranker-id" }

type emptyThenBlockingReranker struct {
	calls int
}

func (r *emptyThenBlockingReranker) Rerank(
	ctx context.Context, _ string, _ []string,
) ([]rerank.RankResult, error) {
	r.calls++
	if r.calls == 1 {
		return nil, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*emptyThenBlockingReranker) GetModelName() string { return "empty-then-blocking-reranker" }

func (*emptyThenBlockingReranker) GetModelID() string { return "empty-then-blocking-reranker-id" }

type rerankTimeoutModelService struct {
	interfaces.ModelService
	reranker rerank.Reranker
}

func (s *rerankTimeoutModelService) GetRerankModel(
	context.Context, string,
) (rerank.Reranker, error) {
	return s.reranker, nil
}

func TestPluginRerankStageTimeoutFallsBackBeforeParentCancellation(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	original := &types.SearchResult{
		ID:          "chunk-1",
		KnowledgeID: "knowledge-1",
		ChunkType:   string(types.ChunkTypeText),
		Content:     "raw retrieval result",
		Score:       0.8,
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SearchTargets: types.SearchTargets{{
				Type:            types.SearchTargetTypeKnowledgeBase,
				KnowledgeBaseID: "kb-1",
			}},
			RerankModelID: "blocking-reranker-id",
			RerankTopK:    5,
		},
		PipelineState: types.PipelineState{
			RewriteQuery: "query",
			SearchResult: []*types.SearchResult{original},
		},
	}
	plugin := &PluginRerank{
		modelService: &rerankTimeoutModelService{reranker: blockingReranker{}},
		stageTimeout: 15 * time.Millisecond,
	}

	nextCalled := false
	startedAt := time.Now()
	err := plugin.OnEvent(parentCtx, types.CHUNK_RERANK, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("expected rerank timeout to degrade successfully, got %#v", err)
	}
	if !nextCalled {
		t.Fatal("expected pipeline to continue after rerank timeout")
	}
	if parentErr := parentCtx.Err(); parentErr != nil {
		t.Fatalf("parent context must remain usable after rerank timeout: %v", parentErr)
	}
	if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
		t.Fatalf("rerank fallback took too long: %v", elapsed)
	}
	if len(chatManage.SearchResult) != 1 || chatManage.SearchResult[0] != original {
		t.Fatalf("expected original retrieval result to be preserved, got %#v", chatManage.SearchResult)
	}
	if len(chatManage.RerankResult) != 0 {
		t.Fatalf("expected no rerank results after timeout, got %#v", chatManage.RerankResult)
	}
}

func TestPluginRerankStageTimeoutIsSharedByThresholdRetry(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	reranker := &emptyThenBlockingReranker{}
	plugin := &PluginRerank{
		modelService: &rerankTimeoutModelService{reranker: reranker},
		stageTimeout: 15 * time.Millisecond,
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SearchTargets:   types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1"}},
			RerankModelID:   "empty-then-blocking-reranker-id",
			RerankThreshold: 0.5,
		},
		PipelineState: types.PipelineState{
			RewriteQuery: "query",
			SearchResult: []*types.SearchResult{{ID: "chunk-1", Content: "raw retrieval result"}},
		},
	}

	startedAt := time.Now()
	err := plugin.OnEvent(parentCtx, types.CHUNK_RERANK, chatManage, func() *PluginError { return nil })

	if err != nil {
		t.Fatalf("expected threshold retry timeout to degrade successfully, got %#v", err)
	}
	if reranker.calls != 2 {
		t.Fatalf("expected initial rerank and one threshold retry, got %d calls", reranker.calls)
	}
	if parentErr := parentCtx.Err(); parentErr != nil {
		t.Fatalf("parent context must remain usable after retry timeout: %v", parentErr)
	}
	if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
		t.Fatalf("shared rerank stage budget was not enforced: %v", elapsed)
	}
}

func TestPluginRerankStageTimeoutStillHonorsParentCancellation(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	plugin := &PluginRerank{
		modelService: &rerankTimeoutModelService{reranker: blockingReranker{}},
		stageTimeout: time.Second,
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SearchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1"}},
			RerankModelID: "blocking-reranker-id",
		},
		PipelineState: types.PipelineState{
			RewriteQuery: "query",
			SearchResult: []*types.SearchResult{{ID: "chunk-1", Content: "raw retrieval result"}},
		},
	}

	startedAt := time.Now()
	_ = plugin.OnEvent(parentCtx, types.CHUNK_RERANK, chatManage, func() *PluginError { return nil })

	if elapsed := time.Since(startedAt); elapsed >= 100*time.Millisecond {
		t.Fatalf("parent cancellation was not propagated promptly: %v", elapsed)
	}
	if !errors.Is(parentCtx.Err(), context.Canceled) {
		t.Fatalf("expected parent context cancellation, got %v", parentCtx.Err())
	}
}

func TestRerankFallbackReasonDistinguishesStageTimeoutFromRequestCancellation(t *testing.T) {
	if got := rerankFallbackReason(context.Background(), context.DeadlineExceeded); got != "stage_timeout" {
		t.Fatalf("stage timeout reason = %q, want stage_timeout", got)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := rerankFallbackReason(cancelledCtx, context.Canceled); got != "request_cancelled" {
		t.Fatalf("request cancellation reason = %q, want request_cancelled", got)
	}

	if got := rerankFallbackReason(context.Background(), errors.New("provider unavailable")); got != "api_error" {
		t.Fatalf("provider error reason = %q, want api_error", got)
	}
}
