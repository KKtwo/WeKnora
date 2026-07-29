package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type imageInfoChunkRepo struct {
	interfaces.ChunkRepository
}

func (r *imageInfoChunkRepo) ListChunksByParentIDs(
	_ context.Context,
	_ uint64,
	parentIDs []string,
) ([]*types.Chunk, error) {
	if len(parentIDs) == 1 && parentIDs[0] == "chunk-1" {
		return []*types.Chunk{{
			ID:            "image-child",
			ParentChunkID: "chunk-1",
			ChunkType:     types.ChunkTypeImageCaption,
			ImageInfo:     `[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv","caption":"图表"}]`,
		}}, nil
	}
	return nil, nil
}

func TestGetChunkByIDOnlyEnrichesImageInfoFromImageChildren(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/chunks/by-id/chunk-1", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "chunk-1"}}
	handler := &ChunkHandler{
		service: &stubChunkService{
			getByIDOnly: func(_ context.Context, _ string) (*types.Chunk, error) {
				return &types.Chunk{
					ID:          "chunk-1",
					TenantID:    7,
					KnowledgeID: "knowledge-1",
					ChunkType:   types.ChunkTypeText,
					ImageInfo:   `[{"url":"resource://AbCdEfGhIjKlMnOpQrStUv"}]`,
				}, nil
			},
			repo: &imageInfoChunkRepo{},
		},
	}

	handler.GetChunkByIDOnly(ctx)

	var body struct {
		Data types.Chunk `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if body.Data.ImageInfo == "" {
		t.Fatal("expected image_info enriched from child image chunks")
	}
	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(body.Data.ImageInfo), &infos); err != nil {
		t.Fatalf("invalid image_info: %v", err)
	}
	if len(infos) != 1 || infos[0].Caption != "图表" {
		t.Fatalf("expected trusted image enriched with caption, got %#v", infos)
	}
}
