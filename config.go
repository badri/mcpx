package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Paths
var (
	ConfigDir   = filepath.Join(os.Getenv("HOME"), ".mcpx")
	ConfigFile  = filepath.Join(ConfigDir, "servers.json")
	SessionFile = filepath.Join(ConfigDir, "sessions.json")
	TokensFile  = filepath.Join(ConfigDir, "tokens.json")
	RegFile     = filepath.Join(ConfigDir, "registrations.json")
	SocketPath  = filepath.Join(ConfigDir, "daemon.sock")
	PIDFile     = filepath.Join(ConfigDir, "daemon.pid")
	LogFile     = filepath.Join(ConfigDir, "daemon.log")
)

const (
	ToolsCacheTTL = 300 * time.Second // 5 minutes
)

// ServerConfig represents a configured MCP server
type ServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   *OAuthConfig      `json:"oauth,omitempty"`
	Scope   string            `json:"scope,omitempty"`
}

// OAuthConfig holds OAuth configuration for a server
type OAuthConfig struct {
	AuthURL         string   `json:"auth_url,omitempty"`
	TokenURL        string   `json:"token_url,omitempty"`
	RegistrationURL string   `json:"registration_url,omitempty"`
	ClientID        string   `json:"client_id,omitempty"`
	ClientSecret    string   `json:"client_secret,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	Resource        string   `json:"resource,omitempty"`
}

// Config is the root configuration structure
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// TokenData holds OAuth token information
type TokenData struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token,omitempty"`
	ExpiresIn    int     `json:"expires_in,omitempty"`
	ExpiresAt    float64 `json:"expires_at,omitempty"`
	TokenType    string  `json:"token_type,omitempty"`
}

// ClientRegistration holds dynamic client registration data
type ClientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// MCPRequest is a JSON-RPC request
type MCPRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      string `json:"id"`
	Params  any    `json:"params,omitempty"`
}

// MCPResponse is a JSON-RPC response
type MCPResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      string         `json:"id,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *RPCError      `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool represents an MCP tool
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ServerInfo for listing servers
type ServerInfo struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	HasAuth bool   `json:"has_auth,omitempty"`
}

// LoadConfig loads server configurations
func LoadConfig() (*Config, error) {
	if _, err := os.Stat(ConfigFile); os.IsNotExist(err) {
		return &Config{Servers: make(map[string]ServerConfig)}, nil
	}

	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.Servers == nil {
		config.Servers = make(map[string]ServerConfig)
	}

	return &config, nil
}

// LoadSessions loads persisted session IDs
func LoadSessions() (map[string]string, error) {
	if _, err := os.Stat(SessionFile); os.IsNotExist(err) {
		return make(map[string]string), nil
	}

	data, err := os.ReadFile(SessionFile)
	if err != nil {
		return nil, err
	}

	var sessions map[string]string
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

// SaveSessions persists session IDs
func SaveSessions(sessions map[string]string) error {
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}

	data, err := json.Marshal(sessions)
	if err != nil {
		return err
	}

	return os.WriteFile(SessionFile, data, 0644)
}

// LoadTokens loads OAuth tokens
func LoadTokens() (map[string]TokenData, error) {
	if _, err := os.Stat(TokensFile); os.IsNotExist(err) {
		return make(map[string]TokenData), nil
	}

	data, err := os.ReadFile(TokensFile)
	if err != nil {
		return nil, err
	}

	var tokens map[string]TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}

	return tokens, nil
}

// SaveTokens saves OAuth tokens
func SaveTokens(tokens map[string]TokenData) error {
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(TokensFile, data, 0600); err != nil {
		return err
	}

	return nil
}

// LoadRegistrations loads client registrations
func LoadRegistrations() (map[string]ClientRegistration, error) {
	if _, err := os.Stat(RegFile); os.IsNotExist(err) {
		return make(map[string]ClientRegistration), nil
	}

	data, err := os.ReadFile(RegFile)
	if err != nil {
		return nil, err
	}

	var regs map[string]ClientRegistration
	if err := json.Unmarshal(data, &regs); err != nil {
		return nil, err
	}

	return regs, nil
}

