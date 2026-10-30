// Command agent is the HappyWork desktop entry point. It only wires
// dependencies together; all behaviour lives in internal packages.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/happywork/happywork/internal/agent"
	"github.com/happywork/happywork/internal/desktop"
	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/mcp"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/singleinst"
	"github.com/happywork/happywork/internal/storage"
)

func main() {
	scriptFlag := flag.String("mock-script", "", "force the mock script (greet|read_file|long|boom); default: auto-select per message")
	providerFlag := flag.String("provider", "auto", "model provider: auto|mock|deepseek (auto: deepseek when DEEPSEEK_API_KEY is set, else mock)")
	flag.Parse()

	logger := newLogger()

	// Single instance: a second launch just focuses the first window (A4).
	lock, err := singleinst.Acquire()
	if errors.Is(err, singleinst.ErrAlreadyRunning) {
		logger.Printf("another instance is running; focusing it")
		_ = singleinst.RequestFocus()
		return
	}
	if err != nil {
		logger.Fatalf("single instance lock: %v", err)
	}
	defer func() { _ = lock.Close() }()

	store, err := openStore()
	if err != nil {
		logger.Fatalf("storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Crash recovery: non-terminal sessions become interrupted; tool calls that
	// were mid-flight have genuinely unknown outcomes (design.md §11).
	if n, err := store.MarkInterruptedNonTerminal(context.Background()); err != nil {
		logger.Printf("recover interrupted sessions: %v", err)
	} else if n > 0 {
		logger.Printf("marked %d interrupted session(s)", n)
	}
	if n, err := store.MarkRunningToolCallsUnknown(context.Background()); err != nil {
		logger.Printf("mark unknown tool calls: %v", err)
	} else if n > 0 {
		logger.Printf("marked %d tool call(s) unknown", n)
	}

	dataDir := appDataDir()
	provider, providerName, modelName, isMock, err := buildProvider(*providerFlag, *scriptFlag, logger)
	if err != nil {
		logger.Fatalf("provider: %v", err)
	}

	mcpManager, mcpErr := startMCP(dataDir, logger)
	if mcpManager != nil {
		defer mcpManager.Close()
	}
	presetDir, hookDir := "", ""
	if dataDir != "" {
		presetDir = filepath.Join(dataDir, "presets.d")
		hookDir = filepath.Join(dataDir, "hooks.d")
	}
	presets, presetErrs := permission.LoadPresets(presetDir)
	presetErr := ""
	for _, err := range presetErrs {
		logger.Printf("presets: %v", err)
		if presetErr == "" {
			presetErr = err.Error()
		}
	}
	hookAudit := func(ev hook.Event, name, kind, reason string) {
		if err := store.AppendAudit(context.Background(), ev.SessionID, name, kind, reason); err != nil {
			logger.Printf("hook audit: %v", err)
		}
		logger.Printf("hook %s %s: %s", name, kind, reason)
	}
	hookRunner, hookInfo, hookErr := hook.LoadDir(hookDir, hook.Builtins(), hookAudit)
	hooksFingerprint := ""
	hookErrText := ""
	if hookErr != nil {
		logger.Printf("hooks config: %v", hookErr)
		hookErrText = "Hook 配置错误：" + hookErr.Error()
		hookRunner = nil
	} else {
		hooksFingerprint = hookInfo.Fingerprint
		if hooksFingerprint != "" {
			logger.Printf("hooks: %d enabled, config sha256=%s", len(hookInfo.Enabled), hooksFingerprint)
		}
	}
	opts := agent.Options{DataDir: dataDir, ModelName: modelName, Presets: presets, Hooks: hookRunner}
	mcpServers := 0
	if mcpManager != nil {
		opts.ExternalTools = mcpManager.Tools
		failed := 0
		for _, st := range mcpManager.Status() {
			if st.Disabled {
				failed++
				continue
			}
			mcpServers++
		}
		if failed > 0 && mcpErr == "" {
			mcpErr = fmt.Sprintf("%d 个 MCP 服务器启动失败，详见日志", failed)
		}
	}
	manager := agent.NewManager(store, provider, desktop.NewEmitter(), logger, opts)

	// Focus requests from second instances arrive on a poll goroutine.
	if err := lock.WatchFocus(func() {
		logger.Printf("focus requested by a second instance")
		desktop.FocusMainWindow()
	}); err != nil {
		logger.Printf("focus watcher: %v", err)
	}

	info := desktop.AppInfo{
		Provider:         providerName,
		Model:            modelName,
		Mock:             isMock,
		DataDestination:  dataDestination(isMock),
		MCPServers:       mcpServers,
		MCPError:         mcpErr,
		HooksFingerprint: hooksFingerprint,
		HooksError:       hookErrText,
		PresetsError:     presetErr,
	}
	if err := desktop.Run(desktop.Options{Manager: manager, Emitter: manager.Emitter(), Logger: logger, AppInfo: info}); err != nil {
		logger.Fatalf("desktop: %v", err)
	}
}

// dataDestination is shown in the UI whenever a session is selected.
func dataDestination(mock bool) string {
	if mock {
		return "本地 mock（不联网）"
	}
	return "api.deepseek.com（工作区内容会发送至该服务）"
}

// startMCP loads mcp.json and launches configured servers. Config errors fail
// closed (no MCP) and are surfaced to the UI; a slow server cannot block start
// beyond the manager's 15s budget.
func startMCP(dataDir string, lg *log.Logger) (*mcp.Manager, string) {
	if dataDir == "" {
		lg.Printf("mcp: skipped, application data directory is unavailable")
		return nil, ""
	}
	cfg, err := mcp.Load(filepath.Join(dataDir, "mcp.json"))
	if err != nil {
		lg.Printf("mcp config: %v", err)
		return nil, err.Error()
	}
	enabled := 0
	for _, server := range cfg.Servers {
		if server.IsEnabled() {
			enabled++
		}
	}
	if enabled == 0 {
		return nil, ""
	}
	mgr := mcp.NewManager(cfg, dataDir, lg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	mgr.Start(ctx)
	lg.Printf("mcp: %d server(s) configured", enabled)
	return mgr, ""
}

// newLogger writes to stderr (visible in console builds) and appends to the
// app log file — under the windowsgui subsystem stderr is detached, so the
// file is the only way a double-click startup failure leaves a trace.
func newLogger() *log.Logger {
	var w io.Writer = os.Stderr
	if base, err := os.UserConfigDir(); err == nil {
		dir := filepath.Join(base, "happywork")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if f, err := os.OpenFile(filepath.Join(dir, "happywork.log"),
				os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				w = io.MultiWriter(os.Stderr, f)
			}
		}
	}
	return log.New(w, "happywork ", log.LstdFlags|log.Lmsgprefix)
}

// openStore opens the SQLite database under the user config dir.
func openStore() (storage.Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, "happywork")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return storage.Open(filepath.Join(dir, "happywork.db"))
}

// appDataDir is the user-visible application data directory (logs, attachments,
// backups, MCP working dirs).
func appDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "happywork")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}
