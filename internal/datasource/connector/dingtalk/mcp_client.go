package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	weknoramcp "github.com/Tencent/WeKnora/internal/mcp"
	"github.com/Tencent/WeKnora/internal/types"
)

type toolAPI interface {
	Call(context.Context, string, string, map[string]interface{}) (map[string]interface{}, error)
	Close() error
}

type mcpToolAPI struct {
	tokens  *tokenProvider
	mu      sync.Mutex
	clients map[string]weknoramcp.MCPClient
}

func newMCPToolAPI(config *Config) toolAPI {
	return &mcpToolAPI{
		tokens:  &tokenProvider{config: config},
		clients: make(map[string]weknoramcp.MCPClient),
	}
}

func endpointFor(service string) (string, error) {
	switch service {
	case "doc":
		return docMCPEndpoint, nil
	case "drive":
		return driveMCPEndpoint, nil
	case "sheet":
		return sheetMCPEndpoint, nil
	case "aitable":
		return aitableMCPEndpoint, nil
	default:
		return "", fmt.Errorf("unsupported dingtalk MCP service: %s", service)
	}
}

func (a *mcpToolAPI) Call(
	ctx context.Context,
	service string,
	tool string,
	args map[string]interface{},
) (map[string]interface{}, error) {
	token, err := a.tokens.token(ctx)
	if err != nil {
		return nil, err
	}
	client, err := a.client(ctx, service, token)
	if err != nil {
		return nil, err
	}
	result, err := client.CallTool(ctx, tool, args)
	if err != nil {
		return nil, fmt.Errorf("call dingtalk %s.%s: %w", service, tool, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("dingtalk %s.%s returned an error", service, tool)
	}
	return decodeToolResult(result.Content)
}

func (a *mcpToolAPI) client(ctx context.Context, service, token string) (weknoramcp.MCPClient, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing := a.clients[service]; existing != nil {
		return existing, nil
	}
	endpoint, err := endpointFor(service)
	if err != nil {
		return nil, err
	}
	svc := &types.MCPService{
		ID:            "dingtalk-" + service,
		Name:          "DingTalk " + service,
		TransportType: types.MCPTransportHTTPStreamable,
		URL:           &endpoint,
		Headers: types.MCPHeaders{
			"x-user-access-token": token,
			"claw-type":           "openClaw",
		},
		AuthConfig:     &types.MCPAuthConfig{AuthType: types.MCPAuthBearer, Token: token},
		AdvancedConfig: &types.MCPAdvancedConfig{Timeout: 60},
	}
	client, err := weknoramcp.NewMCPClient(&weknoramcp.ClientConfig{Service: svc})
	if err != nil {
		return nil, err
	}
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Disconnect()
		return nil, err
	}
	a.clients[service] = client
	return client, nil
}

func (a *mcpToolAPI) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var firstErr error
	for service, client := range a.clients {
		if err := client.Disconnect(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("disconnect dingtalk %s MCP: %w", service, err)
		}
	}
	a.clients = make(map[string]weknoramcp.MCPClient)
	return firstErr
}

func decodeToolResult(items []weknoramcp.ContentItem) (map[string]interface{}, error) {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			texts = append(texts, item.Text)
		}
	}
	if len(texts) == 0 {
		return map[string]interface{}{}, nil
	}
	joined := strings.Join(texts, "\n")
	var object map[string]interface{}
	if err := json.Unmarshal([]byte(joined), &object); err == nil {
		return object, nil
	}
	var array []interface{}
	if err := json.Unmarshal([]byte(joined), &array); err == nil {
		return map[string]interface{}{"items": array}, nil
	}
	return map[string]interface{}{"text": joined}, nil
}
