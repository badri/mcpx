package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDaemonCommandJSON(t *testing.T) {
	cmd := DaemonCommand{
		Action:    "call",
		Server:    "test-server",
		Tool:      "test-tool",
		Arguments: map[string]any{"arg1": "value1", "count": 42},
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded DaemonCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Action != "call" {
		t.Errorf("Expected action 'call', got '%s'", decoded.Action)
	}

	if decoded.Server != "test-server" {
		t.Errorf("Expected server 'test-server', got '%s'", decoded.Server)
	}

	if decoded.Tool != "test-tool" {
		t.Errorf("Expected tool 'test-tool', got '%s'", decoded.Tool)
	}

	if decoded.Arguments["arg1"] != "value1" {
		t.Errorf("Expected arg1 'value1', got '%v'", decoded.Arguments["arg1"])
	}
}

func TestDaemonCommand_MinimalJSON(t *testing.T) {
	// Test minimal command (ping)
	cmd := DaemonCommand{Action: "ping"}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Should omit empty fields
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["action"] != "ping" {
		t.Error("Expected action to be 'ping'")
	}
}

func TestCachedTools(t *testing.T) {
	tools := []Tool{
		{Name: "tool1", Description: "First tool"},
		{Name: "tool2", Description: "Second tool"},
	}

	cached := &CachedTools{
		Tools:   tools,
		Expires: time.Now().Add(5 * time.Minute),
	}

	if len(cached.Tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(cached.Tools))
	}

	if time.Now().After(cached.Expires) {
		t.Error("Cache should not be expired")
	}
}

func TestCachedTools_Expired(t *testing.T) {
	cached := &CachedTools{
		Tools:   []Tool{{Name: "tool1"}},
		Expires: time.Now().Add(-1 * time.Minute), // Expired 1 minute ago
	}

	if time.Now().Before(cached.Expires) {
		t.Error("Cache should be expired")
	}
}

func TestNewMCPDaemon(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create a config first
	config := &Config{
		Servers: map[string]ServerConfig{
			"test": {URL: "https://example.com/mcp"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	if daemon == nil {
		t.Fatal("Expected daemon to be created")
	}

	if daemon.config == nil {
		t.Error("Expected config to be loaded")
	}

	if daemon.clients == nil {
		t.Error("Expected clients map to be initialized")
	}

	if daemon.toolsCache == nil {
		t.Error("Expected toolsCache map to be initialized")
	}

	if !daemon.running {
		t.Error("Expected daemon to be in running state")
	}
}

func TestMCPDaemon_HandleCommand_Ping(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{Action: "ping"})

	if !resp.OK {
		t.Error("Expected OK response for ping")
	}

	if resp.Data != "pong" {
		t.Errorf("Expected 'pong', got '%v'", resp.Data)
	}
}

func TestMCPDaemon_HandleCommand_Servers(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create config with servers
	config := &Config{
		Servers: map[string]ServerConfig{
			"server1": {URL: "https://server1.example.com"},
			"server2": {URL: "https://server2.example.com"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{Action: "servers"})

	if !resp.OK {
		t.Error("Expected OK response for servers")
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("Expected data to be a map")
	}

	servers, ok := data["servers"].([]ServerInfo)
	if !ok {
		t.Fatal("Expected servers in data")
	}

	if len(servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(servers))
	}
}

func TestMCPDaemon_HandleCommand_UnknownAction(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{Action: "invalid-action"})

	if resp.OK {
		t.Error("Expected error response for unknown action")
	}

	if resp.Error == nil {
		t.Fatal("Expected error to be set")
	}

	if resp.Error.Code != ErrUnknownAction {
		t.Errorf("Expected error code %s, got %s", ErrUnknownAction, resp.Error.Code)
	}
}

func TestMCPDaemon_HandleCommand_ToolsWithoutServer(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{Action: "tools"})

	if resp.OK {
		t.Error("Expected error response for tools without server")
	}

	if resp.Error == nil {
		t.Fatal("Expected error to be set")
	}

	if resp.Error.Code != ErrInvalidArgs {
		t.Errorf("Expected error code %s, got %s", ErrInvalidArgs, resp.Error.Code)
	}
}

func TestMCPDaemon_HandleCommand_CallWithoutServer(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{
		Action: "call",
		Tool:   "test-tool",
	})

	if resp.OK {
		t.Error("Expected error response for call without server")
	}

	if resp.Error.Code != ErrInvalidArgs {
		t.Errorf("Expected error code %s, got %s", ErrInvalidArgs, resp.Error.Code)
	}
}

func TestMCPDaemon_HandleCommand_CallWithoutTool(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{
		Action: "call",
		Server: "test-server",
	})

	if resp.OK {
		t.Error("Expected error response for call without tool")
	}

	if resp.Error.Code != ErrInvalidArgs {
		t.Errorf("Expected error code %s, got %s", ErrInvalidArgs, resp.Error.Code)
	}
}

