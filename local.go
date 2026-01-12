package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// LocalProcess represents a locally-managed MCP server process
type LocalProcess struct {
	Name       string
	Config     LocalConfig
	ServerURL  string // The URL to connect to after starting
	Cmd        *exec.Cmd
	LogFile    *os.File
	Started    time.Time
	Restarts   int
	mu         sync.Mutex
	stopping   bool
	done       chan struct{}
}

// ProcessInfo holds status information for a local process
type ProcessInfo struct {
	Name     string `json:"name"`
	PID      int    `json:"pid,omitempty"`
	Running  bool   `json:"running"`
	URL      string `json:"url"`
	Restarts int    `json:"restarts"`
	Uptime   string `json:"uptime,omitempty"`
	LogFile  string `json:"log_file"`
}

// LocalManager manages all locally-spawned MCP server processes
type LocalManager struct {
	processes map[string]*LocalProcess
	mu        sync.RWMutex
}

// NewLocalManager creates a new local process manager
func NewLocalManager() *LocalManager {
	return &LocalManager{
		processes: make(map[string]*LocalProcess),
	}
}

// StartServer starts a local MCP server process
func (m *LocalManager) StartServer(name string, serverConfig ServerConfig) error {
	if serverConfig.Local == nil {
		return fmt.Errorf("server '%s' has no local config", name)
	}

	m.mu.Lock()
	if _, exists := m.processes[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("server '%s' already running", name)
	}
	m.mu.Unlock()

	proc := &LocalProcess{
		Name:      name,
		Config:    *serverConfig.Local,
		ServerURL: serverConfig.URL,
		done:      make(chan struct{}),
	}

	if err := proc.Start(); err != nil {
		return err
	}

	m.mu.Lock()
	m.processes[name] = proc
	m.mu.Unlock()

	// Start monitor goroutine for automatic restart
	go m.monitorProcess(name, serverConfig)

	return nil
}

// StopServer stops a local MCP server process
func (m *LocalManager) StopServer(name string) error {
	m.mu.Lock()
	proc, exists := m.processes[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("server '%s' not running", name)
	}
	delete(m.processes, name)
	m.mu.Unlock()

	return proc.Stop()
}

// StopAll stops all local server processes
func (m *LocalManager) StopAll() {
	m.mu.Lock()
	procs := make([]*LocalProcess, 0, len(m.processes))
	for _, proc := range m.processes {
		procs = append(procs, proc)
	}
	m.processes = make(map[string]*LocalProcess)
	m.mu.Unlock()

	for _, proc := range procs {
		proc.Stop()
	}
}

// GetStatus returns status information for all processes
func (m *LocalManager) GetStatus() []ProcessInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]ProcessInfo, 0, len(m.processes))
	for _, proc := range m.processes {
		infos = append(infos, proc.GetInfo())
	}
	return infos
}

// GetProcess returns a specific process
func (m *LocalManager) GetProcess(name string) (*LocalProcess, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	proc, exists := m.processes[name]
	return proc, exists
}

// IsRunning checks if a server is running
func (m *LocalManager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	proc, exists := m.processes[name]
	if !exists {
		return false
	}
	return proc.IsRunning()
}

// monitorProcess monitors a process and restarts it if it crashes
func (m *LocalManager) monitorProcess(name string, serverConfig ServerConfig) {
	for {
		m.mu.RLock()
		proc, exists := m.processes[name]
		m.mu.RUnlock()

		if !exists {
			return // Process was removed
		}

		// Wait for process to exit
		<-proc.done

		proc.mu.Lock()
		if proc.stopping {
			proc.mu.Unlock()
			return // Intentional stop
		}
		proc.mu.Unlock()

		// Process crashed, attempt restart
		fmt.Fprintf(os.Stderr, "[%s] Server '%s' crashed, restarting...\n",
			time.Now().Format("15:04:05"), name)

		// Brief delay before restart
		time.Sleep(1 * time.Second)

		// Check if we're still tracking this server
		m.mu.Lock()
		_, stillExists := m.processes[name]
		if !stillExists {
			m.mu.Unlock()
			return
		}

		// Create new process
		newProc := &LocalProcess{
			Name:      name,
			Config:    *serverConfig.Local,
			ServerURL: serverConfig.URL,
			Restarts:  proc.Restarts + 1,
			done:      make(chan struct{}),
		}

		if err := newProc.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Failed to restart '%s': %v\n",
				time.Now().Format("15:04:05"), name, err)
			delete(m.processes, name)
			m.mu.Unlock()
			return
		}

		m.processes[name] = newProc
		m.mu.Unlock()
	}
}

