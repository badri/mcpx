package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// DaemonCommand represents a command sent to the daemon
type DaemonCommand struct {
	Action    string         `json:"action"`
	Server    string         `json:"server,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CachedTools holds cached tool information
type CachedTools struct {
	Tools   []Tool
	Expires time.Time
}

// MCPDaemon is the daemon server
type MCPDaemon struct {
	config       *Config
	clients      map[string]*MCPClient
	toolsCache   map[string]*CachedTools
	localManager *LocalManager
	mu           sync.RWMutex
	running      bool
	listener     net.Listener
}

// NewMCPDaemon creates a new daemon instance
func NewMCPDaemon() (*MCPDaemon, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	return &MCPDaemon{
		config:       config,
		clients:      make(map[string]*MCPClient),
		toolsCache:   make(map[string]*CachedTools),
		localManager: NewLocalManager(),
		running:      true,
	}, nil
}

// getClient gets or creates a persistent MCP client for a server
func (d *MCPDaemon) getClient(serverName string) (*MCPClient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if client, ok := d.clients[serverName]; ok {
		return client, nil
	}

	serverConfig, ok := d.config.Servers[serverName]
	if !ok {
		return nil, fmt.Errorf("server '%s' not configured", serverName)
	}

	client := NewMCPClient(serverName, serverConfig)

	// Get OAuth token if available
	token, _ := GetTokenForServer(serverName, serverConfig)
	if token != "" {
		client.SetOAuthToken(token)
	}

	d.clients[serverName] = client
	return client, nil
}

// getTools gets tools for a server with caching
func (d *MCPDaemon) getTools(serverName string) ([]Tool, error) {
	d.mu.RLock()
	if cached, ok := d.toolsCache[serverName]; ok {
		if time.Now().Before(cached.Expires) {
			d.mu.RUnlock()
			return cached.Tools, nil
		}
	}
	d.mu.RUnlock()

	client, err := d.getClient(serverName)
	if err != nil {
		return nil, err
	}

	tools, err := client.ListTools()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.toolsCache[serverName] = &CachedTools{
		Tools:   tools,
		Expires: time.Now().Add(ToolsCacheTTL),
	}
	d.mu.Unlock()

	return tools, nil
}

// callTool calls a tool on a server
func (d *MCPDaemon) callTool(serverName, toolName string, arguments map[string]any) (map[string]any, error) {
	client, err := d.getClient(serverName)
	if err != nil {
		return nil, err
	}

	return client.CallTool(toolName, arguments)
}

// reloadConfig reloads the configuration
func (d *MCPDaemon) reloadConfig() error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	oldConfig := d.config
	d.config = config

	// Handle client updates based on config changes
	for name, client := range d.clients {
		newServerConfig, exists := d.config.Servers[name]
		if !exists {
			// Server was removed - close and delete client
			client.Close()
			delete(d.clients, name)
			delete(d.toolsCache, name)
			continue
		}

		// Check if server config changed significantly (URL or persistent mode)
		oldServerConfig := oldConfig.Servers[name]
		if oldServerConfig.URL != newServerConfig.URL ||
			oldServerConfig.SessionBased != newServerConfig.SessionBased {
			// Config changed - close old client, will be recreated on next request
			client.Close()
			delete(d.clients, name)
			delete(d.toolsCache, name)
		}
	}

	return nil
}

// closeAllClients closes all MCP clients (for shutdown)
func (d *MCPDaemon) closeAllClients() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for name, client := range d.clients {
		client.Close()
		delete(d.clients, name)
	}
	d.toolsCache = make(map[string]*CachedTools)
}

// startLocalServers starts all servers with local configuration
func (d *MCPDaemon) startLocalServers() {
	d.mu.RLock()
	servers := d.config.Servers
	d.mu.RUnlock()

	for name, cfg := range servers {
		if cfg.Local != nil {
			fmt.Fprintf(os.Stderr, "[%s] Starting local server '%s'...\n",
				time.Now().Format("15:04:05"), name)
			if err := d.localManager.StartServer(name, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Failed to start '%s': %v\n",
					time.Now().Format("15:04:05"), name, err)
			}
		}
	}
}

// stopLocalServers stops all locally-managed servers
func (d *MCPDaemon) stopLocalServers() {
	d.localManager.StopAll()
}

// getProcessStatus returns status of all local processes
func (d *MCPDaemon) getProcessStatus() []ProcessInfo {
	return d.localManager.GetStatus()
}

// handleCommand handles a daemon command
func (d *MCPDaemon) handleCommand(cmd DaemonCommand) Response {
	switch cmd.Action {
	case "ping":
		return okResponse("pong")

	case "reload":
		if err := d.reloadConfig(); err != nil {
			return errResponse(ErrMCPError, err.Error())
		}
		return okResponse("config reloaded")

	case "servers":
		d.mu.RLock()
		servers := make([]ServerInfo, 0, len(d.config.Servers))
		for name, cfg := range d.config.Servers {
			servers = append(servers, ServerInfo{
				Name: name,
				URL:  cfg.URL,
			})
		}
		d.mu.RUnlock()
		return okResponse(map[string]any{"servers": servers})

	case "tools":
		if cmd.Server == "" {
			return errResponse(ErrInvalidArgs, "server name required")
		}
		tools, err := d.getTools(cmd.Server)
		if err != nil {
			return errResponse(ErrMCPError, err.Error())
		}
		return okResponse(map[string]any{
			"server": cmd.Server,
			"tools":  tools,
		})

	case "call":
		if cmd.Server == "" || cmd.Tool == "" {
			return errResponse(ErrInvalidArgs, "server and tool names required")
		}
		result, err := d.callTool(cmd.Server, cmd.Tool, cmd.Arguments)
		if err != nil {
			return errResponse(ErrMCPError, err.Error())
		}
		return okResponse(map[string]any{
			"server": cmd.Server,
			"tool":   cmd.Tool,
			"result": result,
		})

	case "status":
		// Return status of daemon and local processes
		processes := d.getProcessStatus()
		d.mu.RLock()
		serverCount := len(d.config.Servers)
		localCount := 0
		for _, cfg := range d.config.Servers {
			if cfg.Local != nil {
				localCount++
			}
		}
		d.mu.RUnlock()
		return okResponse(map[string]any{
			"daemon":     "running",
			"servers":    serverCount,
			"local":      localCount,
			"processes":  processes,
		})

	case "shutdown":
		d.running = false
		d.stopLocalServers()
		return okResponse("shutting down")

	default:
		return errResponse(ErrUnknownAction, fmt.Sprintf("unknown action: %s", cmd.Action))
	}
}

// handleConnection handles a client connection
func (d *MCPDaemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	start := time.Now()
	reader := bufio.NewReader(conn)

	// Read command (single JSON object)
	var cmd DaemonCommand
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&cmd); err != nil {
		response := errResponse(ErrParseError, err.Error())
		json.NewEncoder(conn).Encode(response)
		fmt.Fprintf(os.Stderr, "[%s] ERROR parse: %v\n", time.Now().Format("15:04:05"), err)
		return
	}

	// Handle command
	response := d.handleCommand(cmd)

	// Log request
	elapsed := time.Since(start)
	status := "OK"
	if !response.OK {
		status = "ERR"
	}
	if cmd.Action == "call" {
		fmt.Fprintf(os.Stderr, "[%s] %s %s/%s %s (%v)\n",
			time.Now().Format("15:04:05"), status, cmd.Server, cmd.Tool, cmd.Action, elapsed)
	} else if cmd.Server != "" {
		fmt.Fprintf(os.Stderr, "[%s] %s %s %s (%v)\n",
			time.Now().Format("15:04:05"), status, cmd.Server, cmd.Action, elapsed)
	} else if cmd.Action != "ping" {
		fmt.Fprintf(os.Stderr, "[%s] %s %s (%v)\n",
			time.Now().Format("15:04:05"), status, cmd.Action, elapsed)
	}

	// Send response
	json.NewEncoder(conn).Encode(response)
}

// Run starts the daemon
func (d *MCPDaemon) Run() error {
	// Create config directory if needed
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}

	// Remove stale socket
	if _, err := os.Stat(SocketPath); err == nil {
		os.Remove(SocketPath)
	}

	// Write PID file
	if err := os.WriteFile(PIDFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		return err
	}

	// Create Unix socket
	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return err
	}
	d.listener = listener

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		d.running = false
		listener.Close()
	}()

	fmt.Printf("MCP daemon started (pid %d)\n", os.Getpid())
	fmt.Printf("Socket: %s\n", SocketPath)

	// Start local servers
	d.startLocalServers()

	// Accept connections
	for d.running {
		conn, err := listener.Accept()
		if err != nil {
			if d.running {
				fmt.Fprintf(os.Stderr, "Accept error: %v\n", err)
			}
			continue
		}

		// Handle connection in goroutine (concurrent)
		go d.handleConnection(conn)
	}

	// Cleanup
	d.stopLocalServers()
	d.closeAllClients()
	listener.Close()
	os.Remove(SocketPath)
	os.Remove(PIDFile)

	fmt.Println("MCP daemon stopped")
	return nil
}

// IsDaemonRunning checks if the daemon is running
func IsDaemonRunning() bool {
	if _, err := os.Stat(SocketPath); os.IsNotExist(err) {
		return false
	}

	// Try to ping
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		return false
	}
	defer conn.Close()

	cmd := DaemonCommand{Action: "ping"}
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return false
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return false
	}

	return resp.OK
}

// DaemonSend sends a command to the daemon
func DaemonSend(cmd DaemonCommand) (Response, error) {
	if _, err := os.Stat(SocketPath); os.IsNotExist(err) {
		return errResponse(ErrDaemonNotRunning, "Daemon not running. Start with --daemon"), nil
	}

	conn, err := net.DialTimeout("unix", SocketPath, 30*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()

	// Set deadline
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Send command
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return Response{}, err
	}

	// Read response
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}

	return resp, nil
}

// StartDaemonBackground starts the daemon in the background
func StartDaemonBackground() error {
	if IsDaemonRunning() {
		fmt.Println("Daemon already running")
		return nil
	}

	// Fork to background using syscall
	// For Go, we use a simpler approach - start a new process
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	// Start daemon process
	cmd := &syscall.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []uintptr{0, 0, 0}, // stdin, stdout, stderr
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	pid, err := syscall.ForkExec(executable, []string{executable, "--daemon-foreground"}, cmd)
	if err != nil {
		return err
	}

	// Wait briefly and check if daemon started
	time.Sleep(500 * time.Millisecond)
	if IsDaemonRunning() {
		fmt.Printf("Daemon started (pid %d)\n", pid)
	} else {
		return fmt.Errorf("failed to start daemon")
	}

	return nil
}

// StopDaemon stops the daemon
func StopDaemon() error {
	if !IsDaemonRunning() {
		fmt.Println("Daemon not running")
		return nil
	}

	resp, err := DaemonSend(DaemonCommand{Action: "shutdown"})
	if err != nil {
		return err
	}

	if resp.OK {
		fmt.Println("Daemon stopped")
	} else if resp.Error != nil {
		fmt.Printf("Error: %s\n", resp.Error.Message)
	}

	return nil
}

// GetDaemonStatus returns the daemon status
func GetDaemonStatus() {
	if IsDaemonRunning() {
		fmt.Println("Daemon is running")
		if data, err := os.ReadFile(PIDFile); err == nil {
			fmt.Printf("PID: %s\n", string(data))
		}
	} else {
		fmt.Println("Daemon is not running")
	}
}