func TestMCPDaemon_HandleCommand_Shutdown(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	if !daemon.running {
		t.Error("Expected daemon to be running initially")
	}

	resp := daemon.handleCommand(DaemonCommand{Action: "shutdown"})

	if !resp.OK {
		t.Error("Expected OK response for shutdown")
	}

	if daemon.running {
		t.Error("Expected daemon to stop running after shutdown")
	}
}

func TestMCPDaemon_HandleCommand_Reload(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create initial config
	config := &Config{
		Servers: map[string]ServerConfig{
			"server1": {URL: "https://server1.example.com"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	// Update config file
	config.Servers["server2"] = ServerConfig{URL: "https://server2.example.com"}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Reload
	resp := daemon.handleCommand(DaemonCommand{Action: "reload"})

	if !resp.OK {
		t.Errorf("Expected OK response for reload: %v", resp.Error)
	}

	// Check that new server is visible
	if _, ok := daemon.config.Servers["server2"]; !ok {
		t.Error("Expected server2 to be in config after reload")
	}
}

func TestMCPDaemon_GetClient_NotConfigured(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	_, err = daemon.getClient("nonexistent-server")
	if err == nil {
		t.Error("Expected error for non-configured server")
	}
}

func TestMCPDaemon_CloseAllClients(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create config
	config := &Config{
		Servers: map[string]ServerConfig{
			"server1": {URL: "https://server1.example.com"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	// Create a client
	client, err := daemon.getClient("server1")
	if err != nil {
		t.Fatalf("getClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	// Add to cache
	daemon.toolsCache["server1"] = &CachedTools{
		Tools:   []Tool{{Name: "tool1"}},
		Expires: time.Now().Add(5 * time.Minute),
	}

	// Close all clients
	daemon.closeAllClients()

	if len(daemon.clients) != 0 {
		t.Error("Expected clients to be cleared")
	}

	if len(daemon.toolsCache) != 0 {
		t.Error("Expected tools cache to be cleared")
	}
}

func TestMCPDaemon_ReloadConfig_RemovesDeletedServer(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create initial config with server
	config := &Config{
		Servers: map[string]ServerConfig{
			"server1": {URL: "https://server1.example.com"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	// Create a client for server1
	_, err = daemon.getClient("server1")
	if err != nil {
		t.Fatalf("getClient failed: %v", err)
	}

	// Add cache entry
	daemon.toolsCache["server1"] = &CachedTools{
		Tools:   []Tool{{Name: "tool1"}},
		Expires: time.Now().Add(5 * time.Minute),
	}

	// Remove server from config
	delete(config.Servers, "server1")
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Reload
	if err := daemon.reloadConfig(); err != nil {
		t.Fatalf("reloadConfig failed: %v", err)
	}

	// Client and cache should be removed
	if _, ok := daemon.clients["server1"]; ok {
		t.Error("Expected server1 client to be removed")
	}

	if _, ok := daemon.toolsCache["server1"]; ok {
		t.Error("Expected server1 cache to be removed")
	}
}

func TestMCPDaemon_ReloadConfig_URLChange(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create initial config
	config := &Config{
		Servers: map[string]ServerConfig{
			"server1": {URL: "https://old.example.com"},
		},
	}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	// Create a client
	_, err = daemon.getClient("server1")
	if err != nil {
		t.Fatalf("getClient failed: %v", err)
	}

	// Change URL
	config.Servers["server1"] = ServerConfig{URL: "https://new.example.com"}
	if err := SaveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Reload
	if err := daemon.reloadConfig(); err != nil {
		t.Fatalf("reloadConfig failed: %v", err)
	}

	// Client should be removed (will be recreated with new URL on next request)
	if _, ok := daemon.clients["server1"]; ok {
		t.Error("Expected client to be removed after URL change")
	}
}

func TestMCPDaemon_HandleCommand_ToolsUnconfiguredServer(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	daemon, err := NewMCPDaemon()
	if err != nil {
		t.Fatalf("NewMCPDaemon failed: %v", err)
	}

	resp := daemon.handleCommand(DaemonCommand{
		Action: "tools",
		Server: "nonexistent",
	})

	if resp.OK {
		t.Error("Expected error for unconfigured server")
	}

	if resp.Error == nil {
		t.Fatal("Expected error to be set")
	}

	if resp.Error.Code != ErrMCPError {
		t.Errorf("Expected error code %s, got %s", ErrMCPError, resp.Error.Code)
	}
}

func TestIsDaemonRunning_NoSocket(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Should return false when socket doesn't exist
	if IsDaemonRunning() {
		t.Error("Expected daemon to not be running when socket doesn't exist")
	}
}
