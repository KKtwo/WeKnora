package chatpipeline

import (
	"context"
	"slices"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestShouldExpandQuery(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		query      string
		results    int
		topK       int
		wantExpand bool
	}{
		{name: "disabled", enabled: false, query: "什么是 DataFlow", results: 10, topK: 10},
		{name: "low recall", enabled: true, query: "什么是 DataFlow", results: 3, topK: 10, wantExpand: true},
		{name: "simple full recall", enabled: true, query: "DataFlow 产品定位", results: 10, topK: 10},
		{
			name:       "composite full recall",
			enabled:    true,
			query:      "企业要沿哪些路径建设 BI？除了决策支持还能带来哪些价值？",
			results:    10,
			topK:       10,
			wantExpand: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldExpandQuery(tt.enabled, tt.query, tt.results, tt.topK); got != tt.wantExpand {
				t.Fatalf("shouldExpandQuery() = %v, want %v", got, tt.wantExpand)
			}
		})
	}
}

func TestExpandQueriesDecomposesCompositeQuestion(t *testing.T) {
	p := &PluginSearch{}
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "企业要沿哪些路径建设 BI？除了决策支持还能带来哪些价值？"},
		PipelineState:   types.PipelineState{RewriteQuery: "企业要沿哪些路径建设 BI？除了决策支持还能带来哪些价值？"},
	}
	got := p.expandQueries(context.Background(), cm)
	if !slices.Contains(got, "企业要沿哪些路径建设") {
		t.Fatalf("missing first decomposed query: %#v", got)
	}
	if !slices.Contains(got, "除了决策支持还能带来哪些价值") {
		t.Fatalf("missing second decomposed query: %#v", got)
	}
}
