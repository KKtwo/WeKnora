package searchutil

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMergeImageInfoJSONKeepsTrustedURLAndAddsCaption(t *testing.T) {
	merged := mergeImageInfoJSON(
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","original_url":"images/chart.png"}]`,
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","caption":"季度收入图"}]`,
	)

	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(merged), &infos); err != nil {
		t.Fatalf("invalid image info JSON: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected one deduplicated image, got %#v", infos)
	}
	if infos[0].OriginalURL != "images/chart.png" || infos[0].Caption != "季度收入图" {
		t.Fatalf("expected trusted path and enriched caption, got %#v", infos[0])
	}
}

func TestMergeImageInfoJSONDoesNotAppendChildOnlyURL(t *testing.T) {
	merged := mergeImageInfoJSON(
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv"}]`,
		`[{"url":"resource://ZyXwVuTsRqPoNmLkJiHgFe","caption":"不可信"}]`,
	)

	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(merged), &infos); err != nil {
		t.Fatalf("invalid image info JSON: %v", err)
	}
	if len(infos) != 1 || infos[0].URL != "resource://AbCdEfGhIjKlMnOpQrStUv" {
		t.Fatalf("child-only URL must not be appended, got %#v", infos)
	}
}

func TestMergeImageInfoJSONRequiresTrustedPrimary(t *testing.T) {
	if merged := mergeImageInfoJSON(
		"",
		`[{"url":"resource://ZyXwVuTsRqPoNmLkJiHgFe","caption":"不可信"}]`,
	); merged != "" {
		t.Fatalf("expected no image info without trusted primary, got %s", merged)
	}
}
