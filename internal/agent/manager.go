package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
	"github.com/happywork/happywork/internal/workspace"
)

// Emitter forwards events to the desktop layer (which owns the Wails binding).
type Emitter interface {
	Emit(name string, data any)
}

// ErrBusy is returned by Send when the session already has a run in flight.
var ErrBusy = errors.New("agent: session is busy")

// Options carries app-level paths and tunables the session layer needs.
type Options struct {
	// DataDir is the per-user app data directory (attachments, backups,
	// MCP working dirs). Tests pass a temp dir.
	DataDir string
	// ModelName labels usage/cost events and cost estimation. Default "mock".
	ModelName string
	// ExternalTools is the live MCP snapshot (tool.Registry.SetExternal).
	ExternalTools func() []tool.Tool
}

// Manager creates sessions and routes commands to them. One Manager per app.
type Manager struct {
	store           storage.Store
	prov            model.Provider
	emit            Emitter
	log             *log.Logger
	opts            Options
	gate            permission.Gate
	budget          BudgetConfig
	clock           Clock
	approvalTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager wires the dependencies. emit may be nil in tests (events are
// still persisted via the store).
func NewManager(store storage.Store, prov model.Provider, emit Emitter, lg *log.Logger, opts Options) *Manager {
	if lg == nil {
		lg = log.Default()
	}
	return &Manager{
		store:           store,
		prov:            prov,
		emit:            emit,
		log:             lg,
		opts:            opts,
		budget:          DefaultBudget(),
		clock:           realClock{},
		approvalTimeout: defaultApprovalTimeout,
		sessions:        map[string]*Session{},
	}
}

func (m *Manager) modelName() string {
	if m.opts.ModelName != "" {
		return m.opts.ModelName
	}
	return "mock"
}

// newRegistry builds the per-session tool set bound to the canonical root.
// Every filesystem tool is bound to this session's root (ADR-0004); writes and
// oversized results go under the app data dir.
func (m *Manager) newRegistry(root workspace.Root, sessionID string) *tool.Registry {
	reg := tool.NewRegistry()
	reg.Register(tool.NewReadFile(root))
	reg.Register(tool.NewListDir(root))
	reg.Register(tool.NewWriteFile(root, m.dataPath("backups"), sessionID))
	if m.opts.ExternalTools != nil {
		reg.SetExternal(m.opts.ExternalTools)
	}
	return reg
}

// dataPath joins a subdirectory under the app data dir.
func (m *Manager) dataPath(elem ...string) string {
	if m.opts.DataDir == "" {
		return ""
	}
	parts := append([]string{m.opts.DataDir}, elem...)
	return filepath.Join(parts...)
}

// CreateSession validates the workspace root, persists the session and keeps
// it in memory.
func (m *Manager) CreateSession(ctx context.Context, workspaceRoot string) (storage.Session, error) {
	root, err := workspace.NewRoot(workspaceRoot)
	if err != nil {
		return storage.Session{}, fmt.Errorf("agent: %w", err)
	}
	sess := storage.Session{
		ID:            storage.NewID(),
		Title:         "新会话",
		WorkspaceRoot: root.String(),
		ModelID:       m.modelName(),
		State:         StateIdle,
	}
	if err := m.store.CreateSession(ctx, &sess); err != nil {
		return storage.Session{}, fmt.Errorf("agent: create session: %w", err)
	}
	// The tool registry is per session: every filesystem tool is bound to
	// this session's canonical workspace root (ADR-0004).
	s := newSession(m, sess, root, m.newRegistry(root, sess.ID))
	m.mu.Lock()
	m.sessions[sess.ID] = s
	m.mu.Unlock()
	return sess, nil
}

// GetSession returns the in-memory session; unknown IDs that exist in the
// store are rehydrated.
func (m *Manager) GetSession(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if ok {
		return s, nil
	}
	info, err := m.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	root, err := workspace.NewRoot(info.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("agent: rehydrate workspace: %w", err)
	}
	reg := m.newRegistry(root, info.ID)
	s = newSession(m, info, root, reg)
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

// ListSessions returns persisted sessions ordered by recency.
func (m *Manager) ListSessions(ctx context.Context) ([]storage.Session, error) {
	return m.store.ListSessions(ctx)
}

// Store exposes the persistence layer for read-only service methods.
func (m *Manager) Store() storage.Store { return m.store }

// Emitter returns the event bridge handed in at construction, so the desktop
// layer can bind it to the real application (desktop.NewEmitter contract).
func (m *Manager) Emitter() Emitter { return m.emit }

// emitEvent fans an event out to the desktop layer and the events table.
func (m *Manager) emitEvent(sessionID, name string, payload any) {
	if m.emit != nil {
		m.emit.Emit(name, payload)
	}
	if err := m.store.AppendEvent(context.Background(), sessionID, name, storage.MustJSON(payload)); err != nil {
		m.log.Printf("agent: persist event %s: %v", name, err)
	}
}
