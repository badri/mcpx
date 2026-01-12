package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestConfig(t *testing.T) (string, func()) {
	t.Helper()
	// Create temp directory for test config
	tmpDir, err := os.MkdirTemp("", "mcpx-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Save original paths
	origConfigDir := ConfigDir
	origConfigFile := ConfigFile
	origSessionFile := SessionFile
	origTokensFile := TokensFile
	origRegFile := RegFile

	// Set test paths
	ConfigDir = tmpDir
	ConfigFile = filepath.Join(tmpDir, "servers.json")
	SessionFile = filepath.Join(tmpDir, "sessions.json")
	TokensFile = filepath.Join(tmpDir, "tokens.json")
	RegFile = filepath.Join(tmpDir, "registrations.json")

	return tmpDir, func() {
		// Restore original paths
		ConfigDir = origConfigDir
		ConfigFile = origConfigFile
		SessionFile = origSessionFile
		TokensFile = origTokensFile
		RegFile = origRegFile
		os.RemoveAll(tmpDir)
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Servers == nil {
		t.Error("Expected Servers map to be initialized")
	}

	if len(config.Servers) != 0 {
		t.Errorf("Expected empty servers map, got %d entries", len(config.Servers))
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create config
	config := &Config{
		Servers: map[string]ServerConfig{
			"test-server": {
				URL: "https://example.com/mcp",
				Headers: map[string]string{
					"Authorization": "Bearer test-token",
				},
			},
			"session-server": {
				URL:          "http://localhost:3000/mcp",
				SessionBased: true,
			},
		},
	}

	// Save config
	if err := SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Load config
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify servers
	if len(loaded.Servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(loaded.Servers))
	}

	server, ok := loaded.Servers["test-server"]
	if !ok {
		t.Fatal("Expected test-server to exist")
	}
	if server.URL != "https://example.com/mcp" {
		t.Errorf("Expected URL 'https://example.com/mcp', got '%s'", server.URL)
	}
	if server.Headers["Authorization"] != "Bearer test-token" {
		t.Error("Expected Authorization header to be preserved")
	}

	sessionServer, ok := loaded.Servers["session-server"]
	if !ok {
		t.Fatal("Expected session-server to exist")
	}
	if !sessionServer.SessionBased {
		t.Error("Expected SessionBased to be true")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	// Write invalid JSON
	if err := os.WriteFile(filepath.Join(tmpDir, "servers.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestLoadSessions_NoFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	sessions, err := LoadSessions()
	if err != nil {
		t.Fatalf("LoadSessions failed: %v", err)
	}

	if sessions == nil {
		t.Error("Expected sessions map to be initialized")
	}

	if len(sessions) != 0 {
		t.Errorf("Expected empty sessions, got %d", len(sessions))
	}
}

func TestSaveAndLoadSessions(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	sessions := map[string]string{
		"server1": "session-id-1",
		"server2": "session-id-2",
	}

	if err := SaveSessions(sessions); err != nil {
		t.Fatalf("SaveSessions failed: %v", err)
	}

	loaded, err := LoadSessions()
	if err != nil {
		t.Fatalf("LoadSessions failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(loaded))
	}

	if loaded["server1"] != "session-id-1" {
		t.Errorf("Expected session-id-1, got %s", loaded["server1"])
	}
}

func TestLoadTokens_NoFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	tokens, err := LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens failed: %v", err)
	}

	if tokens == nil {
		t.Error("Expected tokens map to be initialized")
	}

	if len(tokens) != 0 {
		t.Errorf("Expected empty tokens, got %d", len(tokens))
	}
}

func TestSaveAndLoadTokens(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	tokens := map[string]TokenData{
		"server1": {
			AccessToken:  "access-token-1",
			RefreshToken: "refresh-token-1",
			ExpiresAt:    1234567890.0,
			TokenType:    "Bearer",
		},
	}

	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("SaveTokens failed: %v", err)
	}

	loaded, err := LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("Expected 1 token, got %d", len(loaded))
	}

	token, ok := loaded["server1"]
	if !ok {
		t.Fatal("Expected server1 token to exist")
	}

	if token.AccessToken != "access-token-1" {
		t.Errorf("Expected access-token-1, got %s", token.AccessToken)
	}
	if token.RefreshToken != "refresh-token-1" {
		t.Errorf("Expected refresh-token-1, got %s", token.RefreshToken)
	}
}

func TestClearSessions(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Save sessions
	sessions := map[string]string{"server1": "session-1"}
	if err := SaveSessions(sessions); err != nil {
		t.Fatalf("SaveSessions failed: %v", err)
	}

	// Clear sessions
	if err := ClearSessions(); err != nil {
		t.Fatalf("ClearSessions failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(SessionFile); !os.IsNotExist(err) {
		t.Error("Expected sessions file to be deleted")
	}
}

func TestClearSessions_NoFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Should not error when file doesn't exist
	if err := ClearSessions(); err != nil {
		t.Errorf("ClearSessions should not error for missing file: %v", err)
	}
}

func TestClearTokens(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Save tokens
	tokens := map[string]TokenData{
		"server1": {AccessToken: "token-1"},
	}
	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("SaveTokens failed: %v", err)
	}

	// Clear tokens
	if err := ClearTokens(); err != nil {
		t.Fatalf("ClearTokens failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(TokensFile); !os.IsNotExist(err) {
		t.Error("Expected tokens file to be deleted")
	}
}

func TestInitConfig(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	if err := InitConfig(); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	// Verify config file exists
	if _, err := os.Stat(ConfigFile); os.IsNotExist(err) {
		t.Error("Expected config file to be created")
	}

	// Verify content
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Should have example servers
	if len(config.Servers) == 0 {
		t.Error("Expected default servers in config")
	}

	// Verify example server exists
	if _, ok := config.Servers["example"]; !ok {
		t.Error("Expected 'example' server in default config")
	}
}

func TestInitConfig_ExistingFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create existing config
	existingConfig := &Config{
		Servers: map[string]ServerConfig{
			"custom": {URL: "https://custom.example.com"},
		},
	}
	if err := SaveConfig(existingConfig); err != nil {
		t.Fatalf("Failed to create existing config: %v", err)
	}

	// InitConfig should not overwrite
	if err := InitConfig(); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	// Verify original content preserved
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if _, ok := loaded.Servers["custom"]; !ok {
		t.Error("Expected custom server to be preserved")
	}

	if _, ok := loaded.Servers["example"]; ok {
		t.Error("Expected example server NOT to be added to existing config")
	}
}

func TestLoadRegistrations_NoFile(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	regs, err := LoadRegistrations()
	if err != nil {
		t.Fatalf("LoadRegistrations failed: %v", err)
	}

	if regs == nil {
		t.Error("Expected registrations map to be initialized")
	}
}

func TestSaveRegistration(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	reg := ClientRegistration{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	if err := SaveRegistration("test-server", reg); err != nil {
		t.Fatalf("SaveRegistration failed: %v", err)
	}

	// Load and verify
	regs, err := LoadRegistrations()
	if err != nil {
		t.Fatalf("LoadRegistrations failed: %v", err)
	}

	loaded, ok := regs["test-server"]
	if !ok {
		t.Fatal("Expected test-server registration")
	}

	if loaded.ClientID != "test-client-id" {
		t.Errorf("Expected client_id test-client-id, got %s", loaded.ClientID)
	}
}

func TestServerConfigJSON(t *testing.T) {
	config := ServerConfig{
		URL: "https://example.com",
		Headers: map[string]string{
			"X-Custom": "value",
		},
		SessionBased: true,
		OAuth: &OAuthConfig{
			AuthURL:  "https://auth.example.com/authorize",
			TokenURL: "https://auth.example.com/token",
			ClientID: "client-123",
			Scopes:   []string{"read", "write"},
		},
	}

	// Marshal
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal
	var decoded ServerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.URL != config.URL {
		t.Errorf("URL mismatch: %s vs %s", decoded.URL, config.URL)
	}

	if !decoded.SessionBased {
		t.Error("SessionBased should be true")
	}

	if decoded.OAuth == nil {
		t.Fatal("OAuth should not be nil")
	}

	if decoded.OAuth.ClientID != "client-123" {
		t.Errorf("OAuth ClientID mismatch: %s", decoded.OAuth.ClientID)
	}

	if len(decoded.OAuth.Scopes) != 2 {
		t.Errorf("Expected 2 scopes, got %d", len(decoded.OAuth.Scopes))
	}
}
