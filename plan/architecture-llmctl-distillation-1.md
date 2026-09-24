---
goal: Implement llmctl and produce a publication-grade empirical evaluation of distillation-based context handoff, measuring the cost–fidelity frontier against replay, truncation, and summarisation baselines
version: 1.0
date_created: 2026-09-24
last_updated: 2026-09-24
owner: Team No. 260 — Ayush Kushwah (P5/lead), Dolmaa Sharma (P3), Dhairya Dani, Sanket Subhralok Mohapatra, Nivedya B; Supervisor Dr. Vandana Shakya
status: 'Planned'
tags: [architecture, feature, research, evaluation, capstone-phase-2]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This plan takes llmctl from a documentation-only state to a running system and, critically, to a defensible empirical result. The Phase-I report already asserts token savings of 65.6%–90.4%, a 14.2 ms input latency and a 14.2 MB binary, but the same documents describe those runs as *simulated*, and no implementation exists in the repository to have produced them. Those numbers cannot survive peer review, and they are a liability at viva.

The plan therefore treats the research contribution as the primary deliverable and the tool as its instrument. It rests on three corrections that separate a capstone report from a publishable paper:

1. **Cost must be accounted end-to-end.** Distillation is not free: continuous extraction spends tokens on every turn, whether or not a switch ever happens. Comparing a distilled handoff payload against a full replay payload while ignoring cumulative extraction cost is the methodological error a reviewer will find first. This plan measures `C_total = C_handoff + C_extraction` and reports the **switch-count crossover** at which distillation actually becomes cheaper than replay. That crossover — not the headline percentage — is the honest finding.
2. **Compression without fidelity is not a result.** Any lossy scheme reduces tokens; the question is what survives. This plan introduces planted-context probes and a contradiction metric so that cost reduction is always reported against measured context retention, producing a cost–fidelity frontier rather than a single number.
3. **The two unresolved PRD questions become the ablation study.** PRD open question #1 (continuous vs. switch-triggered extraction) and #5 (how many raw turns to send verbatim) are currently guesses. Run as experimental conditions, they turn a scoping gap into the paper's experimental contribution, and `superseded_by` — today a "hard requirement" with no evidence behind it — gets an empirical justification via the contradiction metric.

Phases 1–4 build the system, Phases 5–6 produce the measurements, Phases 7–8 produce the artifact and the release.

## 1. Requirements & Constraints

**Research integrity requirements**

- **REQ-001**: Every quantitative claim in the report, slides, and paper must originate from a logged execution of the compiled binary against a live provider endpoint. No simulated, estimated, or hand-computed figure may appear without being explicitly labelled as a model rather than a measurement.
- **REQ-002**: Token cost must be reported as `C_total = C_handoff + C_extraction`, where `C_extraction` is the cumulative input+output tokens spent by the extraction model since session start. Handoff-only figures may be shown as a component but never as the headline saving.
- **REQ-003**: Every cost measurement must be paired with a fidelity measurement on the same run. Cost figures reported alone are inadmissible.
- **REQ-004**: The evaluation must include at least five comparison arms: cold start (no context), full-transcript replay, rolling-window truncation, single-paragraph summarisation, and structured distillation. A human-curated notes arm serves as the fidelity ceiling.
- **REQ-005**: Every reported condition must be run n ≥ 5 times with independent seeds; results must be reported as mean ± 95% confidence interval. Single-run numbers are not reportable.
- **REQ-006**: All model identifiers must be pinned to exact versioned strings with the access date recorded (e.g. `claude-opus-5-5`, `claude-haiku-4-5-20251001`, `llama3.1:8b`). Family names such as "Claude 3.5 Sonnet" are not acceptable identifiers.
- **REQ-007**: Every figure and table in the paper must be regenerable from the released artifact by a single documented command.
- **REQ-008**: Raw per-run JSONL logs must be retained and published, not only aggregates.

**Functional requirements (inherited, renumbered to preserve SRS traceability)**

- **REQ-010**: Provider profile management for Anthropic, OpenRouter, and Ollama — add, edit, remove, validate connectivity, encrypt keys at rest, display live status. Traces to SRS FR-1.1–FR-1.5.
- **REQ-011**: One-action provider/model switching with shell-correct environment propagation and pre-flight model-ID validation. Traces to SRS FR-2.1–FR-2.4.
- **REQ-012**: Continuous structured note extraction with provider/model attribution on every note and message, handoff assembly from non-superseded notes plus last N raw turns, and a pre-switch cost comparison. Traces to SRS FR-3.1–FR-3.6.
- **REQ-013**: Continuous diagnostics for WSL, provider reachability, provider auth, and optional local services, plus a non-interactive `llmctl doctor` subcommand. Traces to SRS FR-4.1–FR-4.5.
- **REQ-014**: Scrollable attributed transcript, browsable notes view, and Markdown/JSON session export. Traces to SRS FR-5.1–FR-5.3.

