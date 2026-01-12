package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseSSEResponse_SingleData(t *testing.T) {
	input := `data: {"jsonrpc": "2.0", "id": "1", "result": {"tools": []}}`

	resp, err := parseSSEResponse(input)
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	if resp.ID != "1" {
		t.Errorf("Expected id 1, got %s", resp.ID)
	}

	if resp.Result == nil {
		t.Error("Expected result to be set")
	}
}

func TestParseSSEResponse_MultipleLines(t *testing.T) {
	input := `event: message
data: {"jsonrpc": "2.0", "id": "test-id", "result": {"message": "hello"}}
`

	resp, err := parseSSEResponse(input)
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.ID != "test-id" {
		t.Errorf("Expected id test-id, got %s", resp.ID)
	}
}

func TestParseSSEResponse_PlainJSON(t *testing.T) {
	input := `{"jsonrpc": "2.0", "id": "123", "result": {"status": "ok"}}`

	resp, err := parseSSEResponse(input)
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.ID != "123" {
		t.Errorf("Expected id 123, got %s", resp.ID)
	}
}

func TestParseSSEResponse_Error(t *testing.T) {
	input := `data: {"jsonrpc": "2.0", "id": "1", "error": {"code": -32600, "message": "Invalid Request"}}`

	resp, err := parseSSEResponse(input)
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("Expected error to be set")
	}

	if resp.Error.Code != -32600 {
		t.Errorf("Expected error code -32600, got %d", resp.Error.Code)
	}

	if resp.Error.Message != "Invalid Request" {
		t.Errorf("Expected error message 'Invalid Request', got %s", resp.Error.Message)
	}
}

func TestParseSSEResponse_Invalid(t *testing.T) {
	input := `not valid json or sse`

	_, err := parseSSEResponse(input)
	if err == nil {
		t.Error("Expected error for invalid input")
	}
}

func TestNewHTTPClient(t *testing.T) {
	client := NewHTTPClient(30 * time.Second)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.timeout)
	}

	if client.persistent {
		t.Error("Expected non-persistent client")
	}
}

func TestNewPersistentHTTPClient(t *testing.T) {
	client := NewPersistentHTTPClient(30 * time.Second)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if !client.persistent {
		t.Error("Expected persistent client")
	}

	if client.transport == nil {
		t.Error("Expected transport to be set")
	}

	// Clean up
	client.Close()
}

func TestHTTPClientClose(t *testing.T) {
	client := NewPersistentHTTPClient(30 * time.Second)

	// Should not panic
	client.Close()
	client.Close() // Double close should be safe
}

func TestNewMCPClient(t *testing.T) {
	config := ServerConfig{
		URL: "https://example.com/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer test",
		},
	}

	client := NewMCPClient("test-server", config)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.serverName != "test-server" {
		t.Errorf("Expected serverName test-server, got %s", client.serverName)
	}

	if client.config.URL != "https://example.com/mcp" {
		t.Errorf("Expected URL https://example.com/mcp, got %s", client.config.URL)
	}

	if client.IsPersistent() {
		t.Error("Expected non-persistent client for standard config")
	}

	client.Close()
}

func TestNewMCPClient_SessionBased(t *testing.T) {
	config := ServerConfig{
		URL:          "http://localhost:3000/mcp",
		SessionBased: true,
	}

	client := NewMCPClient("session-server", config)

	if !client.IsPersistent() {
		t.Error("Expected persistent client for session-based config")
	}

	client.Close()
}

func TestMCPClient_SetOAuthToken(t *testing.T) {
	config := ServerConfig{URL: "https://example.com/mcp"}
	client := NewMCPClient("test", config)
	defer client.Close()

	client.SetOAuthToken("test-token-123")

	if client.oauthToken != "test-token-123" {
		t.Errorf("Expected oauthToken to be set, got %s", client.oauthToken)
	}
}

func TestMCPClient_SetSessionID(t *testing.T) {
	config := ServerConfig{URL: "https://example.com/mcp"}
	client := NewMCPClient("test", config)
	defer client.Close()

	client.SetSessionID("session-456")

	if client.sessionID != "session-456" {
		t.Errorf("Expected sessionID to be set, got %s", client.sessionID)
	}
}

func TestMCPClient_Request(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json")
		}

		// Check custom headers
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Expected Authorization header")
		}

		// Read request body
		body, _ := io.ReadAll(r.Body)
		var req MCPRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse request: %v", err)
		}

		if req.Method != "initialize" {
			t.Errorf("Expected method initialize, got %s", req.Method)
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "new-session-id")
		json.NewEncoder(w).Encode(MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
			},
		})
	}))
	defer server.Close()

	config := ServerConfig{
		URL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer test-token",
		},
	}

	client := NewMCPClient("test", config)
	defer client.Close()

	resp, sessionID, err := client.Request("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	})

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response")
	}

	if resp.Result == nil {
		t.Error("Expected result in response")
	}

	if sessionID != "new-session-id" {
		t.Errorf("Expected session ID 'new-session-id', got '%s'", sessionID)
	}
}

func TestMCPClient_Request_SSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(`event: message
data: {"jsonrpc": "2.0", "id": "1", "result": {"tools": []}}
`))
	}))
	defer server.Close()

	config := ServerConfig{URL: server.URL}
	client := NewMCPClient("test", config)
	defer client.Close()

	resp, _, err := client.Request("tools/list", nil)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.Result == nil {
		t.Error("Expected result in response")
	}
}

