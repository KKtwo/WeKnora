package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeToolAPI struct {
	responses map[string]map[string]interface{}
}

func (f *fakeToolAPI) Call(
	_ context.Context,
	service, tool string,
	_ map[string]interface{},
) (map[string]interface{}, error) {
	response, ok := f.responses[service+"."+tool]
	if !ok {
		return nil, fmt.Errorf("unexpected call %s.%s", service, tool)
	}
	return response, nil
}

func (f *fakeToolAPI) Close() error { return nil }

func testConfig() *types.DataSourceConfig {
	return &types.DataSourceConfig{Credentials: map[string]interface{}{
		"client_id":     "ding-client",
		"client_secret": "ding-secret",
		"access_token":  "access-token",
	}}
}

func TestBuildAuthorizationURL(t *testing.T) {
	authURL, err := BuildAuthorizationURL(
		"ding-client",
		"https://cis.example.com/oauth/dingtalk/callback",
		"signed-state",
		"corp-1",
	)
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, "login.dingtalk.com", parsed.Host)
	require.Equal(t, "openid corpid", parsed.Query().Get("scope"))
	require.Equal(t, "signed-state", parsed.Query().Get("state"))
	require.Equal(t, "corp-1", parsed.Query().Get("corpId"))
}

func TestRefreshMutatesCredentialMap(t *testing.T) {
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "new-access",
			"refreshToken": "new-refresh",
			"expireIn":     7200,
			"corpId":       "corp-1",
		})
	}))
	defer server.Close()

	credentials := map[string]interface{}{
		"client_id":               "ding-client",
		"client_secret":           "ding-secret",
		"access_token":            "expired",
		"refresh_token":           "old-refresh",
		"access_token_expires_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		"api_base_url":            server.URL,
	}
	config := &Config{
		ClientID:             "ding-client",
		ClientSecret:         "ding-secret",
		AccessToken:          "expired",
		RefreshToken:         "old-refresh",
		AccessTokenExpiresAt: credentials["access_token_expires_at"].(string),
		APIBaseURL:           server.URL,
		credentials:          credentials,
	}
	provider := &tokenProvider{config: config, httpClient: server.Client()}

	token, err := provider.token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "new-access", token)
	require.Equal(t, "refresh_token", body["grantType"])
	require.Equal(t, "new-refresh", credentials["refresh_token"])
	require.Equal(t, "corp-1", credentials["corp_id"])
}

func TestListResourcesCombinesDocumentsAITablesAndDrive(t *testing.T) {
	api := &fakeToolAPI{responses: map[string]map[string]interface{}{
		"doc.search_documents": {
			"documents": []interface{}{
				map[string]interface{}{"nodeId": "doc-1", "name": "方案", "docType": "adoc"},
				map[string]interface{}{"nodeId": "sheet-1", "name": "预算", "docType": "axls"},
			},
		},
		"aitable.list_bases": {
			"bases": []interface{}{map[string]interface{}{"baseId": "base-1", "baseName": "客户表"}},
		},
		"drive.list_files": {
			"files": []interface{}{map[string]interface{}{"dentryUuid": "folder-1", "name": "资料", "type": "folder"}},
		},
	}}
	connector := NewConnector()
	connector.newAPI = func(*Config) toolAPI { return api }

	resources, err := connector.ListResources(context.Background(), testConfig(), "")
	require.NoError(t, err)
	require.Equal(t, []string{"doc:doc-1", "sheet:sheet-1", "aitable:base-1", "drive-folder:folder-1"}, []string{
		resources[0].ExternalID,
		resources[1].ExternalID,
		resources[2].ExternalID,
		resources[3].ExternalID,
	})
}

func TestIncrementalReturnsChangedAndDeletedDocuments(t *testing.T) {
	api := &fakeToolAPI{responses: map[string]map[string]interface{}{
		"doc.get_document_content": {"content": "current body", "title": "Current"},
	}}
	connector := NewConnector()
	connector.newAPI = func(*Config) toolAPI { return api }
	config := testConfig()
	config.ResourceIDs = []string{"doc:current"}
	cursor := &types.SyncCursor{ConnectorCursor: map[string]interface{}{
		"hashes": map[string]interface{}{
			"doc:current": "old-hash",
			"doc:deleted": "deleted-hash",
		},
	}}

	items, next, err := connector.FetchIncremental(context.Background(), config, cursor)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "doc:current", items[0].ExternalID)
	require.Equal(t, "doc:deleted", items[1].ExternalID)
	require.True(t, items[1].IsDeleted)
	require.NotEmpty(t, next.ConnectorCursor["hashes"])
}

func TestFindDownloadURLAcceptsArrayResponse(t *testing.T) {
	result := map[string]interface{}{
		"result": map[string]interface{}{
			"resourceUrl": []interface{}{"https://download.example.com/document.pdf"},
		},
	}

	require.Equal(t, "https://download.example.com/document.pdf", findDownloadURL(result))
}
