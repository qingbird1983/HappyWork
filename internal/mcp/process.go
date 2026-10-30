package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (c *Client) launch() error {
	workdir := c.cfg.Cwd
	if workdir == "" {
		workdir = filepath.Join(c.dataDir, "mcp-work", sanitizeName(c.name))
	}
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		return fmt.Errorf("mcp: server %q workdir: %w", c.name, err)
	}
	env, err := buildEnv(c.cfg.Env)
	if err != nil {
		return fmt.Errorf("mcp: server %q env: %w", c.name, err)
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Dir = workdir
	cmd.Env = env
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: server %q stdin: %w", c.name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: server %q stdout: %w", c.name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcp: server %q stderr: %w", c.name, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: server %q start: %w", c.name, err)
	}
	lines := make(chan *inbound, 16)
	done := make(chan struct{})
	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.lines = lines
	c.done = done
	c.mu.Unlock()
	go c.readLoop(stdout, lines, done)
	go c.drainStderr(stderr)
	return nil
}

func (c *Client) readLoop(stdout io.Reader, lines chan<- *inbound, done <-chan struct{}) {
	defer close(lines)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), maxLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg inbound
		if err := json.Unmarshal(line, &msg); err != nil {
			c.log.Printf("mcp: server %q: invalid JSON line: %v", c.name, err)
			continue
		}
		if msg.Method != "" {
			// Server-initiated request: we support none of them.
			if len(msg.ID) > 0 && string(msg.ID) != "null" {
				_ = c.writeMessage(map[string]any{
					"jsonrpc": "2.0",
					"id":      json.RawMessage(msg.ID),
					"error":   map[string]any{"code": -32601, "message": "method not supported by HappyWork"},
				})
			}
			continue
		}
		select {
		case lines <- &msg:
		case <-done:
			return
		}
	}
	if err := sc.Err(); err != nil {
		c.log.Printf("mcp: server %q: stdout read: %v", c.name, err)
	}
}

// drainStderr logs server stderr line by line with a fixed per-line budget, so
// a chatty or newline-free server cannot grow app memory without bound.
func (c *Client) drainStderr(stderr io.Reader) {
	reader := bufio.NewReaderSize(stderr, 8<<10)
	const maxLine = 8 << 10
	for {
		line, isPrefix, err := reader.ReadLine()
		truncated := false
		for isPrefix && err == nil {
			_, isPrefix, err = reader.ReadLine()
			truncated = true
		}
		if len(line) > 0 || truncated {
			text := strings.TrimRight(string(line), "\r")
			if len(text) > maxLine {
				text = text[:maxLine]
			}
			if truncated {
				text += "…"
			}
			c.log.Printf("mcp[%s] %s", c.name, text)
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) writeMessage(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("mcp: server %q stdin is closed", c.name)
	}
	_, err = stdin.Write(data)
	return err
}

func (c *Client) stop() {
	c.mu.Lock()
	cmd := c.cmd
	stdin := c.stdin
	done := c.done
	c.cmd = nil
	c.stdin = nil
	c.done = nil
	c.lines = nil
	c.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil || cmd.Process == nil {
		return
	}
	wait := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(wait)
	}()
	select {
	case <-wait:
	case <-time.After(closeGrace):
		// Only the forced path needs a tree kill: a hung server may have
		// spawned helpers that would otherwise outlive it.
		if runtime.GOOS == "windows" {
			_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		}
		_ = cmd.Process.Kill()
		<-wait
	}
}

// Close terminates the server.
func (c *Client) Close() {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	c.stop()
}

func (c *Client) disable() {
	c.mu.Lock()
	c.disabled = true
	c.mu.Unlock()
}

// ensureRunning restarts a crashed server within the 3/10min budget.
// buildEnv passes a minimal baseline plus declared $ENV refs; API keys are not
// inherited (mcp.md §3).
func buildEnv(refs map[string]string) ([]string, error) {
	baseline := []string{"PATH", "TEMP", "TMP", "TMPDIR", "HOME", "USER", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SystemRoot"}
	env := make([]string, 0, len(baseline)+len(refs))
	seen := map[string]bool{}
	for _, key := range baseline {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
			seen[key] = true
		}
	}
	for key, ref := range refs {
		m := envRefPattern.FindStringSubmatch(ref)
		if m == nil {
			return nil, fmt.Errorf("env %q must be $ENV:NAME", key)
		}
		value, ok := os.LookupEnv(m[1])
		if !ok {
			return nil, fmt.Errorf("env %q references unset variable %q", key, m[1])
		}
		if seen[key] {
			return nil, fmt.Errorf("env %q conflicts with the baseline", key)
		}
		env = append(env, key+"="+value)
	}
	return env, nil
}
