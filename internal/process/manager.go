// Package process manages child processes such as Code-Server and user applications, and streams execution logs.
package process

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"workspace-manager/internal/config"
	"workspace-manager/web"
)

// LogEntry represents a structured log line.
type LogEntry struct {
	Time     string `json:"time"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	IsStderr bool   `json:"is_stderr"`
}

// ProcessManager manages child processes and real-time logs.
type ProcessManager struct {
	cfgManager   *config.ConfigManager
	workspaceDir string

	processMu     sync.Mutex
	appCmd        *exec.Cmd
	appCancel     context.CancelFunc
	currentDevCmd string

	codeServerCmd    *exec.Cmd
	codeServerCancel context.CancelFunc

	logMu       sync.RWMutex
	logHistory  []LogEntry
	maxLogs     int
	subscribers map[chan LogEntry]struct{}
}

// NewProcessManager creates a new ProcessManager instance.
func NewProcessManager(cm *config.ConfigManager) *ProcessManager {
	return &ProcessManager{
		cfgManager:   cm,
		workspaceDir: cm.GetSystem().WorkspaceDir,
		maxLogs:      2000,
		logHistory:   make([]LogEntry, 0, 2000),
		subscribers:  make(map[chan LogEntry]struct{}),
	}
}

// BroadcastLog adds a log to history and delivers it to all connected SSE clients.
func (pm *ProcessManager) BroadcastLog(source, msg string, isStderr bool) {
	entry := LogEntry{
		Time:     time.Now().Format(time.RFC3339),
		Source:   source,
		Message:  msg,
		IsStderr: isStderr,
	}

	pm.logMu.Lock()
	if len(pm.logHistory) >= pm.maxLogs {
		pm.logHistory = pm.logHistory[1:]
	}
	pm.logHistory = append(pm.logHistory, entry)

	subs := make([]chan LogEntry, 0, len(pm.subscribers))
	for ch := range pm.subscribers {
		subs = append(subs, ch)
	}
	pm.logMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// StartCodeServer launches code-server in the background with automatic restart monitoring.
func (pm *ProcessManager) StartCodeServer(ctx context.Context) {
	sysCfg := pm.cfgManager.GetSystem()
	if sysCfg.DisableCodeSrv {
		pm.BroadcastLog("gateway", "Code-Server startup skipped (DISABLE_CODE_SERVER set)", false)
		return
	}

	csBinary := "code-server"
	if _, err := exec.LookPath(csBinary); err != nil {
		pm.BroadcastLog("gateway", fmt.Sprintf("Code-Server binary '%s' not found on PATH. Editor proxy will wait for it.", csBinary), true)
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pm.processMu.Lock()
			childCtx, cancel := context.WithCancel(ctx)
			pm.codeServerCancel = cancel

			args := []string{
				"--auth", "none",
				"--bind-addr", fmt.Sprintf("127.0.0.1:%d", sysCfg.CodeServerPort),
				"--disable-telemetry",
				"--disable-update-check",
				pm.workspaceDir,
			}

			cmd := exec.CommandContext(childCtx, csBinary, args...)
			cmd.Dir = pm.workspaceDir
			pm.codeServerCmd = cmd
			pm.processMu.Unlock()

			pm.BroadcastLog("editor", fmt.Sprintf("Starting code-server on port %d...", sysCfg.CodeServerPort), false)

			stdout, _ := cmd.StdoutPipe()
			stderr, _ := cmd.StderrPipe()

			if err := cmd.Start(); err != nil {
				pm.BroadcastLog("editor", fmt.Sprintf("Failed to start code-server: %v", err), true)
				time.Sleep(5 * time.Second)
				continue
			}

			go pm.streamPipe("editor", stdout, false)
			go pm.streamPipe("editor", stderr, true)

			err := cmd.Wait()
			pm.BroadcastLog("editor", fmt.Sprintf("code-server exited: %v", err), true)

			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}

// SyncUserApp synchronizes the running user application with current workspace.yml commands.dev.
func (pm *ProcessManager) SyncUserApp(ctx context.Context) {
	pm.processMu.Lock()
	defer pm.processMu.Unlock()

	wsCfg := pm.cfgManager.GetWorkspace()
	newCmd := strings.TrimSpace(wsCfg.Commands.Dev)

	if newCmd == pm.currentDevCmd && pm.appCmd != nil && pm.appCmd.Process != nil {
		return
	}

	if pm.appCancel != nil {
		pm.BroadcastLog("gateway", fmt.Sprintf("Stopping existing user app process ('%s')...", pm.currentDevCmd), false)
		pm.appCancel()
		pm.appCancel = nil
		pm.appCmd = nil
	}

	pm.currentDevCmd = newCmd
	if newCmd == "" {
		pm.BroadcastLog("gateway", "No dev command configured in workspace.yml", false)
		return
	}

	childCtx, cancel := context.WithCancel(ctx)
	pm.appCancel = cancel

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(childCtx, "cmd", "/c", newCmd)
	} else {
		cmd = exec.CommandContext(childCtx, "sh", "-c", newCmd)
	}

	cmd.Dir = pm.workspaceDir
	cmd.Env = os.Environ()
	pm.appCmd = cmd

	pm.BroadcastLog("app", fmt.Sprintf("Starting dev command: '%s'", newCmd), false)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		pm.BroadcastLog("app", fmt.Sprintf("Failed to get stdout: %v", err), true)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		pm.BroadcastLog("app", fmt.Sprintf("Failed to get stderr: %v", err), true)
		return
	}

	if err := cmd.Start(); err != nil {
		pm.BroadcastLog("app", fmt.Sprintf("Failed to start command: %v", err), true)
		return
	}

	go pm.streamPipe("app", stdout, false)
	go pm.streamPipe("app", stderr, true)

	go func(targetCmd *exec.Cmd, cmdStr string) {
		err := targetCmd.Wait()
		pm.BroadcastLog("app", fmt.Sprintf("Process ('%s') finished: %v", cmdStr, err), err != nil)
	}(cmd, newCmd)
}

// StopAll terminates all running child processes.
func (pm *ProcessManager) StopAll() {
	pm.processMu.Lock()
	defer pm.processMu.Unlock()

	if pm.appCancel != nil {
		pm.appCancel()
		pm.appCancel = nil
	}
	if pm.codeServerCancel != nil {
		pm.codeServerCancel()
		pm.codeServerCancel = nil
	}
}

func (pm *ProcessManager) streamPipe(source string, r io.Reader, isStderr bool) {
	if r == nil {
		return
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := scanner.Text()
		pm.BroadcastLog(source, text, isStderr)
	}
}

// LogsDashboardHandler serves the log dashboard HTML page.
func (pm *ProcessManager) LogsDashboardHandler(w http.ResponseWriter, r *http.Request) {
	content, err := web.ReadStaticFile("logs.html")
	if err != nil {
		http.Error(w, "Failed to load logs dashboard", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

// LogsStreamHandler streams log entries as Server-Sent Events.
func (pm *ProcessManager) LogsStreamHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan LogEntry, 100)

	pm.logMu.Lock()
	pm.subscribers[ch] = struct{}{}

	history := make([]LogEntry, len(pm.logHistory))
	copy(history, pm.logHistory)
	pm.logMu.Unlock()

	defer func() {
		pm.logMu.Lock()
		delete(pm.subscribers, ch)
		pm.logMu.Unlock()
	}()

	for _, entry := range history {
		data, _ := json.Marshal(entry)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(entry)
			_, err := fmt.Fprintf(w, "data: %s\n\n", data)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