**Security requirements**

- **SEC-001**: No API key may be committed, logged at any level, or written to an evaluation artifact. Log redaction must be enforced by a unit test that fails the build on a plaintext key pattern.
- **SEC-002**: The benchmark corpus must contain only synthetic code and synthetic decisions. No real credentials, no proprietary source, no personal data.
- **SEC-003**: Decrypted keys exist in process memory only for the duration of a call; `age` encrypts at rest and the OS keyring holds the passphrase, not the keys. Traces to SRS NFR-3, NFR-4.
- **SEC-004**: Published artifacts must be scanned for secrets before release; a failing scan blocks publication.

**Constraints**

- **CON-001**: Embeddings, vector search, similarity ranking, and link-graph traversal remain out of scope per PRD Section 8. They may be discussed as future work but must not be implemented or benchmarked as an llmctl arm.
- **CON-002**: Windows 10/11 (19041+) with WSL2 is the v1 target. The tree must nonetheless compile on macOS and Linux at every merge to `main`.
- **CON-003**: Single static Go binary, zero CGO, embedded migrations. This forbids `mattn/go-sqlite3` and any C-linked dependency.
- **CON-004**: Five-person team, four on Windows and one on macOS. The macOS member may not own `internal/shell/` or `internal/doctor/`, per the build plan.
- **CON-005**: The full experimental campaign must fit a declared API budget. Cost per arm must be estimated before the campaign is launched and tracked against actuals; the campaign design must degrade gracefully (fewer seeds, shorter sessions) rather than overrun.
- **CON-006**: Provider outputs are non-deterministic and provider-side models may be updated mid-campaign. All arms of a comparison must be collected within a single campaign window, and that window must be recorded.
- **CON-007**: A process cannot mutate its parent shell's environment. Any design that claims to "inject variables into the active terminal" is invalid and must be replaced (see TASK-004).

**Guidelines and patterns**

- **GUD-001**: `llmctl_API_INTERFACE_CONTRACT.md` is the authority on every cross-module signature. Code and document change together in one PR, per the interface-change protocol.
- **GUD-002**: Conventional Commits with package scopes; squash-merge to `main`; PRs reference the REQ or SRS FR they satisfy.
- **GUD-003**: SQLite writes commit incrementally per turn, never batched until exit. Traces to SRS NFR-6.
- **GUD-004**: Diagnostics checks carry a hard 2.0 s timeout and degrade to a warning rather than blocking the UI. Traces to SRS NFR-2.
- **PAT-001**: New providers are added solely by implementing `provider.Adapter`. No file outside `internal/provider/` may branch on provider identity.
- **PAT-002**: The TUI follows Bubble Tea's Model-Update-View discipline; no rendering logic outside `internal/ui/`, no I/O inside `View()`.
- **PAT-003**: One distilled fact per `notes` row. Storing a distilled summary as a single paragraph is prohibited — it is both a schema violation and, deliberately, a measured baseline arm.
- **PAT-004**: OS-specific branching is confined to `internal/shell/` and `internal/doctor/`. A `go vet`-style CI check greps for `runtime.GOOS` outside those packages and fails the build.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Establish the repository, close the four contract defects that would otherwise stall parallel work, and make every subsequent phase mergeable. Completion criterion: `go build ./...` and `go test ./...` pass on both Windows and macOS in CI on a green `main`, and the interface contract contains no undefined type.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Initialise the git repository and Go module scaffold matching the tree in `llmctl_TECHNICAL_ARCHITECTURE.md` §4. Commit `.gitignore` excluding `*.db`, `*.age`, `config.toml`, `.env`, `.env.local` as the first commit, before any module code exists. | | |
| TASK-002 | Resolve the `Note` package ambiguity. The contract's §2.2 prose places `Note` in `internal/notes/` while every signature in §6–§8 writes `session.Note`. Declare `Note` canonical in `package session`, create `internal/session/note.go` and `internal/session/switchevent.go`, and update contract §2.2 in the same PR. | | |
| TASK-003 | Define the missing `session.Session` struct. It is referenced by `SessionsRepo.Get`, `SessionsRepo.List`, and `HandoffBuilder.BuildHandoff` but never specified in contract §2. Add it to `internal/session/session.go` with fields matching the `sessions` table, and add it to contract §2 as §2.4. | | |
| TASK-004 | Replace the unimplementable `shell.Exporter.Export` contract. A child process cannot write into its parent shell's environment (CON-007). Specify and implement the eval-snippet pattern: `llmctl env --shell=<kind>` emits shell-native assignments to stdout for `eval "$(llmctl env)"` / `Invoke-Expression`, plus a `Launch(cmd, vars)` path where llmctl spawns the child with the environment already populated. Amend contract §4 and correct the Phase-I report's §4.2.2 claim. | | |
| TASK-005 | Define the missing `costestimate` interface. `ui/switchconfirm` is listed as a consumer in the contract but no owner-side interface exists. Specify `costestimate.Estimator` with per-provider, per-model token and price lookup, and a pinned pricing table with an `as_of` date field. | | |
| TASK-006 | Stand up CI: build, `go vet`, `go test ./...`, and a cross-compile matrix for `windows/amd64`, `darwin/arm64`, `linux/amd64`. Add the PAT-004 guard that fails on `runtime.GOOS` outside `internal/shell/` and `internal/doctor/`. | | |
| TASK-007 | Write the SQLite migration `0001_init.sql` implementing the five-table schema from the architecture document, and `0002_switch_events.sql`. Add a test that opens a temp DB, migrates, and asserts every table and foreign key exists. Embed migrations via `embed.FS`. | | |
| TASK-008 | Publish mock implementations of `provider.Adapter`, all four storage repos, `notes.Extractor`, and `shell.Detector`/`Exporter` so P1–P4 can develop against interfaces before real implementations land. | | |

