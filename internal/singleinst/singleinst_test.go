package singleinst

import (
	"errors"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// isolateConfigDir redirects os.UserConfigDir into the test temp dir where
// the platform honours it (Windows: AppData; Linux: XDG_CONFIG_HOME). On
// darwin UserConfigDir ignores env vars, so tests skip there.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("os.UserConfigDir is not env-driven on darwin; skip to avoid touching the real profile")
	}
	base := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", base)
	default:
		t.Setenv("XDG_CONFIG_HOME", base)
	}
}

func TestAcquireExclusive(t *testing.T) {
	isolateConfigDir(t)
	l1, err := Acquire()
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = l1.Close() }()

	if _, err := Acquire(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Acquire err = %v, want ErrAlreadyRunning", err)
	}

	// After release the lock is free again.
	if err := l1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	l3, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire after close: %v", err)
	}
	_ = l3.Close()
}

func TestWatchFocus(t *testing.T) {
	isolateConfigDir(t)
	l, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = l.Close() }()

	var fired atomic.Bool
	done := make(chan struct{})

	// A stale request from a previous run must not fire the callback: the
	// watcher removes any leftover file when it starts.
	reqPath, err := focusReqPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := l.WatchFocus(func() {
		fired.Store(true)
		close(done)
	}); err != nil {
		t.Fatalf("WatchFocus: %v", err)
	}
	time.Sleep(2 * pollInterval)
	if fired.Load() {
		t.Fatal("stale focus request fired")
	}

	if err := RequestFocus(); err != nil {
		t.Fatalf("RequestFocus: %v", err)
	}
	select {
	case <-done:
		if !fired.Load() {
			t.Fatal("callback ran but flag not set")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("focus callback not called in time")
	}
}
