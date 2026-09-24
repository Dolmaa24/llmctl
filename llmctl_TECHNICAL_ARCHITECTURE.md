# Technical Architecture Document: llmctl

**Status:** Draft v1
**Companion to:** llmctl_PRD.md
**Audience:** 5-person build team

---

## 1. Architectural Principles

A few decisions upstream shape everything below, so stating them explicitly first:

- **Local-first, single binary.** No server component. The Go binary and a local SQLite file are the entire runtime footprint.
- **Interface-first module boundaries.** Provider adapters, shell handling, and storage are all built behind interfaces so the 5-person team can build and test modules independently, and so future retrieval-based memory or new platforms slot in without touching unrelated code (per the PRD's Section 8 extensibility notes).
- **Structured data over blobs.** Notes, messages, and switch events are rows with explicit schema, not flat text files — this is the decision that keeps the future retrieval upgrade additive.
- **OS-specific code stays isolated.** Everything that only works on Windows/WSL (shell detection, `doctor` checks) sits behind an interface with a single implementation today, so it doesn't leak into the 90% of the codebase that's OS-agnostic.

---

## 2. Recommended Tech Stack

| Layer | Choice | Reasoning |
|---|---|---|
| Language | **Go 1.22+** | Compiles to a single static binary — no runtime to install, which matters a lot for a CLI/TUI tool. Strong built-in concurrency (goroutines) for running note extraction and diagnostics checks in the background without blocking the UI. Native cross-compilation to Windows/macOS/Linux and ARM64 with a single flag, which matters directly for your later multi-platform goal. |
| TUI framework | **Bubble Tea** | The standard for this exact category of tool (`lazygit`, `k9s`, `gh dash` all use it). Uses the Elm architecture (Model-Update-View), which gives you a predictable, testable state machine instead of ad-hoc terminal rendering — important with 5 people touching the UI layer. |
| Styling | **Lip Gloss** | Pairs natively with Bubble Tea. Handles borders, colors, layout without hand-rolling ANSI codes. |
| Prebuilt components | **Bubbles** (`viewport`, `spinner`, `table`, `textinput`) | Official component library for Bubble Tea — scrollable transcript view and provider table come largely for free. |
| CLI entry/subcommands | **Cobra** | Lets `llmctl` support both the default interactive TUI launch and non-interactive subcommands (`llmctl doctor`, `llmctl init`) from one binary — useful for scripting and CI-style checks later. |
| Local database | **SQLite** via `modernc.org/sqlite` | Zero-config, single-file, no server. Specifically the pure-Go driver (not `mattn/go-sqlite3`) to avoid a CGO/C-toolchain dependency — this matters because it keeps cross-compilation (including to Windows from a non-Windows dev machine, relevant since part of your team is on macOS) simple and fast. |
| Config encryption | **age** (`filippo.io/age`) | Modern, minimal, well-reviewed encryption library. Avoids the classic mistake of rolling custom crypto for API key storage. |
| Credential storage | **go-keyring** (`zalando/go-keyring`) | Cross-platform wrapper over Windows Credential Manager, macOS Keychain, and Linux Secret Service — resolves PRD open question #2, and means the OS-agnostic code path is already correct for the later macOS/Linux release even though v1 only ships Windows. |
| Non-secret config format | **TOML** (`BurntSushi/toml`) | Simpler and less error-prone than YAML for hand-edited config files; common convention in the Go CLI ecosystem (Hugo, etc.). |
| HTTP/provider calls | Standard library `net/http` + `hashicorp/go-retryablehttp` | Provider APIs are plain HTTP; no need for a heavy SDK dependency per provider. Retry wrapper handles transient network failures gracefully, which matters for the `doctor` diagnostics use case specifically. |
| Logging | **`log/slog`** (Go standard library) | Structured logging built into Go 1.21+, no external dependency needed. |
| DB migrations | Embedded SQL files via `embed.FS` + a minimal custom runner | Keeps the binary self-contained (migrations ship inside the binary, not as separate files the user could lose or edit) without pulling in a full migration framework for what is a small, well-understood schema. |
| Testing | Standard `testing` package + `testify` (assertions only) | Keep it close to idiomatic Go; `testify` just reduces assertion boilerplate. |
| Build/release | **GoReleaser** | Automates cross-platform builds and GitHub release packaging — set this up early even though v1 ships Windows-only, since it costs almost nothing now and saves real time when macOS/Linux builds are added. |

---

## 3. High-Level Module Map

```
Bubble Tea root model (internal/app)
        │
        ├── ui/          — provider pane, transcript, notes pane, status bar (rendering only)
        ├── provider/     — adapter interface + Anthropic/OpenRouter/Ollama implementations
        ├── shell/        — PowerShell/WSL detection, env var export (Windows/WSL-specific)
        ├── doctor/        — diagnostics checks (WSL, Redis, network, auth)
        ├── session/        — session + message state, handoff assembly logic
        ├── notes/           — note extraction pipeline
        ├── storage/          — SQLite access layer (repos per table)
        ├── config/            — encrypted config + keyring integration
        └── costestimate/        — token/cost estimation for the switch confirmation screen
```

Everything under `provider/`, `notes/`, `session/`, `storage/`, `costestimate/`, and most of `ui/` is OS-agnostic. Only `shell/` and `doctor/` are Windows/WSL-specific today — this is the boundary your Mac-based team members should build entirely outside of.

---

## 4. Complete File and Folder Structure

```
llmctl/
├── cmd/
│   └── llmctl/
│       └── main.go                     # entrypoint; wires Cobra root command + launches Bubble Tea
│
├── internal/
│   ├── app/                            # Bubble Tea root model — owns overall app state
│   │   ├── model.go                    # root Model struct, Init()
│   │   ├── update.go                   # top-level Update(), routes msgs to sub-models
│   │   ├── view.go                     # top-level View(), composes panes
│   │   └── keys.go                     # keybinding definitions
│   │
│   ├── ui/
│   │   ├── providerpane/
│   │   │   └── providerpane.go         # left-side provider list + active state
│   │   ├── transcript/
│   │   │   └── transcript.go           # scrollable, attributed message view
│   │   ├── notespane/
│   │   │   └── notespane.go            # extracted notes view (Obsidian-style list)
│   │   ├── statusbar/
│   │   │   └── statusbar.go            # live doctor status bar
│   │   ├── switchconfirm/
│   │   │   └── switchconfirm.go        # pre-switch cost comparison modal
│   │   └── styles/
│   │       └── styles.go               # shared Lip Gloss color/style definitions
│   │
│   ├── provider/
│   │   ├── adapter.go                  # Adapter interface: Validate, ListModels, GetEnvVars, SendMessage
│   │   ├── registry.go                 # maps provider name -> adapter instance
│   │   ├── anthropic/
│   │   │   └── anthropic.go
│   │   ├── openrouter/
│   │   │   └── openrouter.go
│   │   └── ollama/
│   │       └── ollama.go
│   │
│   ├── shell/                          # Windows/WSL-specific — isolated module
│   │   ├── detect.go                   # detects PowerShell vs WSL/Bash
│   │   ├── powershell.go               # PowerShell env var export logic
│   │   └── wsl.go                      # WSL env var export + Windows<->WSL sync
│   │
│   ├── doctor/                         # Windows/WSL-specific — isolated module
│   │   ├── checks.go                   # Check interface + runner
│   │   ├── wsl_check.go
│   │   ├── redis_check.go
│   │   ├── network_check.go
│   │   └── auth_check.go               # per-provider auth validity (OS-agnostic, lives here for cohesion)
│   │
│   ├── session/
│   │   ├── message.go                  # canonical Message struct (role, content, provider, model, tokens)
│   │   ├── session.go                  # Session struct, lifecycle methods
│   │   └── handoff.go                  # assembles distilled notes + last N raw turns for a switch
│   │
│   ├── notes/
│   │   ├── extractor.go                # turns recent conversation into structured Note records
│   │   └── prompt.go                   # the extraction prompt template sent to the cheap/extraction model
│   │
│   ├── storage/
│   │   ├── db.go                       # opens SQLite connection, runs migrations on startup
│   │   ├── migrations/
│   │   │   ├── 0001_init.sql
│   │   │   └── 0002_switch_events.sql
│   │   ├── sessions_repo.go
│   │   ├── messages_repo.go
│   │   ├── notes_repo.go
│   │   └── switch_events_repo.go
│   │
│   ├── config/
│   │   ├── config.go                   # loads/saves non-secret TOML config
│   │   ├── secrets.go                  # age-encrypted secrets file read/write
│   │   └── keyring.go                  # OS keyring integration for the encryption passphrase
│   │
│   └── costestimate/
│       └── estimate.go                 # token counting + cost lookup per provider/model
│
├── docs/
│   ├── llmctl_PRD.md
│   └── llmctl_TECHNICAL_ARCHITECTURE.md
│
├── scripts/
│   └── dev_setup.sh                    # one-command local dev environment bootstrap
│
├── .goreleaser.yaml
├── .gitignore                          # must exclude local config/secrets/db paths, see Section 6
├── Makefile                            # build, test, lint shortcuts
├── go.mod
├── go.sum
└── README.md
```

---

## 5. Database Schema (SQLite)

The database holds session, message, note, and switch-event history — never API keys or secrets (those live in the separate encrypted config store, Section 6). One SQLite file per user, created on first run.

### `sessions`
The top-level record for one working session in the TUI.

| Field | Type | Notes |
|---|---|---|
| `id` | TEXT (UUID), PK | |
| `title` | TEXT | User-editable or auto-generated from first message |
| `active_provider` | TEXT | Which provider is currently active for this session |
| `active_model` | TEXT | Which model is currently active |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | |

*In plain English:* one row per "conversation" the user has open. Everything else hangs off this.

### `messages`
Every message in a session's transcript, raw and attributed.

| Field | Type | Notes |
|---|---|---|
| `id` | TEXT (UUID), PK | |
| `session_id` | TEXT, FK → `sessions.id` | |
| `sequence_num` | INTEGER | Order within the session (avoids relying purely on timestamp) |
| `role` | TEXT | `user` / `assistant` / `system` |
| `content` | TEXT | Raw message text |
| `provider` | TEXT, nullable | Which provider produced this message (null for user messages) |
| `model` | TEXT, nullable | Which model produced this message |
| `token_count` | INTEGER, nullable | Estimated or actual tokens for this message |
| `created_at` | DATETIME | |

*In plain English:* this is the full, unmodified conversation history — the thing the transcript view renders directly, with the provider/model tag driving the "switched to X" dividers.

### `notes`
The structured, distilled facts extracted from the conversation — the core of the handoff mechanism and the future retrieval upgrade path.

| Field | Type | Notes |
|---|---|---|
| `id` | TEXT (UUID), PK | |
| `session_id` | TEXT, FK → `sessions.id` | |
| `content` | TEXT | The distilled fact/decision, e.g. "decided: atomic CAS for refill timer" |
| `provider` | TEXT | Which provider's conversation this note was extracted from |
| `model` | TEXT | Which model |
| `tags` | TEXT (JSON array) | e.g. `["decision", "concurrency"]` — used for filtering in the notes pane now, and as retrieval metadata later |
| `links_to` | TEXT, nullable, FK → `notes.id` | Self-referencing — this note relates to/builds on another note |
| `superseded_by` | TEXT, nullable, FK → `notes.id` | Self-referencing — set when a later note replaces this one (e.g., a rejected approach) |
| `created_at` | DATETIME | |

*In plain English:* this is your "Obsidian vault" for the session — small, linked, taggable facts instead of a wall of text. At handoff time, the distillation approach sends all non-superseded notes for the session; a future retrieval approach would instead embed and search this same table.

### `switch_events`
A log of every provider/model switch, capturing the cost tradeoff shown to the user — doubles as your demo evidence and product analytics later.

| Field | Type | Notes |
|---|---|---|
| `id` | TEXT (UUID), PK | |
| `session_id` | TEXT, FK → `sessions.id` | |
| `from_provider` / `from_model` | TEXT | |
| `to_provider` / `to_model` | TEXT | |
| `notes_sent_count` | INTEGER | How many notes were included in the handoff |
| `raw_turns_sent_count` | INTEGER | How many recent raw messages were included verbatim |
| `estimated_tokens_full_replay` | INTEGER | What sending the entire raw transcript would have cost |
| `estimated_tokens_distilled` | INTEGER | What the actual distilled handoff cost |
| `created_at` | DATETIME | |

*In plain English:* every time the user switches models, this table records "here's what we sent and here's how much cheaper it was than the naive approach" — this is literally the data behind the cost-comparison screen in the PRD, and a good source of real numbers for your capstone presentation.

### `providers`
Non-secret provider configuration (metadata only — keys never live here).

| Field | Type | Notes |
|---|---|---|
| `id` | TEXT, PK | e.g. `anthropic`, `openrouter`, `ollama` |
| `display_name` | TEXT | |
| `base_url` | TEXT | |
| `default_model` | TEXT | |
| `enabled` | BOOLEAN | |
| `created_at` / `updated_at` | DATETIME | |

*In plain English:* this is what populates the provider pane. It's safe to store in the plain SQLite file because it contains no secrets — the API key for each provider lives in the separate encrypted store, referenced by this same `id`.

### Relationships summary
- `sessions` → `messages`: one-to-many
- `sessions` → `notes`: one-to-many
- `sessions` → `switch_events`: one-to-many
- `notes` → `notes` (`links_to`, `superseded_by`): self-referencing, nullable, many-to-one — this is deliberately graph-lite so the future retrieval upgrade can traverse links without a schema change
- `providers` is independent of `sessions` — one set of provider configs shared across all sessions

---

## 6. Environment Variables and Configuration Notes

### File locations (defaults, all overridable)

| Purpose | Default location (Windows) | Override env var |
|---|---|---|
| Non-secret config (TOML) | `%APPDATA%\llmctl\config.toml` | `LLMCTL_CONFIG_DIR` |
| Encrypted secrets file | `%APPDATA%\llmctl\secrets.age` | `LLMCTL_CONFIG_DIR` (same base) |
| SQLite database | `%APPDATA%\llmctl\llmctl.db` | `LLMCTL_DB_PATH` |

On WSL/Linux/macOS (relevant once the later multi-platform work happens), these fall back to `~/.config/llmctl/` by XDG convention — worth deciding this now even though it's not used until then, so the config-loading code doesn't need rework later.

### Environment variables

| Variable | Purpose |
|---|---|
| `LLMCTL_CONFIG_DIR` | Overrides where config/secrets files live |
| `LLMCTL_DB_PATH` | Overrides SQLite database file location |
| `LLMCTL_LOG_LEVEL` | `debug` / `info` / `warn` / `error` — controls `slog` verbosity |
| `LLMCTL_NO_KEYRING` | Forces passphrase-based encryption instead of OS keyring — needed as a fallback since WSL environments often don't have a keyring service running |
| `OLLAMA_HOST` | Respected if already set, per Ollama's own convention, so `llmctl` doesn't fight with an existing Ollama setup |

### Provider API keys — important note before you start building

Do **not** design these as plain environment variables read from a `.env` file. The whole point of the encrypted config store is that keys are encrypted at rest and only decrypted in memory when `llmctl` needs them. The confusion to avoid: `llmctl` *exports* decrypted keys as env vars *into the user's shell session* when a provider is activated (so tools like Claude Code can read `ANTHROPIC_API_KEY` normally) — but it never *reads* keys from a `.env` file itself. Keys go in via the `llmctl add <provider>` flow and are written straight to the encrypted store.

### `.gitignore` — set this up before anyone writes a line of provider code

Must exclude, from day one:
```
*.db
*.age
config.toml
.env
.env.local
```
A leaked `config.toml` alone isn't catastrophic (no secrets), but get the team in the habit immediately — a misconfigured local `secrets.age` or a debug `.env` someone adds for quick testing is the actual risk.

### One thing to decide before Week 1 (ties to PRD open question #2)

Whether the OS keyring holds the actual API keys directly, or just the passphrase that unlocks the `age`-encrypted secrets file. Recommendation: keyring holds the passphrase, `age` file holds the keys — this way the encrypted file remains portable (can be backed up/moved) even though the keyring entry is machine-specific.

---

## 7. Suggested Module Ownership (5-person team)

| Person | Owns | Depends on |
|---|---|---|
| 1 (Windows/WSL access required) | `shell/`, `doctor/` | `provider/` interface (for auth checks) |
| 2 | `provider/` (adapter interface + 3 implementations) | Nothing — build first |
| 3 | `ui/` (all panes, styles) | `session/` types for what to render |
| 4 | `notes/`, `session/handoff.go`, `costestimate/` | `storage/` repos, `provider/` interface |
| 5 | `storage/`, `config/`, `app/` (integration), release packaging | Everyone — this role naturally becomes the integrator |

Build order that minimizes blocking: `provider/` interface and `session/message.go` types first (unblocks everyone), then modules 1-4 in parallel, with person 5 integrating continuously rather than at the end.
