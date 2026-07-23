package chatpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type failingSearchKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	err error
}

func (s *failingSearchKnowledgeBaseService) GetKnowledgeBasesByIDsOnly(
	context.Context, []string,
) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "kb-1", EmbeddingModelID: "embedding-1"}}, nil
}

func (s *failingSearchKnowledgeBaseService) ResolveEmbeddingModelKeys(
	context.Context, []*types.KnowledgeBase,
) map[string]string {
	return map[string]string{"kb-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *failingSearchKnowledgeBaseService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.err
}

func (s *failingSearchKnowledgeBaseService) HybridSearch(
	context.Context, string, types.SearchParams,
) ([]*types.SearchResult, error) {
	return nil, s.err
}

func TestSearchEmbeddingFailureIsNotReportedAsNoResults(t *testing.T) {
	rootCause := errors.New("embedding endpoint unavailable")
	plugin := &PluginSearch{
		knowledgeBaseService: &failingSearchKnowledgeBaseService{err: rootCause},
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SearchTargets: types.SearchTargets{{
				Type:            types.SearchTargetTypeKnowledgeBase,
				KnowledgeBaseID: "kb-1",
			}},
			EmbeddingTopK: 10,
		},
		PipelineState: types.PipelineState{RewriteQuery: "精简邮件效果"},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError {
		return nil
	})
	if err == nil || err.ErrorType != ErrSearch.ErrorType {
		t.Fatalf("expected search_failed, got %#v", err)
	}
	if !errors.Is(err.Err, rootCause) {
		t.Fatalf("expected root cause to be preserved, got %v", err.Err)
	}
}
