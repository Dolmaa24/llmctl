# llmctl — API / Interface Contract Document

**Status:** Draft v1
**Purpose:** This document is the single source of truth for every interface boundary between modules. If your code implements or calls one of these interfaces, it must match this document exactly. Any change to a signature here requires updating this document and notifying the owners of every module that depends on it — this is what lets all 5 people build in parallel without integration surprises.
**Companion to:** llmctl_PRD.md, llmctl_TECHNICAL_ARCHITECTURE.md

---

## 1. How to use this document

Each interface below lists: the Go type/signature, what each method does, who owns the implementation, and who else calls it. Build against these signatures immediately — do not wait for the real implementation to exist. Mock implementations satisfying these interfaces should be written in week 1 so dependent modules can be developed and tested against the mock before the real thing is ready.

---

## 2. Canonical Data Types

These types are shared across nearly every module. They live in `internal/session/` and `internal/notes/` and must not be redefined elsewhere.

### 2.1 `Message`

```go
type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleSystem    Role = "system"
)

type Message struct {
    ID          string    // UUID
    SessionID   string
    SequenceNum int       // order within the session
    Role        Role
    Content     string
    Provider    string    // empty for user messages
    Model       string    // empty for user messages
    TokenCount  int       // 0 if not yet estimated
    CreatedAt   time.Time
}
```

**Owner:** Person 4 (session/notes module)
**Consumed by:** `ui/transcript`, `storage/messages_repo`, `provider/*` adapters (as input to `SendMessage`), `costestimate`

### 2.2 `Note`

```go
type Note struct {
    ID            string    // UUID
    SessionID     string
    Content       string    // the distilled fact/decision
    Provider      string    // which provider's turn this was extracted from
    Model         string
    Tags          []string  // e.g. ["decision", "concurrency"]
    LinksTo       *string   // nullable, ID of a related note
    SupersededBy  *string   // nullable, ID of the note that replaced this one
    CreatedAt     time.Time
}
```

**Owner:** Person 4 (notes module)
**Consumed by:** `ui/notespane`, `storage/notes_repo`, `session/handoff.go`

**Contract note:** `LinksTo` and `SupersededBy` must be populated wherever the extraction logic can determine them, even though nothing in this project's scope reads them for ranking. This is a hard requirement, not an optimization — it is what keeps a future retrieval upgrade additive (see PRD Section 8). Do not leave these fields permanently nil as a shortcut.

### 2.3 `SwitchEvent`

```go
type SwitchEvent struct {
    ID                          string
    SessionID                   string
    FromProvider, FromModel     string
    ToProvider, ToModel         string
    NotesSentCount              int
    RawTurnsSentCount           int
    EstimatedTokensFullReplay   int
    EstimatedTokensDistilled    int
    CreatedAt                   time.Time
}
```

**Owner:** Person 4 (handoff logic) writes this; Person 5 (storage) persists it
**Consumed by:** `ui/switchconfirm` (reads the estimate before confirming), `storage/switch_events_repo`

### 2.4 `ExtractionRun`

```go
type ExtractionRun struct {
    ID                 string
    SessionID          string
    ThroughSequenceNum int
    Provider           string
    Model              string
    PromptVersion      string
    InputTokens        int
    OutputTokens       int
    NoteIDs            []string
    Err                string
    CreatedAt          time.Time
}
```

**Owner:** Person 4 (notes module)
**Consumed by:** `storage/extraction_runs_repo`, `notes/runner.go`

---

## 3. Provider Adapter Interface

This is the most important interface in the system — every provider (Anthropic, OpenRouter, Ollama) implements it identically, and no other module should ever call a provider-specific function directly.

```go
package provider

type Adapter interface {
    // Name returns the provider's identifier, e.g. "anthropic", "openrouter", "ollama"
    Name() string

    // Validate checks that the stored credentials/connection actually work.
    // Returns nil on success, or a descriptive error the doctor panel can display.
    Validate(ctx context.Context) error

    // ListModels returns the model identifiers this provider currently exposes.
    // Used to validate a model ID before switching, per PRD's pre-flight validation requirement.
    ListModels(ctx context.Context) ([]string, error)

    // GetEnvVars returns the environment variables this provider needs exported
    // into the user's shell (e.g. ANTHROPIC_API_KEY), decrypted and ready to export.
    // Does NOT decide shell syntax — that is the shell module's job.
    GetEnvVars(ctx context.Context) (map[string]string, error)

    // SendMessage sends the given conversation (already assembled by session/handoff.go)
    // to this provider/model and returns the assistant's reply as a Message.
    SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error)

    // EstimateTokens returns an approximate token count for the given text,
    // using this provider's tokenizer if available, otherwise a reasonable approximation.
    EstimateTokens(text string) int
}
```

