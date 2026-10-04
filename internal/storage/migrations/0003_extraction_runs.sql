-- Every note-extraction pass: how far it read the transcript, which model
-- and prompt did the reading, and what it cost. This is the extraction side
-- of C_total = C_handoff + C_extraction. No message content or API keys stored.

CREATE TABLE IF NOT EXISTS extraction_runs (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    through_sequence_num  INTEGER NOT NULL,
    provider              TEXT NOT NULL,
    model                 TEXT NOT NULL,
    prompt_version        TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens         INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    error                 TEXT NOT NULL DEFAULT '',
    created_at            DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_extraction_runs_session ON extraction_runs(session_id);

-- Which pass wrote which note (one note = exactly one pass).
CREATE TABLE IF NOT EXISTS extraction_run_notes (
    note_id  TEXT PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
    run_id   TEXT NOT NULL REFERENCES extraction_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_extraction_run_notes_run ON extraction_run_notes(run_id);
