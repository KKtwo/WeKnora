package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type imageInfoUpdateChunkService struct {
	interfaces.ChunkService
	chunk *types.Chunk
}

func (s *imageInfoUpdateChunkService) GetChunkByID(context.Context, string) (*types.Chunk, error) {
	return s.chunk, nil
}

func TestTrustedStoredImageInfoForContentOnlyIncludesPersistedImages(t *testing.T) {
	trusted := "resource://AbCdEfGhIjKlMnOpQrStUv"
	injected := "resource://ZyXwVuTsRqPoNmLkJiHgFe"
	raw := trustedStoredImageInfoForContent(
		"trusted ![a]("+trusted+") injected ![b]("+injected+")",
		[]docparser.StoredImage{
			{ServingURL: trusted, OriginalRef: "images/a.png", MimeType: "image/png"},
		},
	)

	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(raw), &infos); err != nil {
		t.Fatalf("invalid image info JSON: %v", err)
	}
	if len(infos) != 1 || infos[0].URL != trusted || infos[0].OriginalURL != "images/a.png" {
		t.Fatalf("unexpected trusted image info: %#v", infos)
	}
}

func TestTrustedStoredImageInfoForContentDeduplicatesServingURL(t *testing.T) {
	path := "storage://backend-1/minio://bucket/1/exports/a.png"
	raw := trustedStoredImageInfoForContent(
		"![a]("+path+")",
		[]docparser.StoredImage{
			{ServingURL: path, OriginalRef: "images/a.png"},
			{ServingURL: path, OriginalRef: "./images/a.png"},
		},
	)

	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(raw), &infos); err != nil {
		t.Fatalf("invalid image info JSON: %v", err)
	}
	if len(infos) != 1 || infos[0].URL != path {
		t.Fatalf("expected one deduplicated image, got %#v", infos)
	}
}

func TestUpdateImageInfoRejectsChunkFromAnotherKnowledge(t *testing.T) {
	service := &knowledgeService{
		chunkService: &imageInfoUpdateChunkService{
			chunk: &types.Chunk{ID: "chunk-b", KnowledgeID: "knowledge-b"},
		},
	}

	err := service.UpdateImageInfo(
		context.Background(),
		"knowledge-a",
		"chunk-b",
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv"}]`,
	)
	if err == nil {
		t.Fatal("expected cross-knowledge chunk update to be rejected")
	}
}

func TestMergeEditableImageInfoPreservesAssetIdentity(t *testing.T) {
	current := `[
		{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","original_url":"images/a.png"},
		{"url":"resource://ZyXwVuTsRqPoNmLkJiHgFe","original_url":"images/b.png"}
	]`
	merged, updated, err := mergeEditableImageInfo(
		current,
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","original_url":"images/a.png","caption":"新描述"}]`,
	)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(merged), &infos); err != nil {
		t.Fatalf("invalid merged image info: %v", err)
	}
	if len(infos) != 2 || infos[0].Caption != "新描述" || infos[1].OriginalURL != "images/b.png" {
		t.Fatalf("unexpected merged image info: %#v", infos)
	}
	if updated.URL != "resource://AbCdEfGhIjKlMnOpQrStUv" {
		t.Fatalf("asset URL changed: %#v", updated)
	}
}

func TestMergeEditableImageInfoRejectsNewURL(t *testing.T) {
	_, _, err := mergeEditableImageInfo(
		`[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","original_url":"images/a.png"}]`,
		`[{"url":"resource://ZyXwVuTsRqPoNmLkJiHgFe","original_url":"images/a.png"}]`,
	)
	if err == nil {
		t.Fatal("expected unregistered image URL to be rejected")
	}
}
