package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	callbackPort = 8085
	redirectURI  = "http://localhost:8085/callback"
)

// OAuthDiscovery holds discovered OAuth endpoints
type OAuthDiscovery struct {
	AuthURL         string   `json:"auth_url"`
	TokenURL        string   `json:"token_url"`
	RegistrationURL string   `json:"registration_url"`
	Scopes          []string `json:"scopes"`
	Resource        string   `json:"resource"`
}

// generatePKCE creates a code verifier and challenge
func generatePKCE() (verifier, challenge string, err error) {
	// Generate 32 bytes of random data
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	verifier = base64.RawURLEncoding.EncodeToString(b)

	// SHA256 hash the verifier
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

// generateState creates a random state for CSRF protection
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// discoverOAuthEndpoints discovers OAuth endpoints from an MCP server (RFC 9728)
func discoverOAuthEndpoints(serverURL string) (*OAuthDiscovery, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	baseURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Printf("Discovering OAuth configuration for %s...\n", baseURL)

	// Try to get protected resource metadata
	wellKnownURLs := []string{
		fmt.Sprintf("%s/.well-known/oauth-protected-resource%s", baseURL, parsed.Path),
		fmt.Sprintf("%s/.well-known/oauth-protected-resource", baseURL),
	}

	var resourceMetadata map[string]any
	for _, wkURL := range wellKnownURLs {
		req, _ := http.NewRequest("GET", wkURL, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			if err := json.NewDecoder(resp.Body).Decode(&resourceMetadata); err == nil {
				fmt.Printf("  Found resource metadata at %s\n", wkURL)
				break
			}
		}
	}

	if resourceMetadata == nil {
		// Try getting 401 to extract WWW-Authenticate
		payload := `{"jsonrpc": "2.0", "method": "initialize", "id": "1"}`
		req, _ := http.NewRequest("POST", serverURL, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 401 {
				wwwAuth := resp.Header.Get("WWW-Authenticate")
				if strings.Contains(wwwAuth, "resource_metadata=") {
					re := regexp.MustCompile(`resource_metadata="([^"]+)"`)
					if matches := re.FindStringSubmatch(wwwAuth); len(matches) > 1 {
						metaReq, _ := http.NewRequest("GET", matches[1], nil)
						metaResp, err := client.Do(metaReq)
						if err == nil {
							defer metaResp.Body.Close()
							if metaResp.StatusCode == 200 {
								json.NewDecoder(metaResp.Body).Decode(&resourceMetadata)
							}
						}
					}
				}
			}
		}
	}

	if resourceMetadata == nil {
		fmt.Println("  Could not discover OAuth metadata")
		return nil, fmt.Errorf("could not discover OAuth metadata")
	}

	// Get authorization server from resource metadata
	authServers, ok := resourceMetadata["authorization_servers"].([]any)
	if !ok || len(authServers) == 0 {
		fmt.Println("  No authorization servers found in metadata")
		return nil, fmt.Errorf("no authorization servers in metadata")
	}

	authServerIssuer, ok := authServers[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid authorization server")
	}
	fmt.Printf("  Authorization server: %s\n", authServerIssuer)

	// Discover auth server endpoints
	parsedIssuer, err := url.Parse(authServerIssuer)
	if err != nil {
		return nil, err
	}

	authWellKnownURLs := []string{
		fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server%s", parsedIssuer.Scheme, parsedIssuer.Host, parsedIssuer.Path),
		fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server", parsedIssuer.Scheme, parsedIssuer.Host),
		fmt.Sprintf("%s://%s/.well-known/openid-configuration", parsedIssuer.Scheme, parsedIssuer.Host),
	}

	var authMetadata map[string]any
	for _, wkURL := range authWellKnownURLs {
		req, _ := http.NewRequest("GET", wkURL, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			if err := json.NewDecoder(resp.Body).Decode(&authMetadata); err == nil {
				fmt.Println("  Found auth server metadata")
				break
			}
		}
	}

	if authMetadata == nil {
		fmt.Println("  Could not discover auth server metadata")
		return nil, fmt.Errorf("could not discover auth server metadata")
	}

	discovery := &OAuthDiscovery{
		Resource: serverURL,
	}

	if v, ok := authMetadata["authorization_endpoint"].(string); ok {
		discovery.AuthURL = v
	}
	if v, ok := authMetadata["token_endpoint"].(string); ok {
		discovery.TokenURL = v
	}
	if v, ok := authMetadata["registration_endpoint"].(string); ok {
		discovery.RegistrationURL = v
	}
	if v, ok := resourceMetadata["scopes_supported"].([]any); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				discovery.Scopes = append(discovery.Scopes, str)
			}
		}
	}

	return discovery, nil
}

// doDynamicClientRegistration registers a client dynamically (RFC 7591)
func doDynamicClientRegistration(registrationURL, redirectURI, scopes string) (*ClientRegistration, error) {
	fmt.Println("Performing dynamic client registration...")

	client := &http.Client{Timeout: 30 * time.Second}

	regData := map[string]any{
		"client_name":    "mcpx",
		"redirect_uris":  []string{redirectURI},
		"grant_types":    []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"},
	}
	if scopes != "" {
		regData["scope"] = scopes
	}

	body, _ := json.Marshal(regData)
	req, _ := http.NewRequest("POST", registrationURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	fmt.Printf("  Registered client: %s\n", result.ClientID)
	return &ClientRegistration{
		ClientID:     result.ClientID,
		ClientSecret: result.ClientSecret,
	}, nil
}

// OAuthCallbackServer handles the OAuth callback
type OAuthCallbackServer struct {
	server   *http.Server
	authCode string
	state    string
	err      string
	done     chan struct{}
}

func newOAuthCallbackServer() *OAuthCallbackServer {
	s := &OAuthCallbackServer{
		done: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", callbackPort),
		Handler: mux,
	}

	return s
}

func (s *OAuthCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if code := query.Get("code"); code != "" {
		s.authCode = code
		s.state = query.Get("state")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		w.Write([]byte(`
			<html><body style="font-family: system-ui; text-align: center; padding: 50px;">
			<h1>Authorization Successful!</h1>
			<p>You can close this window and return to your terminal.</p>
			</body></html>
		`))
	} else if errMsg := query.Get("error"); errMsg != "" {
		s.err = errMsg
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(400)
		w.Write([]byte(fmt.Sprintf(`
			<html><body style="font-family: system-ui; text-align: center; padding: 50px;">
			<h1>Authorization Failed</h1>
			<p>Error: %s</p>
			</body></html>
		`, errMsg)))
	} else {
		w.WriteHeader(404)
	}

	// Signal completion
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(s.done)
	}()
}