### Implementation Phase 2

- GOAL-002: Deliver a working vertical slice — one real message sent to one real provider, persisted, and rendered — proving the module boundaries hold before parallel breadth begins. Completion criterion: a user types a prompt in the TUI, receives an attributed reply from Anthropic, and the message survives a process kill and restart.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Implement `config.Store` (TOML, non-secret) and `config.SecretStore` (`age`-encrypted file with the passphrase in the OS keyring via `go-keyring`), including the `LLMCTL_NO_KEYRING` passphrase fallback required for WSL environments without a keyring service. | | |
| TASK-010 | Implement `SessionsRepo` and `MessagesRepo` against `modernc.org/sqlite`, committing per turn per GUD-003. | | |
| TASK-011 | Implement the Anthropic adapter: `Name`, `Validate`, `ListModels`, `GetEnvVars`, `SendMessage`, `EstimateTokens`. Return the typed `ErrContextTooLarge` rather than silently truncating history, per contract §3. | | |
| TASK-012 | Implement the OpenRouter adapter against the same interface, reusing the OpenAI-compatible request shape. | | |
| TASK-013 | Implement the Ollama adapter, honouring a pre-set `OLLAMA_HOST` and treating absence of auth as a valid configuration rather than a failure state. | | |
| TASK-014 | Build the static TUI shell: provider pane, transcript viewport, notes pane, status bar, with hardcoded data. Owned by P3 (macOS member) per the build plan's platform split. | | |
| TASK-015 | Wire `cmd/llmctl/main.go` — Cobra root, TUI launch, `doctor` and `env` subcommands — and replace the TUI's hardcoded data with real repository reads. | | |
| TASK-016 | Record the real binary size and startup time on each target platform, replacing the unsubstantiated 14.2 MB figure. Note that the report currently gives the identical value 14.2 for both binary megabytes and input-latency milliseconds, which reads as a copy artifact and must not survive into the paper. | | |

### Implementation Phase 3

- GOAL-003: Implement the distillation engine, handoff assembly, and — the part the current design omits entirely — a token ledger that attributes every token spent to extraction or handoff. Completion criterion: a switch produces a `HandoffPlan`, writes a `switch_events` row containing both handoff and cumulative extraction token counts, and the TUI renders the comparison before the user confirms.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-017 | Implement `notes.Extractor` with a versioned extraction prompt in `internal/notes/prompt.go`. The prompt template must be version-stamped and the version recorded on each note, so results remain interpretable after prompt revisions. | | |
| TASK-018 | Implement structured extraction output: one atomic fact per `Note`, with `Tags` populated at creation time per PRD Section 8, and `Provider`/`Model` attribution on every note. | | |
| TASK-019 | Implement `LinksTo` and `SupersededBy` population. When extraction detects that a new decision invalidates an earlier one, call `NotesRepo.MarkSuperseded`. This is the field whose value Phase 6 measures; leaving it nil makes ABL-003 unrunnable. | | |
| TASK-020 | Implement the **token ledger**: a per-session running total of input and output tokens spent by the extraction model, persisted incrementally. This is the quantity REQ-002 requires and which the existing Table 5 omits entirely. | | |
| TASK-021 | Implement `session.HandoffBuilder.BuildHandoff` — non-superseded notes plus the last N raw turns, with N a configuration parameter rather than a constant, since Phase 6 sweeps it. | | |
| TASK-022 | Implement both extraction triggers behind a strategy interface: continuous background extraction and switch-triggered batch extraction. PRD open question #1 is resolved by experiment in Phase 6, not by assumption, so both must exist. | | |
| TASK-023 | Implement `costestimate.Estimator` per TASK-005, and the `switchconfirm` modal showing full-replay cost, handoff cost, cumulative extraction cost, and net position. The modal must not present estimates as exact financial figures; the report's §3.1 claim of "exact token count and estimated financial cost" overstates what `EstimateTokens` provides. | | |
| TASK-024 | Implement `SwitchEventsRepo` writes capturing `notes_sent_count`, `raw_turns_sent_count`, `estimated_tokens_full_replay`, `estimated_tokens_distilled`, and the new cumulative extraction total. | | |
| TASK-025 | Implement the transcript provider/model divider rendering and per-message attribution in the TUI, satisfying REQ-012. | | |

