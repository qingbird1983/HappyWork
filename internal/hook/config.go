package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Factory builds one compiled-in hook from validated params. Unknown params
// keys must be rejected here so the config file stays strict.
type Factory func(params map[string]any) (Hook, error)

// Registry maps config names to compiled-in factories. Config can only select
// code that is already part of the binary (ADR-0013).
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{factories: map[string]Factory{}} }

// Register adds a factory; duplicate names panic (programming error).
func (r *Registry) Register(name string, f Factory) {
	if _, dup := r.factories[name]; dup {
		panic(fmt.Sprintf("hook: duplicate factory %q", name))
	}
	r.factories[name] = f
}

// Builtins returns the shipped hook factories.
func Builtins() *Registry {
	r := NewRegistry()
	r.Register("secret-scan", newSecretScan)
	return r
}

// Info reports the loaded configuration for AppInfo and the startup log.
type Info struct {
	Fingerprint string
	Enabled     []string
}

type hookFile struct {
	Hooks []hookEntryConfig `yaml:"hooks"`
}

type hookEntryConfig struct {
	Name      string         `yaml:"name"`
	Enabled   *bool          `yaml:"enabled"`
	Priority  *int           `yaml:"priority"`
	TimeoutMS *int           `yaml:"timeout_ms"`
	OnError   string         `yaml:"on_error"`
	Params    map[string]any `yaml:"params"`
}

// LoadDir loads hooks.d/*.yaml. Any invalid file disables the whole hook
// configuration (fail closed, hooks.md §4); the caller keeps running without
// hooks and surfaces the error.
func LoadDir(dir string, reg *Registry, audit AuditFunc) (*Runner, Info, error) {
	if dir == "" {
		return NewRunner(nil, audit), Info{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return NewRunner(nil, audit), Info{}, nil
		}
		return nil, Info{}, fmt.Errorf("hooks: read %s: %w", dir, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".yaml", ".yml":
			files = append(files, entry.Name())
		}
	}
	if len(files) == 0 {
		return NewRunner(nil, audit), Info{}, nil
	}
	sort.Strings(files)

	var (
		entriesOut []Entry
		names      []string
		seen       = map[string]bool{}
	)
	hash := sha256.New()
	for _, name := range files {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, Info{}, fmt.Errorf("hooks: %s: %w", name, err)
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		rawEntries, err := parseFile(data)
		if err != nil {
			return nil, Info{}, fmt.Errorf("hooks: %s: %w", name, err)
		}
		for i, raw := range rawEntries {
			if seen[raw.Name] {
				return nil, Info{}, fmt.Errorf("hooks: %s: hooks[%d]: duplicate name %q", name, i, raw.Name)
			}
			seen[raw.Name] = true
			entry, enabled, err := buildEntry(raw, reg)
			if err != nil {
				return nil, Info{}, fmt.Errorf("hooks: %s: hooks[%d]: %w", name, i, err)
			}
			if enabled {
				entriesOut = append(entriesOut, entry)
				names = append(names, raw.Name)
			}
		}
	}
	info := Info{Fingerprint: hex.EncodeToString(hash.Sum(nil)), Enabled: names}
	return NewRunner(entriesOut, audit), info, nil
}

func parseFile(data []byte) ([]hookEntryConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw hookFile
	if err := dec.Decode(&raw); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("decode: multiple YAML documents are not supported")
	}
	return raw.Hooks, nil
}

func buildEntry(raw hookEntryConfig, reg *Registry) (Entry, bool, error) {
	if strings.TrimSpace(raw.Name) == "" {
		return Entry{}, false, fmt.Errorf("name is required")
	}
	factory, ok := reg.factories[raw.Name]
	if !ok {
		return Entry{}, false, fmt.Errorf("unknown hook %q (only compiled-in hooks are allowed)", raw.Name)
	}
	h, err := factory(raw.Params)
	if err != nil {
		return Entry{}, false, fmt.Errorf("%s: %w", raw.Name, err)
	}
	timeout := DefaultTimeout
	if raw.TimeoutMS != nil {
		if *raw.TimeoutMS <= 0 || *raw.TimeoutMS > int(MaxTimeout.Milliseconds()) {
			return Entry{}, false, fmt.Errorf("timeout_ms must be within 1..%d", MaxTimeout.Milliseconds())
		}
		timeout = time.Duration(*raw.TimeoutMS) * time.Millisecond
	}
	onError := raw.OnError
	if onError == "" {
		onError = OnErrorWarn
	}
	switch onError {
	case OnErrorWarn, OnErrorDeny, OnErrorFailClosed:
	default:
		return Entry{}, false, fmt.Errorf("unknown on_error %q", onError)
	}
	priority := h.Priority()
	if raw.Priority != nil {
		priority = *raw.Priority
	}
	enabled := true
	if raw.Enabled != nil {
		enabled = *raw.Enabled
	}
	return Entry{Hook: h, Priority: priority, Timeout: timeout, OnError: onError}, enabled, nil
}
