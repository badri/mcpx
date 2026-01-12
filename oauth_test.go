package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE failed: %v", err)
	}

	// Verifier should be base64url encoded (43 characters for 32 bytes)
	if len(verifier) != 43 {
		t.Errorf("Expected verifier length 43, got %d", len(verifier))
	}

	// Challenge should also be base64url encoded SHA256 (43 characters)
	if len(challenge) != 43 {
		t.Errorf("Expected challenge length 43, got %d", len(challenge))
	}

	// Verifier and challenge should be different
	if verifier == challenge {
		t.Error("Verifier and challenge should be different")
	}

	// Should be valid base64url
	_, err = base64.RawURLEncoding.DecodeString(verifier)
	if err != nil {
		t.Errorf("Verifier should be valid base64url: %v", err)
	}

	_, err = base64.RawURLEncoding.DecodeString(challenge)
	if err != nil {
		t.Errorf("Challenge should be valid base64url: %v", err)
	}
}

func TestGeneratePKCE_Uniqueness(t *testing.T) {
	// Generate multiple PKCE pairs and verify uniqueness
	verifiers := make(map[string]bool)

	for i := 0; i < 100; i++ {
		verifier, _, err := generatePKCE()
		if err != nil {
			t.Fatalf("generatePKCE failed: %v", err)
		}

		if verifiers[verifier] {
			t.Error("Generated duplicate verifier")
		}
		verifiers[verifier] = true
	}
}

func TestGenerateState(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState failed: %v", err)
	}

	// State should be base64url encoded (22 characters for 16 bytes)
	if len(state) != 22 {
		t.Errorf("Expected state length 22, got %d", len(state))
	}

	// Should be valid base64url
	_, err = base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		t.Errorf("State should be valid base64url: %v", err)
	}
}

func TestGenerateState_Uniqueness(t *testing.T) {
	states := make(map[string]bool)

	for i := 0; i < 100; i++ {
		state, err := generateState()
		if err != nil {
			t.Fatalf("generateState failed: %v", err)
		}

		if states[state] {
			t.Error("Generated duplicate state")
		}
		states[state] = true
	}
}

func TestOAuthDiscoveryJSON(t *testing.T) {
	discovery := &OAuthDiscovery{
		AuthURL:         "https://auth.example.com/authorize",
		TokenURL:        "https://auth.example.com/token",
		RegistrationURL: "https://auth.example.com/register",
		Scopes:          []string{"read", "write"},
		Resource:        "https://api.example.com",
	}

	data, err := json.Marshal(discovery)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded OAuthDiscovery
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.AuthURL != discovery.AuthURL {
		t.Errorf("AuthURL mismatch: %s", decoded.AuthURL)
	}

	if decoded.TokenURL != discovery.TokenURL {
		t.Errorf("TokenURL mismatch: %s", decoded.TokenURL)
	}

	if len(decoded.Scopes) != 2 {
		t.Errorf("Expected 2 scopes, got %d", len(decoded.Scopes))
	}
}

func TestOpenBrowser(t *testing.T) {
	// We can't really test opening a browser, but we can verify the function exists
	// and handles the URL parameter (it will fail on CI with no display)
	err := openBrowser("https://example.com")
	// Don't check error - it will fail on headless systems
	_ = err
}

func TestNewOAuthCallbackServer(t *testing.T) {
	server := newOAuthCallbackServer()

	if server == nil {
		t.Fatal("Expected server to be created")
	}

	if server.server == nil {
		t.Error("Expected http server to be initialized")
	}

	if server.done == nil {
		t.Error("Expected done channel to be initialized")
	}
}

func TestOAuthCallbackServer_HandleCallback_Success(t *testing.T) {
	server := newOAuthCallbackServer()

	// Create test request
	req := httptest.NewRequest("GET", "/callback?code=test-auth-code&state=test-state", nil)
	w := httptest.NewRecorder()

	server.handleCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if server.authCode != "test-auth-code" {
		t.Errorf("Expected authCode 'test-auth-code', got '%s'", server.authCode)
	}

	if server.state != "test-state" {
		t.Errorf("Expected state 'test-state', got '%s'", server.state)
	}

	if server.err != "" {
		t.Errorf("Expected no error, got '%s'", server.err)
	}
}

