// Package mcp implements the local stdio MCP client (design/phase-2/mcp.md):
// strict config, dual-era probe (ADR-0011), tool discovery with a schema
// whitelist, per-call approvals at the agent layer, and bounded restarts.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ToolPolicy is per-tool config.
type ToolPolicy struct {
	Destructive bool   `json:"destructive,omitempty"`
	Action      string `json:"action,omitempty"` // read | write | delete | unknown
}

// ServerConfig is one MCP server declaration.
type ServerConfig struct {
	Command        string                `json:"command"`
	Args           []string              `json:"args,omitempty"`
	Env            map[string]string     `json:"env,omitempty"`
	Cwd            string                `json:"cwd,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
	TimeoutSeconds int                   `json:"timeoutSeconds,omitempty"`
	Tools          map[string]ToolPolicy `json:"tools,omitempty"`
}

// Config is the whole mcp.json.
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

var (
	serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	envRefPattern     = regexp.MustCompile(`^\$ENV:([A-Za-z_][A-Za-z0-9_]*)$`)
)

// Load reads and strictly validates mcp.json. A missing file means "no MCP".
// Unknown fields, relative commands and malformed env refs reject the whole
// file (fail closed, design §3).
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("mcp: read config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("mcp: parse config: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("mcp: trailing data after config object")
	}
	return nil
}

func (c *Config) validate() error {
	for name, s := range c.Servers {
		if !serverNamePattern.MatchString(name) || len(name) > 32 {
			return fmt.Errorf("mcp: server name %q must match [A-Za-z0-9_-]+ and be <= 32 chars", name)
		}
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("mcp: server %q: command is required", name)
		}
		if !filepath.IsAbs(s.Command) {
			return fmt.Errorf("mcp: server %q: command must be an absolute path (no PATH lookup)", name)
		}
		if s.TimeoutSeconds < 0 || s.TimeoutSeconds > 3600 {
			return fmt.Errorf("mcp: server %q: timeoutSeconds must be 0..3600", name)
		}
		if s.Cwd != "" && !filepath.IsAbs(s.Cwd) {
			return fmt.Errorf("mcp: server %q: cwd must be absolute", name)
		}
		for key, value := range s.Env {
			if !envRefPattern.MatchString(value) {
				return fmt.Errorf("mcp: server %q: env %q value must be $ENV:NAME (secrets never live in mcp.json)", name, key)
			}
		}
		for toolName, policy := range s.Tools {
			if !serverNamePattern.MatchString(toolName) {
				return fmt.Errorf("mcp: server %q: tool policy %q has invalid name", name, toolName)
			}
			switch policy.Action {
			case "", "read", "write", "delete", "unknown":
			default:
				return fmt.Errorf("mcp: server %q: tool %q has invalid action %q", name, toolName, policy.Action)
			}
		}
	}
	return nil
}

// IsEnabled reports whether the server is on.
func (s ServerConfig) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Timeout returns the call timeout.
func (s ServerConfig) Timeout() int {
	if s.TimeoutSeconds <= 0 {
		return 60
	}
	return s.TimeoutSeconds
}
