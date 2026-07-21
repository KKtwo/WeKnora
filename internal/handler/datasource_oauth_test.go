package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildOAuthAuthorizationURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &DataSourceHandler{}
	router.POST("/datasource/oauth/authorize-url", handler.BuildOAuthAuthorizationURL)

	body := []byte(`{
		"connector_type":"feishu",
		"client_id":"cli_test",
		"redirect_uri":"https://cis.example.com/callback",
		"state":"signed-state",
		"scopes":["wiki:wiki:readonly"]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/datasource/oauth/authorize-url", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "accounts.feishu.cn")
	require.Contains(t, response.Body.String(), "signed-state")
}
