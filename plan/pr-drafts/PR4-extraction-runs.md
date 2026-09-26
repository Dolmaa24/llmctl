# feat(storage): record extraction runs

## ⚠️ Interface change

This adds a type to `llmctl_API_INTERFACE_CONTRACT.md` §2, a repository to §8
and a migration, and corrects one line of §6. No existing signature changes.
Per `llmctl_GIT_WORKFLOW.md` §6 and contract §11, it is raised before the
storage side is built, and every consumer acknowledges before merge.

**Consumers to tag:**

- **Person 5** — `storage`, which gains the table and the repository, and
  `cmd/llmctl`, which wires the repository into the app
- **Person 3** — `ui/switchconfirm`, which shows the extraction cost this
  table keeps

## The gap

Three things the plan requires have nowhere to live.

1. **What extraction costs.** REQ-002 requires cost to be reported as
   C_total = C_handoff + C_extraction. `switch_events` records the handoff
   side. Nothing records the extraction side, which is the cost that decides
   whether distillation pays for itself at all (the crossover analysis,
   TASK-042).
2. **Where the next pass starts.** Contract §6 says extraction reads only the
   turns since the last pass. That point has to survive a restart. Without
   it, the first pass after every restart reads the whole transcript again,
   paying twice and writing near-duplicate notes.
3. **Which prompt wrote a note.** TASK-017 requires every note to be traceable
   to the prompt version that produced it. `Note` has no field for it.

The notes side is already built. `notes.Runner`, on `feature/dolmaa-p4`,
records a run for every pass through a two-method `Ledger` interface, backed
by memory for now. This proposal gives it a table.

## Proposed type — §2.4 `ExtractionRun`

```go
// ExtractionRun records one note-extraction pass: how far it read, which
// model and prompt did the reading, and what it cost.
type ExtractionRun struct {
    ID        string // assigned by storage
    SessionID string

    // ThroughSequenceNum is the last message the pass read. The next pass
    // starts after the furthest one of any completed run.
    ThroughSequenceNum int

    // The model that did the extracting. Notes carry the conversation's
    // model; this is the one that read it.
    Provider      string
    Model         string
    PromptVersion string

    InputTokens  int
    OutputTokens int

    NoteIDs []string // the notes the pass stored

    // Err is empty for a completed pass. A failed pass moves the read point
    // nowhere, but it was paid for, so it is stored all the same.
    Err string

    CreatedAt time.Time
}
```

**Owner:** Person 4 writes it; Person 5 persists it.
**Consumed by:** `storage/extraction_runs_repo`, `notes` (the runner),
`cmd/llmctl-bench` (the evaluation records the model and prompt of each run).

## Proposed repository — §8

```go
type ExtractionRunsRepo interface {
    Create(ctx context.Context, r *session.ExtractionRun) error
    ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error)
}
```

- `Create` stores the run, and one link per `NoteIDs` entry, in a single
  transaction, and assigns `ID` if it is empty. The notes already exist: the
  runner stores a pass's notes before recording its run, because a lost note
  is worse than turns read twice.
- `ListBySession` returns the runs oldest first, each with its `NoteIDs`.
- A failed run is stored like any other.

This has the same shape as `SwitchEventsRepo`. Once the runner records this
type (change 5 below), it is also exactly the runner's `Ledger`, so the
repository plugs in with no adapter.

## Proposed migration — `0003_extraction_runs.sql`

```sql
-- Every note-extraction pass: how far it read the transcript, which model and
-- prompt did the reading, and what it cost. This is the extraction side of
-- C_total = C_handoff + C_extraction, and the next pass starts after the
-- furthest message a completed run read. No message content or API key is
-- stored here.

CREATE TABLE IF NOT EXISTS extraction_runs (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    through_sequence_num  INTEGER NOT NULL,          -- the last message the pass read
    provider              TEXT NOT NULL,             -- the extraction model's, not the conversation's
    model                 TEXT NOT NULL,
    prompt_version        TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens         INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    error                 TEXT NOT NULL DEFAULT '',  -- empty for a completed pass
    created_at            DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_extraction_runs_session ON extraction_runs(session_id);

-- Which pass wrote which note, so any note traces to the model and prompt
-- version that produced it. Each note is written by exactly one pass.
CREATE TABLE IF NOT EXISTS extraction_run_notes (
    note_id  TEXT PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
    run_id   TEXT NOT NULL REFERENCES extraction_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_extraction_run_notes_run ON extraction_run_notes(run_id);
```

Why it looks like this:

- **Only `CREATE … IF NOT EXISTS`.** `Migrate` re-applies every file on every
  start, so each must be safe to run twice. That rules out
  `ALTER TABLE notes ADD COLUMN`, which fails the second time. It is why
  provenance is a link table rather than a column on `notes`, and it leaves
  the `notes` table untouched (NFR-10).