func (s *OAuthCallbackServer) start() {
	go s.server.ListenAndServe()
}

func (s *OAuthCallbackServer) waitForCallback(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case <-s.done:
	case <-ctx.Done():
	}

	s.server.Shutdown(context.Background())
}

// openBrowser opens a URL in the default browser
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

// DoOAuthFlow performs the full OAuth authorization flow
func DoOAuthFlow(serverName string, serverConfig ServerConfig) error {
	var discovery *OAuthDiscovery
	var err error

	// Try auto-discovery if no oauth config
	if serverConfig.OAuth == nil || serverConfig.OAuth.AuthURL == "" {
		fmt.Println("No OAuth config found, attempting auto-discovery...")
		discovery, err = discoverOAuthEndpoints(serverConfig.URL)
		if err != nil {
			fmt.Printf("Error: Could not discover OAuth endpoints for '%s'\n", serverName)
			fmt.Println("Add 'oauth' section to server config with auth_url, token_url")
			return err
		}
	} else {
		discovery = &OAuthDiscovery{
			AuthURL:         serverConfig.OAuth.AuthURL,
			TokenURL:        serverConfig.OAuth.TokenURL,
			RegistrationURL: serverConfig.OAuth.RegistrationURL,
			Scopes:          serverConfig.OAuth.Scopes,
			Resource:        serverConfig.URL,
		}
	}

	if discovery.AuthURL == "" || discovery.TokenURL == "" {
		return fmt.Errorf("OAuth config requires auth_url and token_url")
	}

	// Determine scope
	scope := ""
	if serverConfig.OAuth != nil && serverConfig.OAuth.Scope != "" {
		scope = serverConfig.OAuth.Scope
	} else if serverConfig.Scope != "" {
		scope = serverConfig.Scope
	} else if len(discovery.Scopes) > 0 {
		scope = strings.Join(discovery.Scopes, " ")
	}

	// Get or create client credentials
	var clientID, clientSecret string
	if serverConfig.OAuth != nil {
		clientID = serverConfig.OAuth.ClientID
		clientSecret = serverConfig.OAuth.ClientSecret
	}

	if clientID == "" {
		// Check for saved registration
		regs, _ := LoadRegistrations()
		if reg, ok := regs[serverName]; ok {
			clientID = reg.ClientID
			clientSecret = reg.ClientSecret
		}
	}

	if clientID == "" && discovery.RegistrationURL != "" {
		// Try dynamic registration
		reg, err := doDynamicClientRegistration(discovery.RegistrationURL, redirectURI, scope)
		if err != nil {
			fmt.Printf("Dynamic registration failed: %v\n", err)
		} else {
			clientID = reg.ClientID
			clientSecret = reg.ClientSecret
			SaveRegistration(serverName, *reg)
		}
	}

	if clientID == "" {
		return fmt.Errorf("no client_id and dynamic registration failed")
	}

	// Generate PKCE
	codeVerifier, codeChallenge, err := generatePKCE()
	if err != nil {
		return err
	}

	// Generate state
	state, err := generateState()
	if err != nil {
		return err
	}

	// Build auth URL
	authParams := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {discovery.Resource},
	}
	if scope != "" {
		authParams.Set("scope", scope)
	}

	fullAuthURL := discovery.AuthURL + "?" + authParams.Encode()

	// Start callback server
	callbackServer := newOAuthCallbackServer()
	callbackServer.start()

	// Open browser
	fmt.Println("Opening browser for authorization...")
	fmt.Printf("If browser doesn't open, visit: %s\n", fullAuthURL)
	openBrowser(fullAuthURL)

	// Wait for callback (2 minute timeout)
	callbackServer.waitForCallback(2 * time.Minute)

	if callbackServer.err != "" {
		return fmt.Errorf("authorization error: %s", callbackServer.err)
	}

	if callbackServer.authCode == "" {
		return fmt.Errorf("authorization timed out or was cancelled")
	}

	if callbackServer.state != state {
		return fmt.Errorf("state mismatch - possible CSRF attack")
	}

	// Exchange code for token
	fmt.Println("Exchanging authorization code for token...")

	tokenData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {callbackServer.authCode},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {codeVerifier},
	}
	if clientSecret != "" {
		tokenData.Set("client_secret", clientSecret)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("POST", discovery.TokenURL, strings.NewReader(tokenData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token exchange failed: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp TokenData
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	// Calculate expiry time
	if tokenResp.ExpiresIn > 0 {
		tokenResp.ExpiresAt = float64(time.Now().Unix()) + float64(tokenResp.ExpiresIn)
	}

	// Save token
	tokens, _ := LoadTokens()
	if tokens == nil {
		tokens = make(map[string]TokenData)
	}
	tokens[serverName] = tokenResp
	if err := SaveTokens(tokens); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Printf("Authorization successful! Token saved for '%s'\n", serverName)
	return nil
}
