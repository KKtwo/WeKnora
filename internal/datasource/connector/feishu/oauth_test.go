package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildAuthorizationURL(t *testing.T) {
	authURL, err := BuildAuthorizationURL(
		"cli_test",
		"https://cis.example.com/oauth/feishu/callback",
		"signed-state",
		[]string{"wiki:wiki:readonly", "drive:drive:readonly"},
	)
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, "accounts.feishu.cn", parsed.Host)
	require.Equal(t, "cli_test", parsed.Query().Get("client_id"))
	require.Equal(t, "signed-state", parsed.Query().Get("state"))
	require.Equal(t, "wiki:wiki:readonly drive:drive:readonly", parsed.Query().Get("scope"))
}

func TestPersonalOAuthRefreshMutatesEncryptedCredentialMap(t *testing.T) {
	var requestBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/open-apis/authen/v2/oauth/token", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":                     0,
			"access_token":             "new-access",
			"refresh_token":            "new-refresh",
			"expires_in":               7200,
			"refresh_token_expires_in": 2592000,
		})
	}))
	defer server.Close()

	credentials := map[string]interface{}{
		"auth_mode":               AuthModeUserOAuth,
		"app_id":                  "cli_test",
		"app_secret":              "secret",
		"base_url":                server.URL,
		"access_token":            "expired-access",
		"refresh_token":           "old-refresh",
		"access_token_expires_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	config := &Config{
		AuthMode:             AuthModeUserOAuth,
		AppID:                "cli_test",
		AppSecret:            "secret",
		BaseURL:              server.URL,
		AccessToken:          "expired-access",
		RefreshToken:         "old-refresh",
		AccessTokenExpiresAt: credentials["access_token_expires_at"].(string),
		credentials:          credentials,
	}
	client := NewClient(config)

	token, err := client.getAccessToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "new-access", token)
	require.Equal(t, "refresh_token", requestBody["grant_type"])
	require.Equal(t, "old-refresh", requestBody["refresh_token"])
	require.Equal(t, "new-access", credentials["access_token"])
	require.Equal(t, "new-refresh", credentials["refresh_token"])
	require.NotEmpty(t, credentials["access_token_expires_at"])
}
