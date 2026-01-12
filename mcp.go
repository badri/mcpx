package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var defaultHeaders = map[string]string{
	"Content-Type": "application/json",
	"Accept":       "application/json, text/event-stream",
}

// HTTPClient wraps http.Client with MCP-specific functionality
type HTTPClient struct {
	client     *http.Client
	transport  *http.Transport
	timeout    time.Duration
	persistent bool
	mu         sync.Mutex
}

// NewHTTPClient creates a new HTTP client
func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

// NewPersistentHTTPClient creates an HTTP client that maintains persistent connections
// for session-based MCP servers (like Playwright MCP using Streamable HTTP).
func NewPersistentHTTPClient(timeout time.Duration) *HTTPClient {
	// Create a transport that keeps connections alive
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          1,
		MaxIdleConnsPerHost:   1,
		MaxConnsPerHost:       1, // Force single connection for session affinity
		IdleConnTimeout:       0, // Never timeout idle connections
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     false, // Use HTTP/1.1 for simpler connection management
		ResponseHeaderTimeout: timeout,
	}

	return &HTTPClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   0, // No overall timeout; use transport-level timeouts
		},
		transport:  transport,
		timeout:    timeout,
		persistent: true,
	}
}

// Close closes idle connections (for persistent clients)
func (h *HTTPClient) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.transport != nil {
		h.transport.CloseIdleConnections()
	}
}

// parseSSEResponse extracts JSON data from an SSE response
func parseSSEResponse(text string) (*MCPResponse, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr != "" {
				var resp MCPResponse
				if err := json.Unmarshal([]byte(dataStr), &resp); err == nil {
					return &resp, nil
				}
			}
		}
	}

	// Try parsing as plain JSON
	var resp MCPResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// MCPClient handles MCP protocol communication
type MCPClient struct {
	httpClient  *HTTPClient
	config      ServerConfig
	serverName  string
	sessionID   string
	oauthToken  string
	persistent  bool
	initialized bool
	mu          sync.Mutex
}

// NewMCPClient creates a new MCP client for a server
func NewMCPClient(serverName string, config ServerConfig) *MCPClient {
	var httpClient *HTTPClient
	if config.SessionBased {
		httpClient = NewPersistentHTTPClient(30 * time.Second)
	} else {
		httpClient = NewHTTPClient(30 * time.Second)
	}

	return &MCPClient{
		httpClient: httpClient,
		config:     config,
		serverName: serverName,
		persistent: config.SessionBased,
	}
}

// Close closes the underlying HTTP client connections
func (c *MCPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.httpClient != nil {
		c.httpClient.Close()
	}
	c.initialized = false
	c.sessionID = ""
}

// IsPersistent returns whether this client uses persistent connections
func (c *MCPClient) IsPersistent() bool {
	return c.persistent
}

// SetOAuthToken sets the OAuth token for requests
func (c *MCPClient) SetOAuthToken(token string) {
	c.oauthToken = token
}

// SetSessionID sets the session ID for requests
func (c *MCPClient) SetSessionID(id string) {
	c.sessionID = id
}

// Request makes an MCP JSON-RPC request
func (c *MCPClient) Request(method string, params any) (*MCPResponse, string, error) {
	payload := MCPRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      uuid.New().String(),
		Params:  params,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.config.URL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}

	// Set server-specific headers
	for k, v := range c.config.Headers {
		req.Header.Set(k, v)
	}

	// Set OAuth token if available (overrides static headers)
	if c.oauthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.oauthToken)
	}

	// Set session ID if available
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Extract session ID from response headers
	newSessionID := resp.Header.Get("Mcp-Session-Id")

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newSessionID, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response (might be SSE or JSON)
	contentType := resp.Header.Get("Content-Type")
	var mcpResp *MCPResponse

	if strings.Contains(contentType, "text/event-stream") {
		mcpResp, err = parseSSEResponse(string(respBody))
	} else {
		err = json.Unmarshal(respBody, &mcpResp)
	}

	if err != nil {
		return nil, newSessionID, fmt.Errorf("failed to parse response: %w", err)
	}

	return mcpResp, newSessionID, nil
}

