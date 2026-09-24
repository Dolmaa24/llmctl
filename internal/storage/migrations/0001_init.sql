-- Schema per llmctl_TECHNICAL_ARCHITECTURE.md section 5.
-- This database holds session history only. API keys live in the separate
-- age-encrypted store and must never appear in any table here.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL DEFAULT '',
    active_provider TEXT NOT NULL DEFAULT '',
    active_model    TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sequence_num INTEGER NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content      TEXT NOT NULL,
    provider     TEXT,              -- null for user messages
    model        TEXT,              -- null for user messages
    token_count  INTEGER,           -- null until estimated
    created_at   DATETIME NOT NULL,
    UNIQUE (session_id, sequence_num)
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, sequence_num);

CREATE TABLE IF NOT EXISTS notes (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    content       TEXT NOT NULL,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    tags          TEXT NOT NULL DEFAULT '[]',  -- JSON array
    links_to      TEXT REFERENCES notes(id),
    superseded_by TEXT REFERENCES notes(id),
    created_at    DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notes_session ON notes(session_id);
-- Handoff assembly reads only non-superseded notes, so index that predicate.
CREATE INDEX IF NOT EXISTS idx_notes_active ON notes(session_id) WHERE superseded_by IS NULL;

CREATE TABLE IF NOT EXISTS providers (
    id            TEXT PRIMARY KEY,   -- 'anthropic', 'openrouter', 'ollama'
    display_name  TEXT NOT NULL,
    base_url      TEXT NOT NULL DEFAULT '',
    default_model TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);