// Start starts the process
func (p *LocalProcess) Start() error {
	// Ensure logs directory exists
	if err := os.MkdirAll(LogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Open log file
	logPath := filepath.Join(LogsDir, p.Name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	p.LogFile = logFile

	// Write startup marker
	fmt.Fprintf(logFile, "\n=== Starting %s at %s ===\n", p.Name, time.Now().Format(time.RFC3339))

	// Find the command
	cmdPath, err := exec.LookPath(p.Config.Command)
	if err != nil {
		logFile.Close()
		return fmt.Errorf("command not found: %s", p.Config.Command)
	}

	// Create command
	p.Cmd = exec.Command(cmdPath, p.Config.Args...)

	// Set environment
	p.Cmd.Env = append(os.Environ(), p.Config.Env...)

	// Capture stdout and stderr
	stdout, err := p.Cmd.StdoutPipe()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := p.Cmd.StderrPipe()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the process
	if err := p.Cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}

	p.Started = time.Now()

	// Start log capture goroutines
	go p.captureOutput("stdout", stdout)
	go p.captureOutput("stderr", stderr)

	// Start wait goroutine
	go func() {
		p.Cmd.Wait()
		p.LogFile.Close()
		close(p.done)
	}()

	// Wait for server to be ready
	if err := p.waitForReady(); err != nil {
		p.Stop()
		return err
	}

	fmt.Fprintf(os.Stderr, "[%s] Started '%s' (pid %d)\n",
		time.Now().Format("15:04:05"), p.Name, p.Cmd.Process.Pid)

	return nil
}

// captureOutput captures output from a pipe and writes to log file
func (p *LocalProcess) captureOutput(name string, pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		p.mu.Lock()
		if p.LogFile != nil {
			fmt.Fprintf(p.LogFile, "[%s] %s\n", time.Now().Format("15:04:05"), line)
		}
		p.mu.Unlock()
	}
}

// waitForReady waits for the server to accept connections
func (p *LocalProcess) waitForReady() error {
	// Extract host:port from URL
	// Simple parsing - assume http://host:port/path format
	url := p.ServerURL
	if len(url) > 7 && url[:7] == "http://" {
		url = url[7:]
	}
	// Find end of host:port (before path)
	for i, c := range url {
		if c == '/' {
			url = url[:i]
			break
		}
	}

	// Try connecting for up to 30 seconds
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", url, 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}

		// Check if process died
		select {
		case <-p.done:
			return fmt.Errorf("process exited before becoming ready")
		default:
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for server to be ready")
}

// Stop stops the process
func (p *LocalProcess) Stop() error {
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()

	if p.Cmd == nil || p.Cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown first
	p.Cmd.Process.Signal(os.Interrupt)

	// Wait up to 5 seconds for graceful shutdown
	select {
	case <-p.done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// Force kill
	p.Cmd.Process.Kill()
	<-p.done

	fmt.Fprintf(os.Stderr, "[%s] Stopped '%s'\n", time.Now().Format("15:04:05"), p.Name)
	return nil
}

// IsRunning checks if the process is still running
func (p *LocalProcess) IsRunning() bool {
	if p.Cmd == nil || p.Cmd.Process == nil {
		return false
	}

	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// GetInfo returns process information
func (p *LocalProcess) GetInfo() ProcessInfo {
	info := ProcessInfo{
		Name:     p.Name,
		URL:      p.ServerURL,
		Restarts: p.Restarts,
		LogFile:  filepath.Join(LogsDir, p.Name+".log"),
	}

	if p.IsRunning() {
		info.Running = true
		info.PID = p.Cmd.Process.Pid
		info.Uptime = time.Since(p.Started).Round(time.Second).String()
	}

	return info
}

// GetLogPath returns the log file path for a server
func GetLogPath(serverName string) string {
	return filepath.Join(LogsDir, serverName+".log")
}
