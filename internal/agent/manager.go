package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/rollback"
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
	// Presets is the loaded preset catalog. Nil uses the builtins.
	Presets permission.PresetSet
	// Hooks is the loaded in-process hook runner. Nil disables hooks.
	Hooks *hook.Runner
}

// Manager creates sessions and routes commands to them. One Manager per app.
type Manager struct {
	store           storage.Store
	prov            model.Provider
	emit            Emitter
	log             *log.Logger
	opts            Options
	engine          *permission.Engine
	hooks           *hook.Runner
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
		engine:          permission.NewEngine(opts.Presets, ruleStore{store: store}),
		hooks:           opts.Hooks,
		budget:          DefaultBudget(),
		clock:           realClock{},
		approvalTimeout: defaultApprovalTimeout,
		sessions:        map[string]*Session{},
	}
}

// ruleStore adapts storage to the permission engine's grant interfaces.
type ruleStore struct{ store storage.Store }

func (r ruleStore) MatchRule(ctx context.Context, toolRef, action, matchKey string) (permission.Rule, bool, error) {
	row, ok, err := r.store.MatchPermissionRule(ctx, toolRef, action, matchKey)
	if err != nil || !ok {
		return permission.Rule{}, ok, err
	}
	return permission.Rule{ID: row.ID, ToolRef: row.ToolRef, Action: row.Action, MatchKey: row.MatchKey}, true, nil
}

func (r ruleStore) TouchRule(ctx context.Context, id string) error {
	return r.store.TouchPermissionRule(ctx, id)
}

func (r ruleStore) HasSessionGrant(ctx context.Context, sessionID, matchKey string) (bool, error) {
	return r.store.HasSessionApproval(ctx, sessionID, matchKey)
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
func (m *Manager) newRegistry(root workspace.Root, backups *rollback.Writer) *tool.Registry {
	reg := tool.NewRegistry()
	read := tool.NewReadFile(root)
	list := tool.NewListDir(root)
	write := tool.NewWriteFile(root, backups)
	for _, t := range []tool.Tool{read, list, write} {
		meta := t.Meta()
		permission.MustCheckTool(meta.Ref, meta.Action, meta.Reversible)
	}
	reg.Register(read)
	reg.Register(list)
	reg.Register(write)
	if m.opts.ExternalTools != nil {
		reg.SetExternal(m.opts.ExternalTools)
	}
	return reg
}

func (m *Manager) backupWriter(root workspace.Root, sessionID string) *rollback.Writer {
	return rollback.NewWriter(root, m.dataPath("backups"), sessionID, m.store, rollback.DefaultLimits())
}

// dataPath joins a subdirectory under the app data dir.
func (m *Manager) dataPath(elem ...string) string {
	if m.opts.DataDir == "" {
		return ""
	}
	parts := append([]string{m.opts.DataDir}, elem...)
	return filepath.Join(parts...)
}

// CreateSession validates the workspace root and preset, persists the session
// and keeps it in memory. Without a preset the "default" profile is used.
func (m *Manager) CreateSession(ctx context.Context, workspaceRoot string, preset ...string) (storage.Session, error) {
	root, err := workspace.NewRoot(workspaceRoot)
	if err != nil {
		return storage.Session{}, fmt.Errorf("agent: %w", err)
	}
	name := "default"
	if len(preset) > 0 && preset[0] != "" {
		name = preset[0]
	}
	if !m.engine.HasPreset(name) {
		return storage.Session{}, fmt.Errorf("agent: unknown preset %q", name)
	}
	sess := storage.Session{
		ID:            storage.NewID(),
		Title:         "新会话",
		WorkspaceRoot: root.String(),
		ModelID:       m.modelName(),
		Preset:        name,
		State:         StateIdle,
	}
	if err := m.store.CreateSession(ctx, &sess); err != nil {
		return storage.Session{}, fmt.Errorf("agent: create session: %w", err)
	}
	// The tool registry and backup writer are per session: every filesystem
	// tool is bound to this session's canonical workspace root (ADR-0004).
	backups := m.backupWriter(root, sess.ID)
	s := newSession(m, sess, root, m.newRegistry(root, backups), backups)
	m.mu.Lock()
	m.sessions[sess.ID] = s
	m.mu.Unlock()
	return sess, nil
}

// PresetNames returns the selectable profiles.
func (m *Manager) PresetNames() []string { return m.engine.PresetNames() }

// Presets returns the selectable profiles with descriptions.
func (m *Manager) Presets() []permission.Preset { return m.engine.Presets() }

// ListRules returns persistent permission rules.
func (m *Manager) ListRules(ctx context.Context) ([]storage.PermissionRule, error) {
	return m.store.ListPermissionRules(ctx)
}

// DeleteRule revokes a persistent permission rule.
func (m *Manager) DeleteRule(ctx context.Context, id string) error {
	return m.store.DeletePermissionRule(ctx, id)
}

// ListAudit returns recent audit rows plus the global chain status.
func (m *Manager) ListAudit(ctx context.Context, sessionID string, limit int) (audit.Page, error) {
	entries, err := m.store.ListAudit(ctx, sessionID, limit)
	if err != nil {
		return audit.Page{}, err
	}
	all, err := m.store.ListAudit(ctx, sessionID)
	if err != nil {
		return audit.Page{}, err
	}
	verify, err := m.store.VerifyAudit(ctx)
	if err != nil {
		return audit.Page{}, err
	}
	return audit.Page{Entries: entries, Verify: verify, Total: len(all)}, nil
}

// VerifyAudit recomputes the audit hash chain.
func (m *Manager) VerifyAudit(ctx context.Context) (audit.VerifyResult, error) {
	return m.store.VerifyAudit(ctx)
}

// ExportAudit writes JSONL + manifest evidence under the app data dir.
func (m *Manager) ExportAudit(ctx context.Context) (audit.ExportResult, error) {
	if m.opts.DataDir == "" {
		return audit.ExportResult{}, fmt.Errorf("agent: app data directory is unavailable")
	}
	rows, err := m.store.ListAudit(ctx, "")
	if err != nil {
		return audit.ExportResult{}, err
	}
	verify, err := m.store.VerifyAudit(ctx)
	if err != nil {
		return audit.ExportResult{}, err
	}
	return audit.Export(filepath.Join(m.opts.DataDir, "exports"), rows, verify)
}

// ListBackups returns version groups for one session.
func (m *Manager) ListBackups(ctx context.Context, sessionID string) ([]rollback.BackupGroup, error) {
	s, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.ListBackups(ctx)
}

// RestoreBackup restores one version after creating a safety copy.
func (m *Manager) RestoreBackup(ctx context.Context, sessionID, backupID string) (rollback.RestoreOutcome, error) {
	s, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return rollback.RestoreOutcome{}, err
	}
	return s.RestoreBackup(ctx, backupID)
}

// UndoCreatedFile moves an unchanged created file into the backup area.
func (m *Manager) UndoCreatedFile(ctx context.Context, sessionID, backupID string) (rollback.RestoreOutcome, error) {
	s, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return rollback.RestoreOutcome{}, err
	}
	return s.UndoCreatedFile(ctx, backupID)
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
	backups := m.backupWriter(root, info.ID)
	s = newSession(m, info, root, m.newRegistry(root, backups), backups)
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