func TestMCPClient_Request_WithSessionID(t *testing.T) {
	var receivedSessionID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{
			JSONRPC: "2.0",
			ID:      "1",
			Result:  map[string]any{},
		})
	}))
	defer server.Close()

	config := ServerConfig{URL: server.URL}
	client := NewMCPClient("test", config)
	defer client.Close()

	client.SetSessionID("existing-session")

	_, _, err := client.Request("test", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if receivedSessionID != "existing-session" {
		t.Errorf("Expected session ID in request header, got %s", receivedSessionID)
	}
}

func TestMCPClient_Request_WithOAuthToken(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{
			JSONRPC: "2.0",
			ID:      "1",
			Result:  map[string]any{},
		})
	}))
	defer server.Close()

	config := ServerConfig{
		URL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer static-token",
		},
	}
	client := NewMCPClient("test", config)
	defer client.Close()

	// OAuth token should override static header
	client.SetOAuthToken("dynamic-token")

	_, _, err := client.Request("test", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if receivedAuth != "Bearer dynamic-token" {
		t.Errorf("Expected OAuth token to override, got %s", receivedAuth)
	}
}

func TestMCPClient_ListTools(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req MCPRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")

		if req.Method == "initialize" {
			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
				},
			})
		} else if req.Method == "tools/list" {
			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "tool1",
							"description": "First tool",
							"inputSchema": map[string]any{"type": "object"},
						},
						{
							"name":        "tool2",
							"description": "Second tool",
						},
					},
				},
			})
		}
	}))
	defer server.Close()

	config := ServerConfig{URL: server.URL}
	client := NewMCPClient("test", config)
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	if tools[0].Name != "tool1" {
		t.Errorf("Expected tool1, got %s", tools[0].Name)
	}

	if tools[0].Description != "First tool" {
		t.Errorf("Expected description 'First tool', got %s", tools[0].Description)
	}
}

func TestMCPClient_CallTool(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	var receivedToolName string
	var receivedArgs map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req MCPRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")

		if req.Method == "initialize" {
			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{"protocolVersion": "2024-11-05"},
			})
		} else if req.Method == "tools/call" {
			params := req.Params.(map[string]any)
			receivedToolName = params["name"].(string)
			receivedArgs = params["arguments"].(map[string]any)

			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "Result from tool"},
					},
				},
			})
		}
	}))
	defer server.Close()

	config := ServerConfig{URL: server.URL}
	client := NewMCPClient("test", config)
	defer client.Close()

	result, err := client.CallTool("my-tool", map[string]any{"arg1": "value1"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if receivedToolName != "my-tool" {
		t.Errorf("Expected tool name 'my-tool', got '%s'", receivedToolName)
	}

	if receivedArgs["arg1"] != "value1" {
		t.Errorf("Expected arg1 'value1', got '%v'", receivedArgs["arg1"])
	}

	if result == nil {
		t.Error("Expected result")
	}

	if result["content"] == nil {
		t.Error("Expected content in result")
	}
}

func TestMCPClient_Request_Error(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req MCPRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")

		if req.Method == "initialize" {
			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{"protocolVersion": "2024-11-05"},
			})
		} else {
			json.NewEncoder(w).Encode(MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &RPCError{
					Code:    -32601,
					Message: "Method not found",
				},
			})
		}
	}))
	defer server.Close()

	config := ServerConfig{URL: server.URL}
	client := NewMCPClient("test", config)
	defer client.Close()

	_, err := client.CallTool("nonexistent", nil)
	if err == nil {
		t.Error("Expected error for failed tool call")
	}

	if err.Error() != "tool call failed: Method not found" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestToolStructJSON(t *testing.T) {
	tool := Tool{
		Name:        "test-tool",
		Description: "A test tool",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"arg1": map[string]any{"type": "string"},
			},
		},
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Tool
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Name != "test-tool" {
		t.Errorf("Name mismatch: %s", decoded.Name)
	}

	if decoded.Description != "A test tool" {
		t.Errorf("Description mismatch: %s", decoded.Description)
	}
}

func TestMCPRequest_JSON(t *testing.T) {
	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "req-123",
		Params:  map[string]any{"cursor": "abc"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded MCPRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.JSONRPC != "2.0" {
		t.Errorf("JSONRPC mismatch: %s", decoded.JSONRPC)
	}

	if decoded.Method != "tools/list" {
		t.Errorf("Method mismatch: %s", decoded.Method)
	}

	if decoded.ID != "req-123" {
		t.Errorf("ID mismatch: %s", decoded.ID)
	}
}

func TestMCPResponse_JSON(t *testing.T) {
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      "resp-456",
		Result: map[string]any{
			"tools": []string{"tool1", "tool2"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded MCPResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != "resp-456" {
		t.Errorf("ID mismatch: %s", decoded.ID)
	}

	if decoded.Result == nil {
		t.Error("Result should not be nil")
	}
}

func TestGetClientID(t *testing.T) {
	tests := []struct {
		name     string
		config   ServerConfig
		expected string
	}{
		{
			name:     "no oauth config",
			config:   ServerConfig{URL: "https://example.com"},
			expected: "mcpx",
		},
		{
			name: "empty client id",
			config: ServerConfig{
				URL:   "https://example.com",
				OAuth: &OAuthConfig{},
			},
			expected: "mcpx",
		},
		{
			name: "with client id",
			config: ServerConfig{
				URL:   "https://example.com",
				OAuth: &OAuthConfig{ClientID: "custom-client"},
			},
			expected: "custom-client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getClientID(tt.config)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
