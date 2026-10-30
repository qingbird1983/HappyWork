-- Phase 1 initial schema: sessions / messages / tool_calls / audit_log / events / kv.
-- See docs/design/phase-1/design.md §4.

CREATE TABLE sessions (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL DEFAULT '',
    workspace_root TEXT NOT NULL,
    state          TEXT NOT NULL DEFAULT 'idle',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    seq             INTEGER NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'system')),
    content         TEXT NOT NULL DEFAULT '',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    tool_name       TEXT NOT NULL DEFAULT '',
    tool_calls_json TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    UNIQUE (session_id, seq)
);

CREATE TABLE tool_calls (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    name        TEXT NOT NULL,
    args_json   TEXT NOT NULL,
    result_json TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'running',
    created_at  TEXT NOT NULL
);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    decision   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
);

-- kv is bootstrapped by the migrator before applying files, hence IF NOT EXISTS.
CREATE TABLE IF NOT EXISTS kv (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX idx_messages_session ON messages (session_id, seq);
CREATE INDEX idx_events_session ON events (session_id, id);
CREATE INDEX idx_tool_calls_session ON tool_calls (session_id);
