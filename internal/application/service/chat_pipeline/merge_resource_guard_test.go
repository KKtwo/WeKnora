package chatpipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type cancelAfterErrChecksContext struct {
	context.Context
	remaining atomic.Int32
}

func (c *cancelAfterErrChecksContext) Err() error {
	if c.remaining.Add(-1) <= 0 {
		return context.Canceled
	}
	return nil
}

func TestPluginMergeStopsWhenRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nextCalled := false
	chatManage := &types.ChatManage{
		PipelineState: types.PipelineState{
			RerankResult: []*types.SearchResult{{ID: "chunk-1", Content: "cancelled request"}},
		},
	}

	err := (&PluginMerge{}).OnEvent(ctx, types.CHUNK_MERGE, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	})

	require.NotNil(t, err)
	assert.True(t, errors.Is(err.Err, context.Canceled))
	assert.False(t, nextCalled)
}

func TestPluginMergeCapsIntermediateCandidatesFromRerankTopK(t *testing.T) {
	results := make([]*types.SearchResult, 0, 160)
	for i := 0; i < 160; i++ {
		results = append(results, &types.SearchResult{
			ID:          fmt.Sprintf("chunk-%03d", i),
			KnowledgeID: fmt.Sprintf("knowledge-%03d", i),
			Content:     fmt.Sprintf("unique candidate content number %03d", i),
			Score:       1 - float64(i)/1000,
		})
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankTopK: 30},
		PipelineState:   types.PipelineState{RerankResult: results},
	}

	err := (&PluginMerge{}).OnEvent(
		context.Background(), types.CHUNK_MERGE, chatManage, func() *PluginError { return nil },
	)

	require.Nil(t, err)
	assert.Len(t, chatManage.MergeResult, 120)
}

func TestPluginMergeDeduplicatesBeforeCandidateCap(t *testing.T) {
	results := make([]*types.SearchResult, 0, 140)
	for i := 0; i < 120; i++ {
		results = append(results, &types.SearchResult{
			ID: "duplicate", Content: "same content", Score: 1,
		})
	}
	for i := 0; i < 20; i++ {
		results = append(results, &types.SearchResult{
			ID:          fmt.Sprintf("unique-%02d", i),
			KnowledgeID: fmt.Sprintf("knowledge-%02d", i),
			Content:     fmt.Sprintf("unique candidate %02d", i),
			Score:       0.5,
		})
	}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankTopK: 30},
		PipelineState:   types.PipelineState{RerankResult: results},
	}

	err := (&PluginMerge{}).OnEvent(
		context.Background(), types.CHUNK_MERGE, chatManage, func() *PluginError { return nil },
	)

	require.Nil(t, err)
	assert.Len(t, chatManage.MergeResult, 21)
}

func TestLimitMergeCandidatesUsesDeterministicTopKOrder(t *testing.T) {
	forward := make([]*types.SearchResult, 0, 101)
	for i := 100; i >= 0; i-- {
		forward = append(forward, &types.SearchResult{
			ID: fmt.Sprintf("chunk-%03d", i), KnowledgeID: fmt.Sprintf("knowledge-%03d", i), Score: 0.5,
		})
	}
	reverse := append([]*types.SearchResult(nil), forward...)
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}

	first := limitMergeCandidates(context.Background(), forward, 10)
	second := limitMergeCandidates(context.Background(), reverse, 10)

	require.Len(t, first, 100)
	require.Len(t, second, 100)
	for i := range first {
		assert.Equal(t, first[i].ID, second[i].ID)
	}
	assert.Equal(t, "chunk-000", first[0].ID)
}

func TestMergeWorkSlotsBoundProcessWideConcurrency(t *testing.T) {
	const jobs = 20
	var active atomic.Int32
	var peak atomic.Int32
	entered := make(chan struct{}, jobs)
	release := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withMergeWorkSlot(context.Background(), func() struct{} {
				current := active.Add(1)
				for observed := peak.Load(); current > observed && !peak.CompareAndSwap(observed, current); {
					observed = peak.Load()
				}
				entered <- struct{}{}
				<-release
				active.Add(-1)
				return struct{}{}
			})
		}()
	}

	for i := 0; i < maxMergeWorkers; i++ {
		<-entered
	}
	close(release)
	wg.Wait()
	assert.Equal(t, int32(maxMergeWorkers), peak.Load())
}

func TestRemovePartialOverlapsKeepsHigherScoredChunk(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "low", Content: "alpha beta gamma delta", Score: 0.4},
		{ID: "high", Content: "alpha beta gamma delta epsilon", Score: 0.9},
	}

	filtered := removePartialOverlaps(context.Background(), results)

	require.Len(t, filtered, 1)
	assert.Equal(t, "high", filtered[0].ID)
}

func TestRemovePartialOverlapsStopsWhenCancelledDuringPairLoop(t *testing.T) {
	ctx := &cancelAfterErrChecksContext{Context: context.Background()}
	ctx.remaining.Store(25)
	results := make([]*types.SearchResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, &types.SearchResult{
			ID: fmt.Sprintf("chunk-%02d", i), Content: fmt.Sprintf("unique content %02d", i),
		})
	}

	filtered := removePartialOverlaps(ctx, results)

	assert.Nil(t, filtered)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}