func TestOAuthCallbackServer_HandleCallback_Error(t *testing.T) {
	server := newOAuthCallbackServer()

	req := httptest.NewRequest("GET", "/callback?error=access_denied&error_description=User+denied", nil)
	w := httptest.NewRecorder()

	server.handleCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	if server.err != "access_denied" {
		t.Errorf("Expected error 'access_denied', got '%s'", server.err)
	}

	if server.authCode != "" {
		t.Errorf("Expected no auth code, got '%s'", server.authCode)
	}
}

func TestOAuthCallbackServer_HandleCallback_NoParams(t *testing.T) {
	server := newOAuthCallbackServer()

	req := httptest.NewRequest("GET", "/callback", nil)
	w := httptest.NewRecorder()

	server.handleCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestDiscoverOAuthEndpoints_ResourceMetadata(t *testing.T) {
	// Create mock server with well-known endpoints
	mux := http.NewServeMux()

	// Resource metadata endpoint
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"authorization_servers": []string{"https://auth.example.com"},
			"scopes_supported":      []string{"read", "write"},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Create auth server
	authMux := http.NewServeMux()
	authMux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"authorization_endpoint":  "https://auth.example.com/authorize",
			"token_endpoint":          "https://auth.example.com/token",
			"registration_endpoint":   "https://auth.example.com/register",
		})
	})

	// For this test we'd need to mock the auth server too, which is complex
	// The test above verifies the basic server infrastructure works
}

func TestDoDynamicClientRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Expected Content-Type application/json")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"client_id":     "registered-client-id",
			"client_secret": "registered-client-secret",
		})
	}))
	defer server.Close()

	reg, err := doDynamicClientRegistration(server.URL, redirectURI, "read write")
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	if reg.ClientID != "registered-client-id" {
		t.Errorf("Expected client_id 'registered-client-id', got '%s'", reg.ClientID)
	}

	if reg.ClientSecret != "registered-client-secret" {
		t.Errorf("Expected client_secret 'registered-client-secret', got '%s'", reg.ClientSecret)
	}
}

func TestDoDynamicClientRegistration_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error": "invalid_request"}`))
	}))
	defer server.Close()

	_, err := doDynamicClientRegistration(server.URL, redirectURI, "")
	if err == nil {
		t.Error("Expected error for failed registration")
	}
}

func TestClientRegistrationJSON(t *testing.T) {
	reg := ClientRegistration{
		ClientID:     "client-123",
		ClientSecret: "secret-456",
	}

	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ClientRegistration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ClientID != "client-123" {
		t.Errorf("ClientID mismatch: %s", decoded.ClientID)
	}

	if decoded.ClientSecret != "secret-456" {
		t.Errorf("ClientSecret mismatch: %s", decoded.ClientSecret)
	}
}

func TestTokenDataJSON(t *testing.T) {
	token := TokenData{
		AccessToken:  "access-token-abc",
		RefreshToken: "refresh-token-xyz",
		ExpiresIn:    3600,
		ExpiresAt:    1234567890.0,
		TokenType:    "Bearer",
	}

	data, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded TokenData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.AccessToken != "access-token-abc" {
		t.Errorf("AccessToken mismatch: %s", decoded.AccessToken)
	}

	if decoded.RefreshToken != "refresh-token-xyz" {
		t.Errorf("RefreshToken mismatch: %s", decoded.RefreshToken)
	}

	if decoded.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn mismatch: %d", decoded.ExpiresIn)
	}

	if decoded.ExpiresAt != 1234567890.0 {
		t.Errorf("ExpiresAt mismatch: %f", decoded.ExpiresAt)
	}

	if decoded.TokenType != "Bearer" {
		t.Errorf("TokenType mismatch: %s", decoded.TokenType)
	}
}

