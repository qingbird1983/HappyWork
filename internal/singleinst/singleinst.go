// Package singleinst implements the desktop single-instance lock and the
// "focus the existing window" channel (architecture/04; acceptance A4).
//
// The lock is a gofrs/flock file under the user config dir. The focus channel
// is a request file plus a low-frequency poll: the second instance touches
// focus.req and exits; the first instance notices and focuses its window.
// Named pipes were rejected because the Go standard library does not support
// listening on them (net's "pipe" network is the in-memory net.Pipe only),
// and an extra dependency or a TCP loopback port was not worth it for a
// sub-second latency budget (see docs/design/phase-1/design.md §14).
package singleinst

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// ErrAlreadyRunning is returned by Acquire when another instance holds the lock.
var ErrAlreadyRunning = errors.New("singleinst: another instance is already running")

// appDirName is the directory under os.UserConfigDir().
const appDirName = "happywork"

// pollInterval trades focus latency against idle syscalls.
const pollInterval = 400 * time.Millisecond

// Lock is the held single-instance lock plus the focus watcher.
type Lock struct {
	fl      *flock.Flock
	focusFn func()
	stop    chan struct{}
}

// appDir returns (and creates) the per-user app config directory.
func appDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("singleinst: user config dir: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("singleinst: mkdir %s: %w", dir, err)
	}
	return dir, nil
}

func lockPath() (string, error) {
	dir, err := appDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "single.lock"), nil
}

func focusReqPath() (string, error) {
	dir, err := appDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "focus.req"), nil
}

// Acquire takes the single-instance lock. It returns ErrAlreadyRunning when
// another process holds it.
func Acquire() (*Lock, error) {
	path, err := lockPath()
	if err != nil {
		return nil, err
	}
	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("singleinst: try lock: %w", err)
	}
	if !ok {
		return nil, ErrAlreadyRunning
	}
	return &Lock{fl: fl, stop: make(chan struct{})}, nil
}

// WatchFocus starts polling for focus requests from second instances. Calling
// it twice is a programming error and panics.
func (l *Lock) WatchFocus(onFocus func()) error {
	if l.focusFn != nil {
		panic("singleinst: WatchFocus called twice")
	}
	if onFocus == nil {
		panic("singleinst: WatchFocus needs a callback")
	}
	reqPath, err := focusReqPath()
	if err != nil {
		return err
	}
	l.focusFn = onFocus

	// A request file left over from a previous run must not fire now.
	_ = os.Remove(reqPath)

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-l.stop:
				return
			case <-ticker.C:
				if _, err := os.Stat(reqPath); err == nil {
					_ = os.Remove(reqPath)
					onFocus()
				}
			}
		}
	}()
	return nil
}

// Close releases the lock and stops the focus watcher.
func (l *Lock) Close() error {
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
	return l.fl.Unlock()
}

// RequestFocus asks a running first instance to focus its window. It is
// called by the second instance right before exiting.
func RequestFocus() error {
	reqPath, err := focusReqPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(reqPath, []byte(fmt.Sprint(time.Now().UnixNano())), 0o600); err != nil {
		return fmt.Errorf("singleinst: write focus request: %w", err)
	}
	return nil
}
