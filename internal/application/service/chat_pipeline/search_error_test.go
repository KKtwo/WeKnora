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

// embedding 失败会先降级为关键词检索；本用例中关键词检索也失败，
// 此时必须仍报 search_failed 并保留根因，而不是伪装成"无结果"。
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

type degradingSearchKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	embedErr     error
	gotParams    *types.SearchParams
	searchResult []*types.SearchResult
}

func (s *degradingSearchKnowledgeBaseService) GetKnowledgeBasesByIDsOnly(
	context.Context, []string,
) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "kb-1", EmbeddingModelID: "embedding-1"}}, nil
}

func (s *degradingSearchKnowledgeBaseService) ResolveEmbeddingModelKeys(
	context.Context, []*types.KnowledgeBase,
) map[string]string {
	return map[string]string{"kb-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *degradingSearchKnowledgeBaseService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.embedErr
}

func (s *degradingSearchKnowledgeBaseService) HybridSearch(
	_ context.Context, _ string, params types.SearchParams,
) ([]*types.SearchResult, error) {
	s.gotParams = &params
	return s.searchResult, nil
}

// embedding API 限流/长尾时检索必须降级为纯关键词模式返回结果，
// 而不是让整次 knowledge-search 以 500 失败（评测中三次复现的故障）。
func TestSearchEmbeddingFailureDegradesToKeywordSearch(t *testing.T) {
	svc := &degradingSearchKnowledgeBaseService{
		embedErr: errors.New("embedding rate limited"),
		searchResult: []*types.SearchResult{
			{ID: "chunk-1", Content: "关键词命中的内容", KnowledgeID: "k-1"},
		},
	}
	plugin := &PluginSearch{knowledgeBaseService: svc}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SearchTargets: types.SearchTargets{{
				Type:            types.SearchTargetTypeKnowledgeBase,
				KnowledgeBaseID: "kb-1",
			}},
			EmbeddingTopK: 10,
		},
		PipelineState: types.PipelineState{RewriteQuery: "树状筛选器 新建入口"},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError {
		return nil
	})
	if err != nil {
		t.Fatalf("expected degraded keyword search to succeed, got %#v", err)
	}
	if len(chatManage.SearchResult) != 1 || chatManage.SearchResult[0].ID != "chunk-1" {
		t.Fatalf("expected keyword results to be returned, got %#v", chatManage.SearchResult)
	}
	if svc.gotParams == nil || !svc.gotParams.DisableVectorMatch {
		t.Fatalf("expected DisableVectorMatch=true after embedding failure, got %#v", svc.gotParams)
	}
	if len(svc.gotParams.QueryEmbedding) != 0 {
		t.Fatalf("expected empty query embedding in degraded mode")
	}
}