// SaveRegistration saves a client registration
func SaveRegistration(serverName string, reg ClientRegistration) error {
	regs, err := LoadRegistrations()
	if err != nil {
		regs = make(map[string]ClientRegistration)
	}

	regs[serverName] = reg

	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(regs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(RegFile, data, 0600)
}

// InitConfig creates the config directory and default config file
func InitConfig() error {
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}

	if _, err := os.Stat(ConfigFile); err == nil {
		return nil // Already exists
	}

	defaultConfig := Config{
		Servers: map[string]ServerConfig{
			"example": {
				URL: "https://mcp.example.com",
				Headers: map[string]string{
					"Authorization": "Bearer YOUR_TOKEN",
				},
			},
		},
	}

	data, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ConfigFile, data, 0644)
}

// ClearSessions removes the sessions file
func ClearSessions() error {
	if _, err := os.Stat(SessionFile); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(SessionFile)
}

// ClearTokens removes the tokens file
func ClearTokens() error {
	if _, err := os.Stat(TokensFile); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(TokensFile)
}

// Claude Code skill paths
var (
	ClaudeSkillsDir  = filepath.Join(os.Getenv("HOME"), ".claude", "skills")
	ClaudeSkillFile  = filepath.Join(ClaudeSkillsDir, "mcpx.md")
)

// mcpxSkillContent is the Claude Code skill for MCP server access
const mcpxSkillContent = `---
name: mcpx
description: Query MCP servers (databases, logs, APIs). Matches requests about Supabase, BetterStack, database queries, log searches, or any configured MCP server.
user-invocable: true
---

# MCP Server Access via mcpx

Use this skill when the user wants to interact with MCP servers - databases, logging services, APIs, or any tool accessible via MCP protocol.

## Prerequisites

- mcpx daemon must be running (` + "`mcpx --daemon`" + `)
- Servers configured in ` + "`~/.mcpx/servers.json`" + `

## Workflow

### 1. Discover available servers

` + "```bash\nmcpx --servers\n```" + `

### 2. Explore tools on a server

` + "```bash\nmcpx --daemon-tools <server-name>\n```" + `

### 3. Execute queries via subagent

**Important:** Spawn a subagent for actual queries to keep the main context clean.

` + "```" + `
Use the Task tool with subagent_type="general-purpose" to run:
mcpx --query <server> <tool> '{"arg": "value"}'
` + "```" + `

### 4. Return results

Parse the JSON response and return a human-readable summary to the user. Only include relevant data, not raw JSON unless requested.

## Example

User: "Check my Supabase for users created today"

1. Confirm server exists: ` + "`mcpx --servers`" + `
2. Find the right tool: ` + "`mcpx --daemon-tools supabase`" + `
3. Spawn subagent to run: ` + "`mcpx --query supabase execute_sql '{\"query\": \"SELECT * FROM users WHERE created_at > CURRENT_DATE\"}'`" + `
4. Return: "Found 12 users created today: [summary]"

## Error Handling

- If daemon not running: Tell user to run ` + "`mcpx --daemon`" + `
- If server not found: Show available servers from ` + "`mcpx --servers`" + `
- If tool not found: Show available tools from ` + "`mcpx --daemon-tools <server>`" + `

## Model

Use ` + "`haiku`" + ` for simple queries to reduce cost. Use ` + "`sonnet`" + ` for complex multi-step operations.
`

// InitSkill installs the mcpx skill for Claude Code
func InitSkill() error {
	// Create skills directory
	if err := os.MkdirAll(ClaudeSkillsDir, 0755); err != nil {
		return err
	}

	// Write skill file
	return os.WriteFile(ClaudeSkillFile, []byte(mcpxSkillContent), 0644)
}
