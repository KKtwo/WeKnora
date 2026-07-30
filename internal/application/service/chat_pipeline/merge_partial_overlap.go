package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// removePartialOverlaps drops chunks whose content is largely contained within
// a higher-scored chunk, even across different knowledge sources. This catches
// cross-KB duplicates and near-duplicates that exact-signature dedup misses.
//
// Two thresholds are used:
//   - Substring containment: if the normalized short text is a literal substring
//     of the normalized long text, the shorter chunk is removed.
//   - Token overlap coefficient >= 0.85: if 85%+ of the smaller chunk's tokens
//     appear in the larger chunk, the smaller one is redundant.
//
// The input slice MUST already be deduplicated by ID/signature. Within each
// pair the chunk with the lower score is the candidate for removal; ties are
// broken by content length (longer wins).
func removePartialOverlaps(ctx context.Context, results []*types.SearchResult) []*types.SearchResult {
	const overlapThreshold = 0.85

	if len(results) <= 1 {
		return results
	}

	type normEntry struct {
		norm   string
		tokens map[string]struct{}
		result *types.SearchResult
	}

	entries := make([]normEntry, 0, len(results))
	for _, r := range results {
		if ctx.Err() != nil {
			return nil
		}
		entries = append(entries, normEntry{
			norm:   searchutil.NormalizeContent(r.Content),
			tokens: searchutil.TokenizeSimple(r.Content),
			result: r,
		})
	}

	removed := make(map[int]bool)

	for i := 0; i < len(entries); i++ {
		if ctx.Err() != nil {
			return nil
		}
		if removed[i] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if ctx.Err() != nil {
				return nil
			}
			if removed[j] {
				continue
			}

			a, b := entries[i], entries[j]

			shortIdx, longIdx := i, j
			if len(a.norm) > len(b.norm) {
				shortIdx, longIdx = j, i
			}

			contained := searchutil.IsContentContained(
				entries[shortIdx].norm, entries[longIdx].norm,
			)

			if !contained {
				// Token sets are computed once per candidate; the previous pairwise
				// tokenization amplified this O(n²) loop into sustained CPU saturation.
				ratio := tokenOverlapRatio(
					entries[shortIdx].tokens,
					entries[longIdx].tokens,
				)
				if ratio < overlapThreshold {
					continue
				}
			}

			victim := shortIdx
			if entries[shortIdx].result.Score > entries[longIdx].result.Score {
				victim = longIdx
			}
			removed[victim] = true

			keptIdx := i
			if victim == i {
				keptIdx = j
			}
			pipelineInfo(ctx, "Merge", "partial_overlap_drop", map[string]interface{}{
				"kept_id":    entries[keptIdx].result.ID,
				"dropped_id": entries[victim].result.ID,
				"contained":  contained,
			})
		}
	}

	out := make([]*types.SearchResult, 0, len(results)-len(removed))
	for i, e := range entries {
		if !removed[i] {
			out = append(out, e.result)
		}
	}
	return out
}

// tokenOverlapRatio returns |intersection| / |smaller set| for cached token sets.
func tokenOverlapRatio(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, large := a, b
	if len(small) > len(large) {
		small, large = large, small
	}
	intersection := 0
	for token := range small {
		if _, ok := large[token]; ok {
			intersection++
		}
	}
	return float64(intersection) / float64(len(small))
}
