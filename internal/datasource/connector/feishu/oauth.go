package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
)

const feishuAuthorizeURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"

// OAuthToken is the normalized personal token returned by Feishu OAuth v2.
type OAuthToken struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	Scope                 string
}

type oauthTokenResponse struct {
	Code                  int    `json:"code"`
	Message               string `json:"message"`
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
}

// BuildAuthorizationURL creates a Feishu end-user consent URL. State is
// generated and verified by the management plane so this package never owns a
// browser session.
func BuildAuthorizationURL(clientID, redirectURI, state string, scopes []string) (string, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(redirectURI) == "" || strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("client_id, redirect_uri and state are required")
	}
	values := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"state":         {state},
	}
	if len(scopes) > 0 {
		values.Set("scope", strings.Join(scopes, " "))
	}
	return feishuAuthorizeURL + "?" + values.Encode(), nil
}

// ExchangeOAuthCode exchanges a one-time authorization code for personal
// tokens. The caller must persist the returned token through the encrypted
// data-source credential API.
func ExchangeOAuthCode(
	ctx context.Context,
	httpClient *http.Client,
	config *Config,
	code string,
	redirectURI string,
) (*OAuthToken, error) {
	return requestOAuthToken(ctx, httpClient, config, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     config.AppID,
		"client_secret": config.AppSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
	})
}

func requestOAuthToken(
	ctx context.Context,
	httpClient *http.Client,
	config *Config,
	payload map[string]string,
) (*OAuthToken, error) {
	if config == nil || config.AppID == "" || config.AppSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal feishu oauth request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		config.GetBaseURL()+"/open-apis/authen/v2/oauth/token",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create feishu oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if httpClient == nil {
		httpClient = datasource.NewConnectorHTTPClient(30 * time.Second)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request feishu oauth token: %w", err)
	}
	defer resp.Body.Close()

	var result oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode feishu oauth token: %w", err)
	}
	if resp.StatusCode != http.StatusOK || result.Code != 0 || result.AccessToken == "" {
		return nil, fmt.Errorf("feishu oauth failed: status=%d code=%d message=%s", resp.StatusCode, result.Code, result.Message)
	}
	now := time.Now().UTC()
	return &OAuthToken{
		AccessToken:           result.AccessToken,
		RefreshToken:          result.RefreshToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(result.ExpiresIn) * time.Second),
		RefreshTokenExpiresAt: now.Add(time.Duration(result.RefreshTokenExpiresIn) * time.Second),
		Scope:                 result.Scope,
	}, nil
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	if c.config.AuthMode == AuthModeUserOAuth {
		return c.getUserAccessToken(ctx)
	}
	return c.getTenantAccessToken(ctx)
}

func (c *Client) getUserAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	expiresAt, _ := time.Parse(time.RFC3339, c.config.AccessTokenExpiresAt)
	if c.config.AccessToken != "" && (expiresAt.IsZero() || time.Now().UTC().Add(5*time.Minute).Before(expiresAt)) {
		return c.config.AccessToken, nil
	}
	if c.config.RefreshToken == "" {
		return "", fmt.Errorf("feishu personal OAuth access token expired; reconnect required")
	}
	token, err := requestOAuthToken(ctx, c.httpClient, c.config, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     c.config.AppID,
		"client_secret": c.config.AppSecret,
		"refresh_token": c.config.RefreshToken,
	})
	if err != nil {
		return "", err
	}
	c.applyOAuthToken(token)
	return token.AccessToken, nil
}

func (c *Client) applyOAuthToken(token *OAuthToken) {
	if token == nil {
		return
	}
	c.config.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		c.config.RefreshToken = token.RefreshToken
	}
	c.config.AccessTokenExpiresAt = token.AccessTokenExpiresAt.Format(time.RFC3339)
	if !token.RefreshTokenExpiresAt.IsZero() {
		c.config.RefreshTokenExpiresAt = token.RefreshTokenExpiresAt.Format(time.RFC3339)
	}
	if c.config.credentials == nil {
		return
	}
	// The sync service compares this encrypted credential map after fetching
	// and persists it when token rotation changed any value.
	c.config.credentials["access_token"] = c.config.AccessToken
	c.config.credentials["refresh_token"] = c.config.RefreshToken
	c.config.credentials["access_token_expires_at"] = c.config.AccessTokenExpiresAt
	c.config.credentials["refresh_token_expires_at"] = c.config.RefreshTokenExpiresAt
}
