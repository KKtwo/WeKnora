package handler

import (
	"net/http"
	"time"

	dingtalkConnector "github.com/Tencent/WeKnora/internal/datasource/connector/dingtalk"
	feishuConnector "github.com/Tencent/WeKnora/internal/datasource/connector/feishu"
	weknoraErrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type dataSourceOAuthAuthorizeRequest struct {
	ConnectorType string   `json:"connector_type" binding:"required"`
	ClientID      string   `json:"client_id" binding:"required"`
	RedirectURI   string   `json:"redirect_uri" binding:"required"`
	State         string   `json:"state" binding:"required"`
	TargetCorpID  string   `json:"target_corp_id,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

type dataSourceOAuthExchangeRequest struct {
	ConnectorType string `json:"connector_type" binding:"required"`
	ClientID      string `json:"client_id" binding:"required"`
	ClientSecret  string `json:"client_secret" binding:"required"`
	Code          string `json:"code" binding:"required"`
	RedirectURI   string `json:"redirect_uri,omitempty"`
}

// BuildOAuthAuthorizationURL returns only a browser-safe consent URL. OAuth
// state generation and verification remains the caller's responsibility.
func (h *DataSourceHandler) BuildOAuthAuthorizationURL(c *gin.Context) {
	var req dataSourceOAuthAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(weknoraErrors.NewBadRequestError(err.Error()))
		return
	}
	var (
		authURL string
		err     error
	)
	switch req.ConnectorType {
	case types.ConnectorTypeFeishu:
		authURL, err = feishuConnector.BuildAuthorizationURL(
			req.ClientID, req.RedirectURI, req.State, req.Scopes,
		)
	case types.ConnectorTypeDingTalk:
		authURL, err = dingtalkConnector.BuildAuthorizationURL(
			req.ClientID, req.RedirectURI, req.State, req.TargetCorpID,
		)
	default:
		err = weknoraErrors.NewBadRequestError("connector does not support OAuth")
	}
	if err != nil {
		c.Error(weknoraErrors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"authorization_url": authURL})
}

// ExchangeOAuthCode normalizes connector tokens into the credential map used
// by the encrypted /credentials subresource. The management plane must persist
// the map immediately and must not forward it to a browser.
func (h *DataSourceHandler) ExchangeOAuthCode(c *gin.Context) {
	var req dataSourceOAuthExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(weknoraErrors.NewBadRequestError(err.Error()))
		return
	}
	var (
		credentials map[string]interface{}
		err         error
	)
	switch req.ConnectorType {
	case types.ConnectorTypeFeishu:
		if req.RedirectURI == "" {
			c.Error(weknoraErrors.NewBadRequestError("redirect_uri is required for Feishu OAuth"))
			return
		}
		token, exchangeErr := feishuConnector.ExchangeOAuthCode(
			c.Request.Context(), nil,
			&feishuConnector.Config{AppID: req.ClientID, AppSecret: req.ClientSecret},
			req.Code, req.RedirectURI,
		)
		err = exchangeErr
		if token != nil {
			credentials = map[string]interface{}{
				"auth_mode":                feishuConnector.AuthModeUserOAuth,
				"app_id":                   req.ClientID,
				"app_secret":               req.ClientSecret,
				"access_token":             token.AccessToken,
				"refresh_token":            token.RefreshToken,
				"access_token_expires_at":  token.AccessTokenExpiresAt.Format(time.RFC3339),
				"refresh_token_expires_at": token.RefreshTokenExpiresAt.Format(time.RFC3339),
			}
		}
	case types.ConnectorTypeDingTalk:
		token, exchangeErr := dingtalkConnector.ExchangeOAuthCode(
			c.Request.Context(), nil,
			&dingtalkConnector.Config{ClientID: req.ClientID, ClientSecret: req.ClientSecret},
			req.Code,
		)
		err = exchangeErr
		if token != nil {
			credentials = map[string]interface{}{
				"client_id":                req.ClientID,
				"client_secret":            req.ClientSecret,
				"access_token":             token.AccessToken,
				"refresh_token":            token.RefreshToken,
				"access_token_expires_at":  token.AccessTokenExpiresAt.Format(time.RFC3339),
				"refresh_token_expires_at": token.RefreshTokenExpiresAt.Format(time.RFC3339),
				"corp_id":                  token.CorpID,
			}
		}
	default:
		err = weknoraErrors.NewBadRequestError("connector does not support OAuth")
	}
	if err != nil {
		c.Error(weknoraErrors.NewBadRequestError("OAuth exchange failed: " + err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"credentials": credentials})
}
