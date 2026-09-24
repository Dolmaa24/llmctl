-- Every switch records what was sent and what the naive alternative would have
-- cost. This table is the evidence behind the pre-switch comparison screen.

CREATE TABLE IF NOT EXISTS switch_events (
    id                            TEXT PRIMARY KEY,
    session_id                    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    from_provider                 TEXT NOT NULL DEFAULT '',
    from_model                    TEXT NOT NULL DEFAULT '',
    to_provider                   TEXT NOT NULL,
    to_model                      TEXT NOT NULL,
    notes_sent_count              INTEGER NOT NULL DEFAULT 0,
    raw_turns_sent_count          INTEGER NOT NULL DEFAULT 0,
    estimated_tokens_full_replay  INTEGER NOT NULL DEFAULT 0,
    estimated_tokens_distilled    INTEGER NOT NULL DEFAULT 0,
    created_at                    DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_switch_events_session ON switch_events(session_id);
