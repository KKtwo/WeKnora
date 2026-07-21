// Package dingtalk implements a personal-OAuth DingTalk data-source connector.
package dingtalk

import "time"

const (
	defaultAPIBaseURL = "https://api.dingtalk.com"
	authorizeURL      = "https://login.dingtalk.com/oauth2/auth"

	docMCPEndpoint     = "https://mcp-gw.dingtalk.com/server/91e17caf44f6ca1ed9c6ce614221a518ac93300ece63ca8d7e9b133f912e0607"
	driveMCPEndpoint   = "https://mcp-gw.dingtalk.com/server/536f3b329ee774322b14361c666d6e9471e5bbb281b91ded8ca033b3ce7189af"
	sheetMCPEndpoint   = "https://mcp-gw.dingtalk.com/server/f7340bef5170f3baf97815989fc2bff68f4c293be82b2106c5d3e9cbbb14a17f"
	aitableMCPEndpoint = "https://mcp-gw.dingtalk.com/server/5f0d121611f14e878f7d42c3e32bf6c4a790d433066adae38c062a657c397047"
)

type Config struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	AccessToken           string `json:"access_token,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	AccessTokenExpiresAt  string `json:"access_token_expires_at,omitempty"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at,omitempty"`
	CorpID                string `json:"corp_id,omitempty"`
	APIBaseURL            string `json:"api_base_url,omitempty"`

	credentials map[string]interface{}
}

func (c *Config) apiBaseURL() string {
	if c != nil && c.APIBaseURL != "" {
		return c.APIBaseURL
	}
	return defaultAPIBaseURL
}

type oauthToken struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	CorpID                string
}

type cursorState struct {
	Hashes map[string]string `json:"hashes"`
}
