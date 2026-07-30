package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cancellationAwareSessionService struct {
	interfaces.SessionService
	entered   chan struct{}
	cancelled chan struct{}
}

func (s *cancellationAwareSessionService) SearchKnowledge(
	ctx context.Context,
	_ []string,
	_ []string,
	_ []types.TagScope,
	_ string,
) ([]*types.SearchResult, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.cancelled)
	return nil, ctx.Err()
}

func TestSearchKnowledgePropagatesRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &cancellationAwareSessionService{
		entered:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	handler := &Handler{sessionService: service}

	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(
		http.MethodPost,
		"/sessions/search",
		strings.NewReader(`{"query":"cancel me","knowledge_base_ids":["kb-1"]}`),
	).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = request

	done := make(chan struct{})
	go func() {
		handler.SearchKnowledge(ginContext)
		close(done)
	}()

	awaitSignal(t, service.entered, "search service entry")
	cancel()
	awaitSignal(t, service.cancelled, "request cancellation at search service")
	awaitSignal(t, done, "handler completion")
}

func TestSearchKnowledgeReturnsTooManyRequestsWhenQueueIsFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalAdmission := sessionSearchAdmission
	sessionSearchAdmission = newSearchAdmission(1, 0)
	defer func() { sessionSearchAdmission = originalAdmission }()

	service := &cancellationAwareSessionService{
		entered:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	handler := &Handler{sessionService: service}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	first := newSearchGinContext(firstContext)
	firstDone := make(chan struct{})
	go func() {
		handler.SearchKnowledge(first)
		close(firstDone)
	}()
	awaitSignal(t, service.entered, "first search service entry")

	second := newSearchGinContext(context.Background())
	handler.SearchKnowledge(second)
	require.Len(t, second.Errors, 1)
	appError, ok := second.Errors[0].Err.(*apperrors.AppError)
	require.True(t, ok)
	require.Equal(t, http.StatusTooManyRequests, appError.HTTPCode)
	require.Equal(t, "1", second.Writer.Header().Get("Retry-After"))

	cancelFirst()
	awaitSignal(t, service.cancelled, "first request cancellation")
	awaitSignal(t, firstDone, "first handler completion")
}

func newSearchGinContext(ctx context.Context) *gin.Context {
	request := httptest.NewRequest(
		http.MethodPost,
		"/sessions/search",
		strings.NewReader(`{"query":"search","knowledge_base_ids":["kb-1"]}`),
	).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request
	return ginContext
}

func awaitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
