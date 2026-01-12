package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// headerFlags allows multiple --header flags
type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

var (
	// Basic commands
	flagServers       = flag.Bool("servers", false, "List configured servers")
	flagTools         = flag.String("tools", "", "List tools on a server")
	flagCall          = flag.Bool("call", false, "Call a tool: --call <server> <tool> '<json>'")
	flagInit          = flag.Bool("init", false, "Initialize config file")
	flagInitSkill     = flag.Bool("init-skill", false, "Install Claude Code skill to ~/.claude/skills/")
	flagClearSessions = flag.Bool("clear-sessions", false, "Clear cached sessions")
	flagClearTokens   = flag.Bool("clear-tokens", false, "Clear stored OAuth tokens")
	flagAuth          = flag.String("auth", "", "OAuth login for a server")

	// Server management
	flagAdd    = flag.Bool("add", false, "Add a server: --add <name> <url>")
	flagHeader headerFlags
	flagRemove = flag.String("remove", "", "Remove a server: --remove <name>")

	// Daemon mode
	flagDaemon           = flag.Bool("daemon", false, "Start daemon in background")
	flagDaemonForeground = flag.Bool("daemon-foreground", false, "Run daemon in foreground (internal)")
	flagDaemonStop       = flag.Bool("daemon-stop", false, "Stop the daemon")
	flagDaemonStatus     = flag.Bool("daemon-status", false, "Check daemon status")
	flagDaemonTools      = flag.String("daemon-tools", "", "List tools via daemon")
	flagQuery            = flag.Bool("query", false, "Fast query via daemon: --query <server> <tool> '<json>'")

	// Process management
	flagStatus = flag.Bool("status", false, "Show running processes")
	flagLogs   = flag.String("logs", "", "Tail logs for a managed server: --logs <server>")
)

