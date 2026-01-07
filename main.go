package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

var (
	// Basic commands
	flagServers       = flag.Bool("servers", false, "List configured servers")
	flagTools         = flag.String("tools", "", "List tools on a server")
	flagCall          = flag.Bool("call", false, "Call a tool: --call <server> <tool> '<json>'")
	flagInit          = flag.Bool("init", false, "Initialize config file")
	flagClearSessions = flag.Bool("clear-sessions", false, "Clear cached sessions")
	flagClearTokens   = flag.Bool("clear-tokens", false, "Clear stored OAuth tokens")
	flagAuth          = flag.String("auth", "", "OAuth login for a server")

	// Daemon mode
	flagDaemon           = flag.Bool("daemon", false, "Start daemon in background")
	flagDaemonForeground = flag.Bool("daemon-foreground", false, "Run daemon in foreground (internal)")
	flagDaemonStop       = flag.Bool("daemon-stop", false, "Stop the daemon")
	flagDaemonStatus     = flag.Bool("daemon-status", false, "Check daemon status")
	flagDaemonTools      = flag.String("daemon-tools", "", "List tools via daemon")
	flagQuery            = flag.Bool("query", false, "Fast query via daemon: --query <server> <tool> '<json>'")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `mcpx - MCP protocol bridge for AI agents

Usage:
  mcpx --servers                          # List configured servers
  mcpx --tools <server>                   # List tools on a server
  mcpx --call <server> <tool> '<json>'    # Call a tool
  mcpx --auth <server>                    # OAuth login for a server
  mcpx --init                             # Create config file

Daemon mode (fast queries):
  mcpx --daemon                           # Start daemon in background
  mcpx --query <server> <tool> '<json>'   # Fast query via daemon
  mcpx --daemon-tools <server>            # List tools via daemon
  mcpx --daemon-stop                      # Stop daemon

Config: ~/.mcpx/servers.json

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
		})
	}

	ok(map[string]any{"servers": servers})
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
	errExit(ErrMCPError, "Not implemented yet - Phase 6")
}

func daemonQuery(serverName, toolName, argsJSON string) {
	errExit(ErrMCPError, "Not implemented yet - Phase 6")
}