**Owner:** Person 2 (provider adapters)
**Consumed by:** `app` (root model, to invoke switches), `doctor/auth_check.go`, `session/handoff.go`, `costestimate`

**Contract notes:**
- Every adapter method must return a Go `error`, never panic — the diagnostics panel depends on catching and displaying provider errors gracefully.
- `SendMessage` must not silently truncate `history` — if it doesn't fit the model's context window, it must return a specific, typed error (`ErrContextTooLarge`) so `session/handoff.go` can react, not swallow the problem.
- New providers are added by implementing this interface only — no other file should need modification. This is the extension point referenced in the PRD's "nice to have: plugin system."

---

## 4. Shell Interface (Windows/WSL-specific)

```go
package shell

type Kind string

const (
    KindPowerShell Kind = "powershell"
    KindWSLBash    Kind = "wsl_bash"
)

type Detector interface {
    // Detect determines which shell context llmctl is currently running in.
    Detect() (Kind, error)
}

type Exporter interface {
    // Export writes the given env vars into the current shell session in the
    // correct syntax for the given Kind (e.g. `$env:X=` vs `export X=`).
    Export(kind Kind, vars map[string]string) error

    // Sync reconciles config between Windows and WSL so both see the same
    // provider configuration. Returns whether a sync was needed and performed.
    Sync() (synced bool, err error)
}
```

**Owner:** Person 1 (shell/doctor, requires Windows/WSL access)
**Consumed by:** `app` root model (after a switch, to actually export vars), `doctor/wsl_check.go`

**Contract note:** Every other module should treat `shell.Kind` as an opaque value and never branch on OS-specific logic directly. If you find yourself writing `if runtime.GOOS == "windows"` outside the `shell/` or `doctor/` packages, stop — that logic belongs behind this interface instead.

---

## 5. Diagnostics ("doctor") Interface

```go
package doctor

type Status string

const (
    StatusOK    Status = "ok"
    StatusWarn  Status = "warn"
    StatusFail  Status = "fail"
)

type CheckResult struct {
    Name      string
    Status    Status
    Message   string    // human-readable detail, shown in the status bar / doctor output
    CheckedAt time.Time
}

type Check interface {
    // Name is the identifier shown in the status bar, e.g. "wsl", "redis", "network", "auth:anthropic"
    Name() string

    // Run performs the check and returns its result. Must not block longer than
    // 2 seconds — long-running checks should use ctx cancellation and report a
    // timeout as StatusWarn, not hang the UI.
    Run(ctx context.Context) CheckResult
}

// Runner executes all registered checks, used by both the live status bar
// (continuous, low-frequency polling) and the one-shot `llmctl doctor` command.
type Runner interface {
    Register(c Check)
    RunAll(ctx context.Context) []CheckResult
}
```

**Owner:** Person 1 (doctor checks), Person 2 provides the `auth:<provider>` check implementations since they own adapter `Validate()`
**Consumed by:** `ui/statusbar`, `cmd/llmctl` (the `llmctl doctor` subcommand)

---

## 6. Notes Extraction Pipeline

```go
package notes

type Extractor interface {
    // Extract takes recent conversation turns and returns zero or more new
    // Note records. May call out to a provider adapter internally (a cheap/fast
    // model) — this is an implementation detail, not part of the contract.
    Extract(ctx context.Context, recent []session.Message, existing []session.Note) ([]session.Note, error)
}
```