// Initialize establishes an MCP session
func (c *MCPClient) Initialize() error {
	// For session-based servers (Streamable HTTP), skip session cache lookup.
	// The session is tied to the TCP connection, so cached session IDs are invalid.
	if !c.config.SessionBased {
		// Check if we have a cached session
		sessions, err := LoadSessions()
		if err == nil {
			if sessionID, ok := sessions[c.serverName]; ok {
				c.sessionID = sessionID
				return nil
			}
		}
	}

	// Initialize new session
	resp, sessionID, err := c.Request("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "mcpx",
			"version": "0.1.0",
		},
	})

	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize failed: %s", resp.Error.Message)
	}

	// Save session ID if we got one (skip for session-based servers)
	if sessionID != "" {
		c.sessionID = sessionID
		if !c.config.SessionBased {
			sessions, _ := LoadSessions()
			if sessions == nil {
				sessions = make(map[string]string)
			}
			sessions[c.serverName] = sessionID
			SaveSessions(sessions)
		}
	}

	return nil
}

// ListTools retrieves available tools from the server
func (c *MCPClient) ListTools() ([]Tool, error) {
	if err := c.Initialize(); err != nil {
		return nil, err
	}

	resp, _, err := c.Request("tools/list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools failed: %s", resp.Error.Message)
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("unexpected response format")
	}

	// Extract tools from result
	toolsRaw, ok := resp.Result["tools"]
	if !ok {
		return nil, fmt.Errorf("no tools in response")
	}

	// Convert to []Tool
	toolsJSON, err := json.Marshal(toolsRaw)
	if err != nil {
		return nil, err
	}

	var rawTools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(toolsJSON, &rawTools); err != nil {
		return nil, err
	}

	tools := make([]Tool, len(rawTools))
	for i, t := range rawTools {
		tools[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		}
	}

	return tools, nil
}

// CallTool invokes a tool on the server
func (c *MCPClient) CallTool(toolName string, arguments map[string]any) (map[string]any, error) {
	if err := c.Initialize(); err != nil {
		return nil, err
	}

	resp, _, err := c.Request("tools/call", map[string]any{
		"name":      toolName,
		"arguments": arguments,
	})

	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tool call failed: %s", resp.Error.Message)
	}

	return resp.Result, nil
}

// GetTokenForServer retrieves the OAuth token for a server, refreshing if needed
func GetTokenForServer(serverName string, serverConfig ServerConfig) (string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return "", nil // No tokens, not an error
	}

	tokenData, ok := tokens[serverName]
	if !ok {
		return "", nil // No token for this server
	}

	// Check if token is expired
	if tokenData.ExpiresAt > 0 {
		if float64(time.Now().Unix()) > tokenData.ExpiresAt-60 { // 60s buffer
			// Try to refresh
			if tokenData.RefreshToken != "" {
				newToken, err := RefreshOAuthToken(serverName, serverConfig, tokenData)
				if err != nil || newToken == "" {
					return "", nil // Refresh failed, need re-auth
				}
				return newToken, nil
			}
			return "", nil // Token expired, no refresh token
		}
	}

	return tokenData.AccessToken, nil
}

// RefreshOAuthToken refreshes an expired OAuth token
func RefreshOAuthToken(serverName string, serverConfig ServerConfig, tokenData TokenData) (string, error) {
	if serverConfig.OAuth == nil || serverConfig.OAuth.TokenURL == "" {
		return "", fmt.Errorf("no token URL configured")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	data := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s&client_id=%s",
		tokenData.RefreshToken,
		getClientID(serverConfig))

	req, err := http.NewRequest("POST", serverConfig.OAuth.TokenURL, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token refresh failed: %d", resp.StatusCode)
	}

	var newTokenData TokenData
	if err := json.NewDecoder(resp.Body).Decode(&newTokenData); err != nil {
		return "", err
	}

	// Calculate expiry time
	if newTokenData.ExpiresIn > 0 {
		newTokenData.ExpiresAt = float64(time.Now().Unix()) + float64(newTokenData.ExpiresIn)
	}

	// Preserve refresh token if not returned
	if newTokenData.RefreshToken == "" {
		newTokenData.RefreshToken = tokenData.RefreshToken
	}

	// Save updated token
	tokens, _ := LoadTokens()
	if tokens == nil {
		tokens = make(map[string]TokenData)
	}
	tokens[serverName] = newTokenData
	SaveTokens(tokens)

	return newTokenData.AccessToken, nil
}

func getClientID(config ServerConfig) string {
	if config.OAuth != nil && config.OAuth.ClientID != "" {
		return config.OAuth.ClientID
	}
	return "mcpx"
}