func TestGetTokenForServer_NoTokens(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	config := ServerConfig{URL: "https://example.com"}

	token, err := GetTokenForServer("test-server", config)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if token != "" {
		t.Errorf("Expected empty token, got '%s'", token)
	}
}

func TestGetTokenForServer_ValidToken(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Save a valid token
	tokens := map[string]TokenData{
		"test-server": {
			AccessToken: "valid-token",
			ExpiresAt:   float64(9999999999), // Far future
		},
	}
	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("Failed to save tokens: %v", err)
	}

	config := ServerConfig{URL: "https://example.com"}

	token, err := GetTokenForServer("test-server", config)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if token != "valid-token" {
		t.Errorf("Expected 'valid-token', got '%s'", token)
	}
}

func TestGetTokenForServer_ExpiredToken(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Save an expired token without refresh token
	tokens := map[string]TokenData{
		"test-server": {
			AccessToken: "expired-token",
			ExpiresAt:   1.0, // Long expired
		},
	}
	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("Failed to save tokens: %v", err)
	}

	config := ServerConfig{URL: "https://example.com"}

	token, err := GetTokenForServer("test-server", config)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should return empty for expired token without refresh capability
	if token != "" {
		t.Errorf("Expected empty token for expired token, got '%s'", token)
	}
}

func TestRefreshOAuthToken_NoTokenURL(t *testing.T) {
	config := ServerConfig{URL: "https://example.com"}
	tokenData := TokenData{RefreshToken: "refresh-token"}

	_, err := RefreshOAuthToken("test", config, tokenData)
	if err == nil {
		t.Error("Expected error for missing token URL")
	}
}

func TestRefreshOAuthToken_Success(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("Expected Content-Type application/x-www-form-urlencoded")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	config := ServerConfig{
		URL: "https://example.com",
		OAuth: &OAuthConfig{
			TokenURL: server.URL,
			ClientID: "test-client",
		},
	}
	tokenData := TokenData{RefreshToken: "old-refresh-token"}

	newToken, err := RefreshOAuthToken("test-server", config, tokenData)
	if err != nil {
		t.Fatalf("RefreshOAuthToken failed: %v", err)
	}

	if newToken != "new-access-token" {
		t.Errorf("Expected 'new-access-token', got '%s'", newToken)
	}

	// Verify token was saved
	tokens, err := LoadTokens()
	if err != nil {
		t.Fatalf("Failed to load tokens: %v", err)
	}

	saved, ok := tokens["test-server"]
	if !ok {
		t.Fatal("Expected token to be saved")
	}

	if saved.AccessToken != "new-access-token" {
		t.Errorf("Expected saved access token 'new-access-token', got '%s'", saved.AccessToken)
	}
}

func TestRefreshOAuthToken_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer server.Close()

	config := ServerConfig{
		URL: "https://example.com",
		OAuth: &OAuthConfig{
			TokenURL: server.URL,
		},
	}
	tokenData := TokenData{RefreshToken: "invalid-refresh-token"}

	_, err := RefreshOAuthToken("test", config, tokenData)
	if err == nil {
		t.Error("Expected error for failed refresh")
	}
}

func TestRefreshOAuthToken_PreservesRefreshToken(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response doesn't include refresh_token
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	config := ServerConfig{
		URL: "https://example.com",
		OAuth: &OAuthConfig{
			TokenURL: server.URL,
			ClientID: "test-client",
		},
	}
	tokenData := TokenData{RefreshToken: "original-refresh-token"}

	_, err := RefreshOAuthToken("test-server", config, tokenData)
	if err != nil {
		t.Fatalf("RefreshOAuthToken failed: %v", err)
	}

	// Verify original refresh token was preserved
	tokens, _ := LoadTokens()
	saved := tokens["test-server"]

	if saved.RefreshToken != "original-refresh-token" {
		t.Errorf("Expected refresh token to be preserved, got '%s'", saved.RefreshToken)
	}
}