### Implementation Phase 4

- GOAL-004: Deliver the diagnostics subsystem and the non-interactive `doctor` command, and settle the Redis scope question. Completion criterion: `llmctl doctor` correctly classifies at least four induced failure states, and no check can block the UI beyond 2.0 s.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-026 | Implement the `doctor.Runner` dispatching each `Check` as a goroutine under a 2.0 s `context.WithTimeout`, collecting results without blocking the Bubble Tea update loop. | | |
| TASK-027 | Implement `wsl_check` (instance state, interop socket) and `network_check` (provider endpoint reachability). Windows/WSL-owning member only, per CON-004. | | |
| TASK-028 | Implement `auth_check` as a thin wrapper over each adapter's `Validate()`, and make Ollama's variant a reachability check rather than an auth check. | | |
| TASK-029 | Resolve the Redis scope question. Redis appears as a core check across the PRD, SRS FR-4.4, and the Phase-I report, but no document states why an LLM terminal tool depends on it; the build plan itself asks whether it should be optional. Either document a concrete dependency or demote it to an optional user-configured service check, and record the decision. | | |
| TASK-030 | Implement the persistent status bar consuming `CheckResult` values, with OK/WARN/FAIL rendering and a visible last-checked timestamp. | | |
| TASK-031 | Build a fault-injection harness that induces each failure state reproducibly (stopped WSL instance, revoked key, unreachable Ollama port, severed network) so the SRS success metric is demonstrated rather than asserted. | | |

### Implementation Phase 5

