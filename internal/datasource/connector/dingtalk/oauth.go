package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

// BuildAuthorizationURL creates the DingTalk personal consent URL. State must
// be generated and verified by the management plane.
func BuildAuthorizationURL(clientID, redirectURI, state, targetCorpID string) (string, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(redirectURI) == "" || strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("client_id, redirect_uri and state are required")
	}
	values := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid corpid"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	if targetCorpID != "" {
		values.Set("corpId", targetCorpID)
	}
	return authorizeURL + "?" + values.Encode(), nil
}

// ExchangeOAuthCode exchanges a browser authorization code for personal tokens.
func ExchangeOAuthCode(ctx context.Context, client *http.Client, config *Config, code string) (*oauthToken, error) {
	return requestOAuthToken(ctx, client, config, map[string]string{
		"clientId":     config.ClientID,
		"clientSecret": config.ClientSecret,
		"code":         code,
		"grantType":    "authorization_code",
	})
}

func requestOAuthToken(
	ctx context.Context,
	client *http.Client,
	config *Config,
	payload map[string]string,
) (*oauthToken, error) {
	if config == nil || config.ClientID == "" || config.ClientSecret == "" {
		return nil, fmt.Errorf("dingtalk client_id and client_secret are required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal dingtalk OAuth request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		config.apiBaseURL()+"/v1.0/oauth2/userAccessToken",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create dingtalk OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = datasource.NewConnectorHTTPClient(30 * time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request dingtalk OAuth token: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expireIn"`
		ExpiresInAlt int64  `json:"expiresIn"`
		CorpID       string `json:"corpId"`
		Code         string `json:"code"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode dingtalk OAuth token: %w", err)
	}
	if resp.StatusCode != http.StatusOK || result.AccessToken == "" {
		return nil, fmt.Errorf("dingtalk OAuth failed: status=%d code=%s message=%s", resp.StatusCode, result.Code, result.Message)
	}
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = result.ExpiresInAlt
	}
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	now := time.Now().UTC()
	return &oauthToken{
		AccessToken:           result.AccessToken,
		RefreshToken:          result.RefreshToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(expiresIn) * time.Second),
		RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour),
		CorpID:                result.CorpID,
	}, nil
}

func parseConfig(config *types.DataSourceConfig) (*Config, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	body, err := json.Marshal(config.Credentials)
	if err != nil {
		return nil, fmt.Errorf("marshal dingtalk credentials: %w", err)
	}
	var parsed Config
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse dingtalk credentials: %w", err)
	}
	if parsed.ClientID == "" || parsed.ClientSecret == "" {
		return nil, fmt.Errorf("dingtalk client_id and client_secret are required")
	}
	if parsed.AccessToken == "" && parsed.RefreshToken == "" {
		return nil, fmt.Errorf("dingtalk personal OAuth token is required")
	}
	if err := datasource.ValidateConnectorBaseURL(parsed.apiBaseURL()); err != nil {
		return nil, err
	}
	parsed.credentials = config.Credentials
	return &parsed, nil
}

type tokenProvider struct {
	config     *Config
	httpClient *http.Client
	mu         sync.Mutex
}

func (p *tokenProvider) token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	expiresAt, _ := time.Parse(time.RFC3339, p.config.AccessTokenExpiresAt)
	if p.config.AccessToken != "" && (expiresAt.IsZero() || time.Now().UTC().Add(5*time.Minute).Before(expiresAt)) {
		return p.config.AccessToken, nil
	}
	if p.config.RefreshToken == "" {
		return "", fmt.Errorf("dingtalk personal OAuth access token expired; reconnect required")
	}
	token, err := requestOAuthToken(ctx, p.httpClient, p.config, map[string]string{
		"clientId":     p.config.ClientID,
		"clientSecret": p.config.ClientSecret,
		"refreshToken": p.config.RefreshToken,
		"grantType":    "refresh_token",
	})
	if err != nil {
		return "", err
	}
	p.apply(token)
	return token.AccessToken, nil
}

func (p *tokenProvider) apply(token *oauthToken) {
	p.config.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		p.config.RefreshToken = token.RefreshToken
	}
	p.config.AccessTokenExpiresAt = token.AccessTokenExpiresAt.Format(time.RFC3339)
	p.config.RefreshTokenExpiresAt = token.RefreshTokenExpiresAt.Format(time.RFC3339)
	if token.CorpID != "" {
		p.config.CorpID = token.CorpID
	}
	if p.config.credentials == nil {
		return
	}
	p.config.credentials["access_token"] = p.config.AccessToken
	p.config.credentials["refresh_token"] = p.config.RefreshToken
	p.config.credentials["access_token_expires_at"] = p.config.AccessTokenExpiresAt
	p.config.credentials["refresh_token_expires_at"] = p.config.RefreshTokenExpiresAt
	p.config.credentials["corp_id"] = p.config.CorpID
}