- **A link table, not a JSON array** like `notes.tags`. It keeps foreign keys,
  and it answers "which prompt wrote this note" with an index lookup. The
  primary key on `note_id` means every note was written by exactly one pass.
- **Failed runs are rows.** They cost tokens, so they count toward
  C_extraction. The query for where the next pass starts skips them.
- **`provider` and `model` are the extraction model's.** Notes already carry
  the conversation's.
- **No content.** Runs hold counts and identifiers, never message text or keys.

## Checked, not assumed

The migration was applied with the existing `Migrate` and the pure-Go driver,
in a scratch test that was not committed:

- It applies twice, and the five existing storage tests still pass beside it.
- It rejects negative token counts, unknown sessions, a missing prompt
  version, links to unknown notes or runs, and a note linked to two runs.
- Deleting a session removes its runs and their links.
- The queries below return the expected values on a session of three runs
  with a failed pass in the middle.

## Queries this makes possible

```sql
-- Where the next pass starts: completed runs only.
SELECT MAX(through_sequence_num) FROM extraction_runs
 WHERE session_id = ? AND error = '';

-- C_extraction so far, failed passes included.
SELECT SUM(input_tokens + output_tokens) FROM extraction_runs
 WHERE session_id = ?;

-- Extraction cost up to a switch.
SELECT SUM(r.input_tokens + r.output_tokens)
  FROM switch_events e
  JOIN extraction_runs r ON r.session_id = e.session_id
                        AND r.created_at <= e.created_at
 WHERE e.id = ?;

-- The prompt version that wrote a note.
SELECT r.prompt_version
  FROM extraction_run_notes l
  JOIN extraction_runs r ON r.id = l.run_id
 WHERE l.note_id = ?;
```

The switch query assumes `created_at` is written in one sortable format, as
in the other tables. Pricing groups the cost by `provider, model`, since
extraction and the conversation usually run on models with different prices.

## Deliberately not included

- **No column on `switch_events`.** TASK-024 asked for the cumulative
  extraction total there. The third query derives it, and a stored copy could
  drift from the runs it sums: the same reason the handoff estimate and the
  handoff are built from one `Payload` function.
- **No count of notes overturned.** It also derives from
  `notes.superseded_by`, through the link table.
- **No prices.** Tokens only, as in the `Estimator` PR. Pricing is the
  report's job, with the date the prices were checked.
- **No export change.** Adding runs to the §10 export changes a public format,
  so it is its own PR.
- **No all-or-nothing pass.** One transaction across a pass's notes, its
  supersession marks and its run would make a pass atomic. For now the
  runner orders its writes so that a failure costs a re-read, never a lost
  note. It is worth a follow-up once the repositories exist.

## Noticed while writing this

- §10's export gives notes no `id`, yet `links_to` and `superseded_by` hold
  note IDs, so neither can be resolved from the file.
- §6 says `session/handoff.go` triggers extraction. It cannot: `notes` imports
  `session`, so `session` cannot import `notes`. The app triggers it, through
  the runner.

## Requirements addressed

- Plan **REQ-002**, **TASK-017** and **TASK-020**; **TASK-024** answered by
  derivation
- SRS **NFR-6** — each run is written as its pass ends, not held until exit
- SRS **NFR-10** — the `notes` table is not restructured

## Changes in this PR

1. `internal/session/extractionrun.go` — the type above.
2. `internal/storage/repos.go` — `ExtractionRunsRepo`.
3. `internal/storage/migrations/0003_extraction_runs.sql`, with both tables
   added to `TestMigrateCreatesEverySchemaObject` and the constraint checks
   above as tests.
4. `internal/mock` — an in-memory `ExtractionRunsRepo`, replacing
   `mock.Ledger`.
5. `internal/notes` — the runner records `session.ExtractionRun` in place of
   `notes.Run`, and its count of overturned notes moves to `Result`. No
   behaviour changes.
6. Contract §2.4 and §8 as above. In §6, "Consumed by" changes from
   "`session/handoff.go` (triggers extraction)" to "`app`, through
   `notes.Runner`: `AfterReply` once a reply is stored, and `BeforeSwitch`
   before a switch is priced".
7. Technical Architecture §4 (files) and §5 (schema, and its relationships
   summary).

The SQLite implementation of `ExtractionRunsRepo` follows with Person 5's
other repository implementations, none of which exist yet.

## Checklist (`llmctl_GIT_WORKFLOW.md` §4)

- [x] Targets `main`
- [x] References the requirements it addresses
- [x] Migration checked against `Migrate` and the pure-Go driver
- [ ] Raised in the team channel before implementation (contract §11)
- [ ] CI green
- [ ] Contract and architecture documents updated in this PR
- [ ] Acknowledged by Person 5 (`storage`, `cmd/llmctl`)
- [ ] Acknowledged by Person 3 (`ui/switchconfirm`)