**Owner:** Person 4
**Consumed by:** `session/handoff.go` (triggers extraction), `app` (if extraction runs continuously in the background — see PRD open question #1)

---

## 7. Session Handoff Assembly

```go
package session

type HandoffPlan struct {
    Notes           []Note
    RecentRawTurns  []Message
    EstimatedTokensFullReplay int
    EstimatedTokensDistilled  int
}

type HandoffBuilder interface {
    // BuildHandoff assembles what will actually be sent to the new provider
    // on a switch: the accumulated non-superseded notes plus the last N raw
    // turns. Does not send anything — this is a pure planning step so the UI
    // can show the cost comparison before the user confirms.
    BuildHandoff(ctx context.Context, sess *Session) (HandoffPlan, error)
}
```

**Owner:** Person 4
**Consumed by:** `ui/switchconfirm` (displays the plan and cost estimate), `app` (executes the switch once confirmed by calling the target adapter's `SendMessage` with `RecentRawTurns` plus a synthesized note-summary message)

---

## 8. Storage Repository Interfaces

One repository interface per table, all implemented against SQLite in `internal/storage/`.

```go
package storage

type SessionsRepo interface {
    Create(ctx context.Context, s *session.Session) error
    Get(ctx context.Context, id string) (*session.Session, error)
    UpdateActiveProvider(ctx context.Context, id, provider, model string) error
    List(ctx context.Context) ([]session.Session, error)
}

type MessagesRepo interface {
    Append(ctx context.Context, m *session.Message) error
    ListBySession(ctx context.Context, sessionID string) ([]session.Message, error)
}

type NotesRepo interface {
    Create(ctx context.Context, n *session.Note) error
    ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]session.Note, error)
    MarkSuperseded(ctx context.Context, noteID, supersededByID string) error
}

type SwitchEventsRepo interface {
    Create(ctx context.Context, e *session.SwitchEvent) error
    ListBySession(ctx context.Context, sessionID string) ([]session.SwitchEvent, error)
}

type ExtractionRunsRepo interface {
    Create(ctx context.Context, r *session.ExtractionRun) error
    ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error)
}
```

**Owner:** Person 5
**Consumed by:** Person 4's handoff/notes logic, `ui/*` (read-only, for rendering), `cmd/llmctl` export subcommand

**Contract note:** All repo methods take `context.Context` first and return a plain Go `error` — no repo method should be allowed to `os.Exit` or panic on a DB error; the caller decides how to surface it.

---

## 9. Config / Secrets Interface

```go
package config

type Store interface {
    // GetProviderConfig returns non-secret metadata (base URL, default model) for a provider.
    GetProviderConfig(providerID string) (ProviderConfig, error)
    SetProviderConfig(cfg ProviderConfig) error
}

type SecretStore interface {
    // GetAPIKey returns the decrypted API key for a provider, decrypting on demand.
    GetAPIKey(providerID string) (string, error)
    SetAPIKey(providerID, apiKey string) error
}
```

**Owner:** Person 5
**Consumed by:** `provider/*` adapters (via `GetAPIKey`), `app` init (loads config on startup)

---

## 10. JSON Export Schema (for session transcript/notes export)

This is the on-disk format produced by the export feature (PRD Feature 5) — treat this as a public contract even though it's just a file format, since users may script against it.

```json
{
  "session_id": "uuid",
  "title": "string",
  "exported_at": "RFC3339 timestamp",
  "messages": [
    {
      "role": "user | assistant | system",
      "content": "string",
      "provider": "string | null",
      "model": "string | null",
      "created_at": "RFC3339 timestamp"
    }
  ],
  "notes": [
    {
      "content": "string",
      "provider": "string",
      "model": "string",
      "tags": ["string"],
      "links_to": "note_id | null",
      "superseded_by": "note_id | null",
      "created_at": "RFC3339 timestamp"
    }
  ],
  "switch_events": [
    {
      "from": { "provider": "string", "model": "string" },
      "to": { "provider": "string", "model": "string" },
      "estimated_tokens_full_replay": 0,
      "estimated_tokens_distilled": 0,
      "created_at": "RFC3339 timestamp"
    }
  ]
}
```

**Owner:** Person 5 (export logic), field definitions owned jointly with Person 4

---

## 11. Change Control

Any change to a signature in this document must:
1. Be raised in the team channel before implementation, not after.
2. Update this document in the same pull request as the code change.
3. Be acknowledged by every module owner listed as a consumer of that interface.

Interfaces marked `Owner` above may only be implemented by that person's module — other modules depend on the interface, never on a concrete struct from another package.
