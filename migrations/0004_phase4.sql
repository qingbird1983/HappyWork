-- Phase 4: immutable audit chain and versioned file backups.
-- See docs/design/phase-4/design.md §4 and §5.

ALTER TABLE audit_log ADD COLUMN turn_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE audit_log ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN detail_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE audit_log ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN entry_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_audit_session ON audit_log (session_id, id);
CREATE INDEX idx_audit_trace ON audit_log (trace_id);

CREATE TABLE audit_chain (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    last_id    INTEGER NOT NULL DEFAULT 0,
    last_hash  TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO audit_chain (id) VALUES (1);

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only');
END;

CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only');
END;

-- A sealed row must continue the current head and carry a hash. Legacy rows
-- (empty hashes) are only allowed before the first sealed row.
CREATE TRIGGER audit_log_chain_insert BEFORE INSERT ON audit_log
WHEN (SELECT last_hash FROM audit_chain WHERE id = 1) <> ''
 AND (NEW.entry_hash = '' OR NEW.prev_hash <> (SELECT last_hash FROM audit_chain WHERE id = 1))
BEGIN
    SELECT RAISE(ABORT, 'audit chain mismatch');
END;

CREATE TABLE file_backups (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions(id),
    turn_id        INTEGER NOT NULL DEFAULT 0,
    rel_path       TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('overwrite', 'create')),
    reason         TEXT NOT NULL DEFAULT 'write',
    version        INTEGER NOT NULL,
    mode           INTEGER NOT NULL DEFAULT 420,
    size           INTEGER NOT NULL DEFAULT 0,
    backup_rel     TEXT NOT NULL DEFAULT '',
    backup_sha256  TEXT NOT NULL DEFAULT '',
    written_sha256 TEXT NOT NULL DEFAULT '',
    discarded_rel  TEXT NOT NULL DEFAULT '',
    restore_of     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    verified_at    TEXT NOT NULL DEFAULT '',
    restored_at    TEXT NOT NULL DEFAULT '',
    UNIQUE (session_id, rel_path, version)
);

CREATE INDEX idx_file_backups_session ON file_backups (session_id, created_at);