func init() {
	flag.Var(&flagHeader, "header", "Header for --add: --header 'Authorization: Bearer TOKEN'")
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `mcpx - MCP protocol bridge for AI agents

Usage:
  mcpx --servers                          # List configured servers
  mcpx --tools <server>                   # List tools on a server
  mcpx --call <server> <tool> '<json>'    # Call a tool
  mcpx --auth <server>                    # OAuth login for a server
  mcpx --init                             # Create config file
  mcpx --init-skill                       # Install Claude Code skill

Server management:
  mcpx --add <name> <url>                 # Add a server
  mcpx --add --header 'Authorization: Bearer TOKEN' <name> <url>
  mcpx --remove <name>                    # Remove a server

Daemon mode (fast queries):
  mcpx --daemon                           # Start daemon + local servers
  mcpx --query <server> <tool> '<json>'   # Fast query via daemon
  mcpx --daemon-tools <server>            # List tools via daemon
  mcpx --daemon-stop                      # Stop daemon + local servers

Process management:
  mcpx --status                           # Show running processes
  mcpx --logs <server>                    # Tail logs for a managed server

Config: ~/.mcpx/servers.json
Logs: ~/.mcpx/logs/<server>.log

Flags:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	// Handle commands
	switch {
	case *flagInit:
		if err := InitConfig(); err != nil {
			errExit(ErrMCPError, fmt.Sprintf("Failed to init config: %v", err))
		}
		fmt.Printf("Config directory: %s\n", ConfigDir)
		if _, err := os.Stat(ConfigFile); err == nil {
			fmt.Printf("Config file exists: %s\n", ConfigFile)
		} else {
			fmt.Printf("Created config file: %s\n", ConfigFile)
		}

	case *flagInitSkill:
		path, err := InitSkill()
		if err != nil {
			errExit(ErrMCPError, fmt.Sprintf("Failed to install skill: %v", err))
		}
		fmt.Printf("Installed Claude Code skill: %s\n", path)

	case *flagClearSessions:
		if err := ClearSessions(); err != nil {
			errExit(ErrMCPError, fmt.Sprintf("Failed to clear sessions: %v", err))
		}
		fmt.Println("Sessions cleared.")

	case *flagClearTokens:
		if err := ClearTokens(); err != nil {
			errExit(ErrMCPError, fmt.Sprintf("Failed to clear tokens: %v", err))
		}
		fmt.Println("OAuth tokens cleared.")

	case *flagServers:
		listServers()

	case *flagAdd:
		args := flag.Args()
		if len(args) < 2 {
			errExit(ErrInvalidArgs, "Usage: --add <name> <url>")
		}
		addServer(args[0], args[1], flagHeader)

	case *flagRemove != "":
		removeServer(*flagRemove)

	case *flagTools != "":
		listTools(*flagTools)

	case *flagAuth != "":
		doAuth(*flagAuth)

	case *flagDaemon:
		startDaemon()

	case *flagDaemonForeground:
		runDaemonForeground()

	case *flagDaemonStop:
		stopDaemon()

	case *flagDaemonStatus:
		daemonStatus()

	case *flagDaemonTools != "":
		daemonTools(*flagDaemonTools)

	case *flagCall:
		args := flag.Args()
		if len(args) < 3 {
			errExit(ErrInvalidArgs, "Usage: --call <server> <tool> '<json>'")
		}
		callTool(args[0], args[1], args[2])

	case *flagQuery:
		args := flag.Args()
		if len(args) < 3 {
			errExit(ErrInvalidArgs, "Usage: --query <server> <tool> '<json>'")
		}
		daemonQuery(args[0], args[1], args[2])

	case *flagStatus:
		showStatus()

	case *flagLogs != "":
		tailLogs(*flagLogs)

	default:
		flag.Usage()
	}
}

// listServers lists all configured servers
func listServers() {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	servers := make([]ServerInfo, 0, len(config.Servers))
	for name, cfg := range config.Servers {
		servers = append(servers, ServerInfo{
			Name:    name,
			URL:     cfg.URL,
			HasAuth: len(cfg.Headers) > 0,
			IsLocal: cfg.Local != nil,
		})
	}

	ok(map[string]any{"servers": servers})
}

// addServer adds a server to the configuration
func addServer(name, url string, headers headerFlags) {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	if _, exists := config.Servers[name]; exists {
		errExit(ErrExists, fmt.Sprintf("Server '%s' already exists. Remove it first with --remove.", name))
	}

	serverConfig := ServerConfig{URL: url}
	if len(headers) > 0 {
		serverConfig.Headers = make(map[string]string)
		for _, h := range headers {
			if !strings.Contains(h, ":") {
				errExit(ErrInvalidArgs, fmt.Sprintf("Invalid header format: '%s'. Use 'Name: Value'", h))
			}
			parts := strings.SplitN(h, ":", 2)
			serverConfig.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	config.Servers[name] = serverConfig
	if err := SaveConfig(config); err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to save config: %v", err))
	}

	ok(map[string]any{
		"message": fmt.Sprintf("Server '%s' added", name),
		"server": ServerInfo{
			Name:    name,
			URL:     url,
			HasAuth: len(serverConfig.Headers) > 0,
		},
	})
}

// removeServer removes a server from the configuration
func removeServer(name string) {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	if _, exists := config.Servers[name]; !exists {
		errExit(ErrNotFound, fmt.Sprintf("Server '%s' not found.", name))
	}

	delete(config.Servers, name)
	if err := SaveConfig(config); err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to save config: %v", err))
	}

	ok(map[string]any{
		"message": fmt.Sprintf("Server '%s' removed", name),
	})
}

// Placeholder implementations - will be filled in subsequent phases

func listTools(serverName string) {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	serverConfig, exists := config.Servers[serverName]
	if !exists {
		errExit(ErrNotFound, fmt.Sprintf("Server '%s' not configured. Run --servers to list.", serverName))
	}

	client := NewMCPClient(serverName, serverConfig)

	// Get OAuth token if available
	token, _ := GetTokenForServer(serverName, serverConfig)
	if token != "" {
		client.SetOAuthToken(token)
	}

	tools, err := client.ListTools()
	if err != nil {
		errExit(ErrMCPError, err.Error())
	}

	ok(map[string]any{
		"server": serverName,
		"tools":  tools,
	})
}

func callTool(serverName, toolName, argsJSON string) {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	serverConfig, exists := config.Servers[serverName]
	if !exists {
		errExit(ErrNotFound, fmt.Sprintf("Server '%s' not configured. Run --servers to list.", serverName))
	}

	var arguments map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
		errExit(ErrInvalidJSON, fmt.Sprintf("Invalid JSON arguments: %v", err))
	}

	client := NewMCPClient(serverName, serverConfig)

	// Get OAuth token if available
	token, _ := GetTokenForServer(serverName, serverConfig)
	if token != "" {
		client.SetOAuthToken(token)
	}

	result, err := client.CallTool(toolName, arguments)
	if err != nil {
		errExit(ErrMCPError, err.Error())
	}

	ok(map[string]any{
		"server": serverName,
		"tool":   toolName,
		"result": result,
	})
}

func doAuth(serverName string) {
	config, err := LoadConfig()
	if err != nil {
		errExit(ErrMCPError, fmt.Sprintf("Failed to load config: %v", err))
	}

	serverConfig, exists := config.Servers[serverName]
	if !exists {
		errExit(ErrNotFound, fmt.Sprintf("Server '%s' not configured", serverName))
	}

	if err := DoOAuthFlow(serverName, serverConfig); err != nil {
		errExit(ErrAuthExpired, err.Error())
	}
}

func startDaemon() {
	if err := StartDaemonBackground(); err != nil {
		errExit(ErrDaemonError, err.Error())
	}
}

func runDaemonForeground() {
	daemon, err := NewMCPDaemon()
	if err != nil {
		errExit(ErrMCPError, err.Error())
	}
	if err := daemon.Run(); err != nil {
		errExit(ErrMCPError, err.Error())
	}
}

func stopDaemon() {
	if err := StopDaemon(); err != nil {
		errExit(ErrDaemonError, err.Error())
	}
}

func daemonStatus() {
	GetDaemonStatus()
}

func daemonTools(serverName string) {
	resp, err := DaemonSend(DaemonCommand{
		Action: "tools",
		Server: serverName,
	})
	if err != nil {
		errExit(ErrDaemonError, err.Error())
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	if !resp.OK {
		os.Exit(1)
	}
}

func daemonQuery(serverName, toolName, argsJSON string) {
	var arguments map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
		errExit(ErrInvalidJSON, fmt.Sprintf("Invalid JSON arguments: %v", err))
	}

	resp, err := DaemonSend(DaemonCommand{
		Action:    "call",
		Server:    serverName,
		Tool:      toolName,
		Arguments: arguments,
	})
	if err != nil {
		errExit(ErrDaemonError, err.Error())
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	if !resp.OK {
		os.Exit(1)
	}
}

func showStatus() {
	resp, err := DaemonSend(DaemonCommand{
		Action: "status",
	})
	if err != nil {
		errExit(ErrDaemonError, err.Error())
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	if !resp.OK {
		os.Exit(1)
	}
}

func tailLogs(serverName string) {
	logPath := GetLogPath(serverName)

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		errExit(ErrNotFound, fmt.Sprintf("No logs found for server '%s'. Log path: %s", serverName, logPath))
	}

	// Use tail -f to follow the log file
	cmd := exec.Command("tail", "-f", "-n", "100", logPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Tailing logs for '%s' (Ctrl+C to stop)\n", serverName)
	fmt.Printf("Log file: %s\n\n", logPath)

	if err := cmd.Run(); err != nil {
		// Ignore interrupt errors from Ctrl+C
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		errExit(ErrMCPError, err.Error())
	}
}
