package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type attachmentTestSessionService struct {
	interfaces.SessionService
}

func (s *attachmentTestSessionService) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &types.Session{ID: "session-1", TenantID: 1}, nil
}

func TestInlineAttachmentRequestLimitIncludesBase64AndMetadata(t *testing.T) {
	const maxFileSize = int64(100 * 1024 * 1024)
	if got, want := inlineAttachmentRequestLimit(maxFileSize), int64(135*1024*1024); got != want {
		t.Fatalf("inlineAttachmentRequestLimit() = %d, want %d", got, want)
	}
}

func TestParseQARequestRejectsOversizedBodyBeforeDependencies(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "1")
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/sessions/session-1/qa", strings.NewReader(`{"query":"`+strings.Repeat("x", 3*1024*1024)+`"}`))
	ctx.Params = gin.Params{{Key: "session_id", Value: "session-1"}}

	_, _, err := (&Handler{}).parseQARequest(ctx, "test")
	if err == nil || !strings.Contains(err.Error(), "request body too large") {
		t.Fatalf("parseQARequest() error = %v, want request body too large", err)
	}
}

func TestParseQARequestRejectsDecodedAttachmentAboveBusinessLimit(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "1")
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(CreateKnowledgeQARequest{
		Query: "test",
		AttachmentUploads: []AttachmentUpload{{
			Data:     base64.StdEncoding.EncodeToString(make([]byte, 2*1024*1024)),
			FileName: "sample.txt",
			FileSize: 1,
		}},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/sessions/session-1/qa", strings.NewReader(string(body)))
	ctx.Params = gin.Params{{Key: "session_id", Value: "session-1"}}
	handler := &Handler{sessionService: &attachmentTestSessionService{}}

	_, _, parseErr := handler.parseQARequest(ctx, "test")
	if parseErr == nil || !strings.Contains(parseErr.Error(), "attachment 1 exceeds size limit of 1MB") {
		t.Fatalf("parseQARequest() error = %v, want decoded attachment size error", parseErr)
	}
}

func TestDecodeAttachmentUploadsUsesDecodedSize(t *testing.T) {
	uploads := []AttachmentUpload{{
		Data:     base64.StdEncoding.EncodeToString([]byte("12345")),
		FileName: "sample.txt",
		FileSize: 1,
	}}

	if _, err := decodeAttachmentUploads(uploads, 4); err == nil {
		t.Fatal("expected decoded bytes above the limit to be rejected")
	}
}

func TestDecodeAttachmentUploadsCapsAggregateSize(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("123"))
	uploads := []AttachmentUpload{
		{Data: encoded, FileName: "one.txt", FileSize: 3},
		{Data: encoded, FileName: "two.txt", FileSize: 3},
	}

	if _, err := decodeAttachmentUploads(uploads, 5); err == nil {
		t.Fatal("expected aggregate decoded bytes above the limit to be rejected")
	}
}

func TestDecodeAttachmentUploadsAcceptsBoundary(t *testing.T) {
	payload := []byte("12345")
	uploads := []AttachmentUpload{{
		Data:     base64.StdEncoding.EncodeToString(payload),
		FileName: "sample.txt",
		FileSize: int64(len(payload)),
	}}

	decoded, err := decodeAttachmentUploads(uploads, int64(len(payload)))
	if err != nil {
		t.Fatalf("decodeAttachmentUploads() error = %v", err)
	}
	if got := string(decoded[0]); got != string(payload) {
		t.Fatalf("decoded payload = %q, want %q", got, payload)
	}
}
