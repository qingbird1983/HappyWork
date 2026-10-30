-- Phase 2: approvals, trust/turn metadata and execution-lock columns.
-- See docs/design/phase-2/design.md §8.

ALTER TABLE sessions ADD COLUMN model_id TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN turn_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN trust TEXT NOT NULL DEFAULT 'trusted';

ALTER TABLE tool_calls ADD COLUMN turn_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tool_calls ADD COLUMN tool_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE tool_calls ADD COLUMN match_key TEXT NOT NULL DEFAULT '';
ALTER TABLE tool_calls ADD COLUMN risk TEXT NOT NULL DEFAULT '';
ALTER TABLE tool_calls ADD COLUMN reversible INTEGER NOT NULL DEFAULT 1;
ALTER TABLE tool_calls ADD COLUMN decision TEXT NOT NULL DEFAULT '';

CREATE TABLE approvals (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id),
    turn_id      INTEGER NOT NULL DEFAULT 0,
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_ref     TEXT NOT NULL,
    match_key    TEXT NOT NULL DEFAULT '',
    level        TEXT NOT NULL,
    decision     TEXT NOT NULL,
    scope        TEXT NOT NULL,
    args_json    TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    decided_by   TEXT NOT NULL DEFAULT 'user',
    decided_at   TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_approvals_grant ON approvals (session_id, match_key, scope, decision);
CREATE INDEX idx_approvals_call ON approvals (tool_call_id);
