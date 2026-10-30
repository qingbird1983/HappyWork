package mcp

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/happywork/happywork/internal/tool"
)

// Manager owns all configured MCP clients and exposes their tool snapshot.
type Manager struct {
	dataDir string
	log     *log.Logger
	clients []*Client
}

// NewManager builds clients for enabled servers (sorted for determinism).
func NewManager(cfg Config, dataDir string, lg *log.Logger) *Manager {
	if lg == nil {
		lg = log.Default()
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	m := &Manager{dataDir: dataDir, log: lg}
	for _, name := range names {
		server := cfg.Servers[name]
		if !server.IsEnabled() {
			continue
		}
		m.clients = append(m.clients, NewClient(name, server, dataDir, lg))
	}
	return m
}

// Start launches every server concurrently, bounded by a 15s overall wait.
// Failures disable that server for the run; the app still starts.
func (m *Manager) Start(ctx context.Context) {
	overall, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, client := range m.clients {
		client := client
		wg.Add(1)
		go func() {
			defer wg.Done()
			serverCtx, cancel := context.WithTimeout(overall, startupTimeout)
			defer cancel()
			if err := client.Start(serverCtx); err != nil {
				m.log.Printf("mcp: %v", err)
			}
		}()
	}
	wg.Wait()
}

// Tools returns a fresh snapshot; a restarted server's rediscovered tools are
// reflected without rebuilding sessions.
func (m *Manager) Tools() []tool.Tool {
	out := make([]tool.Tool, 0)
	for _, client := range m.clients {
		out = append(out, client.Tools()...)
	}
	return out
}

// Close terminates all server processes.
func (m *Manager) Close() {
	var wg sync.WaitGroup
	for _, client := range m.clients {
		client := client
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Close()
		}()
	}
	wg.Wait()
}

// Status returns per-server state for diagnostics.
func (m *Manager) Status() []ServerStatus {
	out := make([]ServerStatus, 0, len(m.clients))
	for _, client := range m.clients {
		client.mu.Lock()
		status := ServerStatus{Name: client.name, Disabled: client.disabled, Tools: len(client.tools), Modern: client.modern}
		client.mu.Unlock()
		out = append(out, status)
	}
	return out
}

// ServerStatus is a read-only diagnostic row.
type ServerStatus struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Tools    int    `json:"tools"`
	Modern   bool   `json:"modern"`
}
