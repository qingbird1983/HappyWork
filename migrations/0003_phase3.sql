-- Phase 3: session presets, execHash execution locks and persistent rules.
-- See docs/design/phase-3/design.md §10.

ALTER TABLE sessions ADD COLUMN preset TEXT NOT NULL DEFAULT 'default';
ALTER TABLE approvals ADD COLUMN exec_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE approvals ADD COLUMN content_trust TEXT NOT NULL DEFAULT 'trusted';
ALTER TABLE tool_calls ADD COLUMN exec_hash TEXT NOT NULL DEFAULT '';

CREATE TABLE permission_rules (
    id           TEXT PRIMARY KEY,
    tool_ref     TEXT NOT NULL,
    action       TEXT NOT NULL,
    match_key    TEXT NOT NULL,
    decision     TEXT NOT NULL DEFAULT 'allow' CHECK (decision = 'allow'),
    note         TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT '',
    use_count    INTEGER NOT NULL DEFAULT 0,
    UNIQUE (tool_ref, action, match_key)
);

CREATE INDEX idx_permission_rules_match ON permission_rules (tool_ref, action, match_key);
