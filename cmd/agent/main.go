// Command agent is the HappyWork desktop entry point. It only wires
// dependencies together; all behaviour lives in internal packages.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/happywork/happywork/internal/agent"
	"github.com/happywork/happywork/internal/desktop"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/model/mock"
	"github.com/happywork/happywork/internal/singleinst"
	"github.com/happywork/happywork/internal/storage"
)

func main() {
	scriptFlag := flag.String("mock-script", "", "force the mock script (greet|read_file|long|boom); default: auto-select per message")
	flag.Parse()

	logger := log.New(os.Stderr, "happywork ", log.LstdFlags|log.Lmsgprefix)

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

	// Crash recovery placeholder: mark non-terminal sessions as interrupted.
	if n, err := store.MarkInterruptedNonTerminal(context.Background()); err != nil {
		logger.Printf("recover interrupted sessions: %v", err)
	} else if n > 0 {
		logger.Printf("marked %d interrupted session(s)", n)
	}

	provider, err := buildProvider(*scriptFlag)
	if err != nil {
		logger.Fatalf("mock provider: %v", err)
	}

	manager := agent.NewManager(store, provider, desktop.NewEmitter(), logger)

	// Focus requests from second instances arrive on a poll goroutine.
	if err := lock.WatchFocus(func() { desktop.FocusMainWindow() }); err != nil {
		logger.Printf("focus watcher: %v", err)
	}

	if err := desktop.Run(desktop.Options{Manager: manager, Logger: logger}); err != nil {
		logger.Fatalf("desktop: %v", err)
	}
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

// buildProvider loads the embedded mock scripts; an explicit flag forces one
// script for every turn.
func buildProvider(script string) (model.Provider, error) {
	scripts, err := mock.Load()
	if err != nil {
		return nil, err
	}
	if script != "" {
		return mock.New(scripts, func(string, int) string { return script }), nil
	}
	return mock.New(scripts, nil), nil
}