- GOAL-005: Construct the evaluation instrument — the benchmark corpus, the probe methodology, and the automated runner. This phase produces no user-facing feature and is the single most important phase for the paper. Completion criterion: the runner executes all six arms unattended over the full corpus and emits per-run JSONL that regenerates every planned figure.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-032 | Design the benchmark corpus: scripted multi-turn software-development sessions at lengths 5, 15, 30, and 50 turns, across at least three task genres (debugging, refactoring, schema design). Each session embeds K planted context items — architectural decisions, negative constraints ("do not use X"), identifier and file names, and at least one decision that is later superseded. All content synthetic per SEC-002. | | |
| TASK-033 | Design the probe set. For each session, author P post-switch probe prompts whose correct answers are derivable only from planted items, with a machine-checkable ground-truth answer key. Include contradiction probes that specifically invite the model to revert to a superseded decision. | | |
| TASK-034 | Implement the grader. Primary scoring is exact/keyword match against the answer key; ambiguous responses fall to an LLM judge with the ground truth in context. Validate the judge against a human-labelled subset of ≥100 probe responses and report inter-rater agreement (Cohen's κ); a judge below the agreement threshold is not usable. | | |
| TASK-035 | Implement the six comparison arms behind one interface: ARM-0 cold start (no context), ARM-1 full-transcript replay, ARM-2 rolling-window truncation to a fixed budget, ARM-3 single-paragraph LLM summary, ARM-4 llmctl structured distillation, ARM-5 human-curated notes (fidelity ceiling). ARM-3 is the honest cheap alternative the PRD forbids in the product and must be beaten, not ignored. | | |
| TASK-036 | Implement `cmd/llmctl-bench`: takes a corpus file, an arm, a seed, and a provider pair; drives a real session through the real binary path; and emits one JSONL record per run with handoff tokens, cumulative extraction tokens, wall-clock latency, per-probe correctness, contradiction flags, model IDs, prompt version, and timestamp. | | |
| TASK-037 | Implement deterministic seeding and provenance capture: fixed sampling parameters where the provider exposes them, recorded git SHA, recorded extraction-prompt version, recorded model IDs and campaign window per CON-006. | | |
| TASK-038 | Implement the latency instrument: measure keyboard-to-render latency under concurrent extraction and diagnostics load, reporting the p50/p95/p99 distribution rather than a single figure. NFR-1's 100 ms budget is a tail property, so a lone mean is not evidence of compliance. | | |
| TASK-039 | Pre-register the analysis plan: state hypotheses, arms, metrics, seed count, and the crossover analysis before collecting data, and commit it. Pre-registration is cheap here and materially strengthens the paper's claims. | | |

### Implementation Phase 6

- GOAL-006: Execute the experimental campaign and produce the paper's results. Completion criterion: every figure and table is generated from committed JSONL by a single command, with confidence intervals, and the crossover analysis is complete.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-040 | Run the main campaign: 6 arms × 4 session lengths × 3 genres × 5 seeds, with provider pairs covering frontier→local (`claude-opus-5-5` → `llama3.1:8b`), frontier→frontier, and frontier→gateway. Estimate cost against the CON-005 budget before launch and track actuals per run. | | |
| TASK-041 | Produce the **cost–fidelity frontier**: retention rate on the y-axis against `C_total` on the x-axis, one point per arm per session length, with 95% CIs. This plot is the paper's central figure and replaces the existing single-column savings table. | | |
| TASK-042 | Produce the **crossover analysis**, the plan's most important honest result: given that continuous extraction bills every turn while replay bills only at a switch, compute the number of switches per session at which distillation's `C_total` falls below replay's. Report the crossover point per session length. If distillation loses for single-switch sessions, that must be stated plainly — it is a finding about when the technique applies, and a paper that reports it is stronger than one that hides it. | | |
| TASK-043 | Run ABL-001, resolving PRD open question #5: sweep N ∈ {0, 2, 3, 5, 10} raw turns and report the retention/cost curve, replacing the current unjustified "two or three" with a measured recommendation. | | |
| TASK-044 | Run ABL-002, resolving PRD open question #1: continuous versus switch-triggered extraction, reporting perceived latency, total extraction cost, and retention for each. | | |
| TASK-045 | Run ABL-003, the `superseded_by` ablation: handoff with and without filtering superseded notes, scored on contradiction rate. This supplies the first evidence for a field three documents currently mandate on faith. | | |
| TASK-046 | Run ABL-004: extraction-model size, comparing a small fast extractor (`claude-haiku-4-5-20251001`) against the active frontier model as extractor, on retention and total cost. | | |
| TASK-047 | Run the statistical analysis: mean ± 95% CI for every reported quantity, paired significance tests between ARM-4 and each baseline on matched seeds, and effect sizes. Report negative and null results as readily as positive ones. | | |
| TASK-048 | Run the failure-mode analysis: manually inspect a stratified sample of low-retention runs and classify what distillation dropped. A qualitative taxonomy of what the technique loses is what separates an engineering report from a paper. | | |
| TASK-049 | Regenerate the Phase-I numbers honestly and write the erratum. The 65.6/80.7/86.3/90.4% table, the 14.2 ms latency, and the 14.2 MB footprint must each be replaced by a measured value or withdrawn, and the change flagged to the supervisor rather than silently corrected. | | |

### Implementation Phase 7

- GOAL-007: Produce the paper and the reproducibility artifact. Completion criterion: an independent reader can clone the artifact, run one command, and regenerate every figure.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-050 | Draft the paper: motivation, related work positioned against MemGPT and LLMLingua rather than only against developer tools, the distillation method, the evaluation design, results including the crossover, threats to validity, and future work naming the deferred retrieval upgrade. | | |
| TASK-051 | Write the threats-to-validity section explicitly: synthetic corpus, LLM-judge reliability, provider non-determinism and mid-campaign drift, single-host latency measurement, and the fact that planted-probe retention is a proxy for real task continuity, not task success itself. | | |
| TASK-052 | Package the artifact: corpus, probe answer keys, runner, raw JSONL, analysis notebooks, pinned dependency versions, and a `make figures` target satisfying REQ-007. | | |
| TASK-053 | Run the secret scan over every artifact file and the full git history before publication, per SEC-004. | | |
| TASK-054 | Update the Phase-I document set for consistency: reconcile the SRS's NFR-1–NFR-11 with the report's renumbered NFR-1–NFR-8 (which drops SRS NFR-8, NFR-9, and NFR-10 — the last being the retrieval-readiness requirement that carries the extensibility argument); fill the `[Team Member 2]`–`[Team Member 5]` placeholders in the proposal; fix "Bachlore of Engineering and Technology", "development environmPriorityent", and the §4.3 citation of Table 1 for data that lives in Table 5. | | |
| TASK-055 | Replace stale model references throughout. The report's canonical example `[Anthropic: claude-3-5-sonnet]` and reference [8] to the Claude 3 family are two generations old for a September 2026 document; current families are Claude 5 (Opus 5.5, Sonnet 5, Fable 5.1) and Haiku 4.5. For a tool whose premise is catching outdated model identifiers, this detail will be noticed. | | |
| TASK-056 | Produce a text-layer PDF of the PRD. The current `llmctl PRD.pdf` has all text converted to vector outlines, so it cannot be searched, diffed, quoted, or cited by the team, despite being the document every other file treats as authoritative. | | |

### Implementation Phase 8

- GOAL-008: Ship the public release. Completion criterion: a tagged `v0.1.0` with cross-platform binaries and documentation sufficient for an unaffiliated Windows/WSL user to install and complete a provider switch.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-057 | Configure GoReleaser for Windows, macOS, and Linux across amd64 and arm64, with checksums and embedded version metadata. | | |
| TASK-058 | Write the README, the WSL setup guide, and the "how to add a provider adapter" extensibility document that is the public-release story for PAT-001. | | |
| TASK-059 | Record the asciinema demo covering a session, a switch with the cost modal, and a diagnostics failure being caught. | | |
| TASK-060 | Implement session export to Markdown and JSON conforming to contract §10, with a golden-file test pinning the schema since users may script against it. | | |
| TASK-061 | Run the error-path hardening pass: corrupted database, missing config, wrong passphrase, rate-limited provider, auth expiry mid-session, malformed provider response. Traces to SRS NFR-5. | | |
| TASK-062 | Tag `v0.1.0`, generate `CHANGELOG.md` from squashed history, and publish the release together with the research artifact. | | |

## 3. Alternatives

- **ALT-001**: Report handoff-payload tokens only, as the Phase-I documents currently do. Rejected: it ignores amortised extraction cost and therefore overstates the saving. It is also the first thing a reviewer will check, so the current framing converts a genuine contribution into an apparent overclaim.
- **ALT-002**: Evaluate on token reduction alone, without fidelity probes. Rejected: any lossy compression reduces tokens, so the result would be unfalsifiable and uninteresting. Cost is only meaningful against retained fidelity.
- **ALT-003**: Use real developer sessions harvested from team members instead of a synthetic corpus. Rejected for v1: no ground truth for probes, no reproducibility, and a credential-leakage risk under SEC-002. Recommended as future work with consent and redaction.
- **ALT-004**: Implement vector retrieval and benchmark it as a sixth llmctl arm. Rejected: CON-001 places it out of scope, and adding it would dilute the contribution. It remains the paper's future-work section, with the existing schema as evidence the migration is additive.
- **ALT-005**: Human evaluation of continuity instead of planted probes. Rejected as primary method — cost and rater availability at five seeds per condition are prohibitive — but retained as the TASK-034 validation subset.
- **ALT-006**: Persist environment variables to the Windows registry or shell profile so they survive as "real" exports. Rejected: it mutates user state outside the session, leaks keys to disk in plaintext, and violates SEC-003. The eval-snippet pattern in TASK-004 is the correct mechanism.
- **ALT-007**: Store the distilled context as one summary paragraph, which is simpler and cheaper to implement. Rejected as a product design by PAT-003, but deliberately retained as evaluation ARM-3, because it is the strongest cheap baseline and beating it is what justifies the structured-row discipline.

## 4. Dependencies

- **DEP-001**: Go 1.22+ toolchain on all five developer machines.
- **DEP-002**: `github.com/charmbracelet/bubbletea`, `lipgloss`, `bubbles` for the TUI layer.
- **DEP-003**: `modernc.org/sqlite` — the pure-Go driver, mandatory under CON-003.
- **DEP-004**: `filippo.io/age` for at-rest encryption and `github.com/zalando/go-keyring` for passphrase custody.
- **DEP-005**: `github.com/spf13/cobra` for subcommand dispatch and `github.com/BurntSushi/toml` for non-secret config.
- **DEP-006**: `github.com/hashicorp/go-retryablehttp` over standard `net/http` for provider calls.
- **DEP-007**: Funded API access to Anthropic and OpenRouter sufficient for the Phase 6 campaign, sized under CON-005 before launch.
- **DEP-008**: A working WSL2 host owned by the shell/doctor member — the one environment no other member can substitute for.
- **DEP-009**: A local Ollama instance with a pinned model tag for the local-provider arm.
- **DEP-010**: Python with pandas/matplotlib, or Go equivalents, for the analysis and figure pipeline.
- **DEP-011**: Human annotator time for the TASK-034 judge-validation subset.
- **DEP-012**: Supervisor sign-off on the TASK-049 erratum before the corrected numbers are circulated.

## 5. Files

- **FILE-001**: `cmd/llmctl/main.go` — Cobra root, TUI launch, `doctor`, `env`, and `export` subcommands.
- **FILE-002**: `cmd/llmctl-bench/main.go` — the evaluation runner; new, and not present in the architecture document's tree.
- **FILE-003**: `internal/session/{message,session,note,switchevent,handoff}.go` — canonical types and handoff assembly; `note.go`, `session.go`, and `switchevent.go` are additions closing the TASK-002/TASK-003 gaps.
- **FILE-004**: `internal/notes/{extractor,prompt,ledger}.go` — extraction, the versioned prompt template, and the REQ-002 token ledger.
- **FILE-005**: `internal/provider/{adapter,registry}.go` plus `anthropic/`, `openrouter/`, `ollama/`.
- **FILE-006**: `internal/shell/{detect,powershell,wsl,snippet}.go` — `snippet.go` implements the TASK-004 eval-snippet emitter.
- **FILE-007**: `internal/doctor/{checks,wsl_check,redis_check,network_check,auth_check}.go`.
- **FILE-008**: `internal/storage/{db,sessions_repo,messages_repo,notes_repo,switch_events_repo}.go` and `migrations/*.sql`.
- **FILE-009**: `internal/config/{config,secrets,keyring}.go`.
- **FILE-010**: `internal/costestimate/{estimate,pricing}.go` — including the dated pricing table.
- **FILE-011**: `internal/ui/{providerpane,transcript,notespane,statusbar,switchconfirm,styles}/`.
- **FILE-012**: `eval/corpus/*.json` — scripted sessions with planted items and probe answer keys.
- **FILE-013**: `eval/arms/*.go` — the six comparison-arm implementations.
- **FILE-014**: `eval/results/*.jsonl` — raw per-run records, committed per REQ-008.
- **FILE-015**: `eval/analysis/` — figure and table generation behind a `make figures` target.
- **FILE-016**: `eval/PREREGISTRATION.md` — the TASK-039 pre-registered analysis plan.
- **FILE-017**: `docs/` — the corrected PRD, SRS, architecture, contract, and the TASK-049 erratum.
- **FILE-018**: `.github/workflows/ci.yml`, `.goreleaser.yaml`, `Makefile`, `.gitignore`.

## 6. Testing

- **TEST-001**: Storage repository round-trip tests against a temporary SQLite file, including foreign-key enforcement and incremental-commit behaviour under simulated abrupt termination (GUD-003, SRS NFR-6).
- **TEST-002**: Provider adapter conformance suite run identically against all three adapters plus the mock, asserting that no method panics and that `ErrContextTooLarge` is returned rather than history being truncated.
- **TEST-003**: Secret-redaction test asserting that no log record at any level, including debug, matches a key-shaped pattern. Fails the build on violation (SEC-001).
- **TEST-004**: Shell snippet tests asserting correct PowerShell and Bash syntax, including values containing spaces, quotes, and backslashes — the historical source of the path-with-spaces bug class.
- **TEST-005**: Doctor timeout test asserting a check that sleeps 5 s returns WARN within 2.0 s and never blocks the runner (GUD-004).
- **TEST-006**: Handoff assembly unit tests asserting that superseded notes are excluded, that exactly N raw turns are attached, and that the token ledger is correctly summed.
- **TEST-007**: Cost-comparison correctness test asserting the reported full-replay estimate equals the sum of per-message token estimates over the transcript.
- **TEST-008**: Export golden-file test pinning the contract §10 JSON schema, since users may script against it.
- **TEST-009**: Cross-platform build test in CI for all three OS targets on every PR (CON-002).
- **TEST-010**: PAT-004 isolation test failing the build on `runtime.GOOS` outside `internal/shell/` and `internal/doctor/`.
- **TEST-011**: Grader validation: Cohen's κ between the LLM judge and human labels on the TASK-034 subset must exceed the pre-registered threshold before the judge is used for reported results.
- **TEST-012**: Benchmark reproducibility test: re-running one committed seed reproduces its recorded token counts exactly and its retention score within the pre-registered tolerance.
- **TEST-013**: Fault-injection acceptance test asserting the doctor panel correctly classifies each of the four induced failure states (TASK-031).
- **TEST-014**: Latency regression test asserting p95 keyboard-to-render stays under the NFR-1 100 ms budget with extraction and diagnostics running.

## 7. Risks & Assumptions

- **RISK-001**: The crossover analysis may show distillation is *more* expensive than replay for short sessions or single-switch workflows. Mitigation: treat this as a finding, not a failure. The paper's contribution becomes a characterisation of when the technique pays, which is more useful and more credible than a uniform savings claim. Plan the narrative for this outcome in advance.
- **RISK-002**: Retention may prove statistically indistinguishable between distillation and the single-paragraph summary baseline (ARM-3), which would undercut the structured-row discipline. Mitigation: ABL-003's contradiction metric is where structure should show its advantage, since a flat paragraph cannot represent supersession. If no arm separates, report that honestly.
- **RISK-003**: Provider-side model updates mid-campaign invalidate cross-arm comparison (CON-006). Mitigation: collect all arms for a given comparison within one window, record the window, and re-run affected arms if a version change is detected.
- **RISK-004**: API budget overrun during the six-arm campaign. Mitigation: cost-estimate before launch, meter per run, and degrade seeds or session lengths rather than dropping arms — losing an arm destroys the comparison, losing a seed only widens the interval.
- **RISK-005**: The shell-export redesign (TASK-004) invalidates a claim already made in the submitted Phase-I report. Mitigation: raise it with the supervisor as a corrected design decision early, alongside the TASK-049 erratum, rather than letting it surface at the Phase-II viva.
- **RISK-006**: The single Windows/WSL member becomes a bottleneck for Phases 4 and 6. Mitigation: the build plan already identifies this; front-load TASK-027 and pair a second member on WSL access for redundancy.
- **RISK-007**: Corpus authoring is underestimated. Writing 12 scripted sessions with planted items and validated answer keys is a substantial, unglamorous effort that determines the quality of every downstream number. Mitigation: start Phase 5 corpus work in parallel with Phase 3 rather than after it.
- **RISK-008**: LLM-judge unreliability contaminates the fidelity metric. Mitigation: TEST-011's κ gate, plus exact-match primary scoring so the judge handles only the ambiguous residue.
- **RISK-009**: Scope creep toward implementing retrieval because it is intellectually attractive. Mitigation: CON-001 is explicit, and the schema already carries the extensibility argument without an implementation.
- **ASSUMPTION-001**: The team is prepared to publish a corrected set of numbers that may be less impressive than the figures already presented at Phase-I review. The plan is not viable otherwise.
- **ASSUMPTION-002**: Planted-probe retention is a defensible proxy for real task continuity. This is a stated threat to validity (TASK-051), not a claim of equivalence.
- **ASSUMPTION-003**: Anthropic, OpenRouter, and Ollama remain accessible and API-stable across the campaign window.
- **ASSUMPTION-004**: Supervisor approval for a Phase-II scope weighted toward evaluation rather than feature breadth. Worth confirming before Phase 5 begins, since it changes what the final report is assessed on.
- **ASSUMPTION-005**: Extraction cost is dominated by input tokens over the recent-turn window, making it roughly linear in session length. If extraction is instead run over the full transcript each time, cost becomes quadratic and the crossover moves sharply against distillation — TASK-020 must measure this rather than assume it.

## 8. Related Specifications / Further Reading

- `llmctl PRD.pdf` — Sections 7 (open questions #1 and #5, resolved by ABL-001 and ABL-002) and 8 (retrieval extensibility, and the prohibition on paragraph-summary storage that PAT-003 and ARM-3 both derive from)
- `llmctl_SRS.md` — FR-1.x–FR-5.x and NFR-1–NFR-11, the traceability targets for REQ-010–REQ-014
- `llmctl_TECHNICAL_ARCHITECTURE.md` — §4 file tree, §5 SQLite schema, §6 configuration and keyring decision
- `llmctl_API_INTERFACE_CONTRACT.md` — the authority amended by TASK-002, TASK-003, TASK-004, and TASK-005
- `llmctl_BUILD_PLAN_BY_TEAMMATE.md` — P1–P5 ownership and the macOS platform split governing CON-004
- `llmctl_GIT_WORKFLOW.md` — §6 interface-change protocol, mandatory for the Phase 1 contract amendments
- C. Packer et al., "MemGPT: Towards LLMs as Operating Systems," arXiv:2310.08560 — the retrieval-based comparison point for related work
- H. Jiang et al., "LLMLingua: Compressing Prompts for Accelerated Inference," EMNLP 2023 — prompt compression, the closest methodological neighbour, and the work against which distillation must be positioned
- ACM SIGSOFT and ACM artifact-evaluation badging criteria — the standard FILE-015 and TASK-052 are built to satisfy
