# llmctl — Step-Wise Build Plan by Teammate

**Status:** Draft v1
**Team:** 5 people — 4 on Windows, 1 on macOS
**Timeline:** 7–8 months, mid-point demo at Month 4
**Companion to:** llmctl_PRD.md, llmctl_TECHNICAL_ARCHITECTURE.md, llmctl_API_INTERFACE_CONTRACT.md, llmctl_GIT_WORKFLOW.md

---

## How platform access shapes the split

Only two packages in the whole system are genuinely OS-specific: `internal/shell` and `internal/doctor`. Both require a real Windows/WSL2 environment to write and test against. Everything else — provider adapters, the TUI, notes/handoff logic, storage, config — is pure Go with no OS dependency, and compiles and runs identically on macOS.

So the rule for this plan is simple: **the macOS teammate never owns a file inside `internal/shell/` or `internal/doctor/`.** Everyone else can still build on Windows without any adjustment — this split isn't about giving the Mac user "lesser" work, it's about not blocking them on code they can't run.

| Person | Machine | Owns |
|---|---|---|
| P1 | Windows (WSL2) | `internal/shell`, `internal/doctor` |
| P2 | Windows or Mac | `internal/provider` (all 3 adapters) |
| P3 | Windows or Mac — **assign to the Mac teammate** | `internal/ui` (all TUI panes) |
| P4 | Windows or Mac | `internal/notes`, `internal/session`, `internal/costestimate` |
| P5 | Windows or Mac | `internal/storage`, `internal/config`, integration |

Recommendation: put the Mac teammate on **P3 (TUI)**. It's the most visually iterative module (fastest feedback loop, no waiting on other modules to see progress), entirely platform-agnostic, and doesn't require them to ever touch a Windows machine to verify their own work. P2, P4, or P5 would also work for them — just not P1.

---

## Phase 0 — Setup (Week 1, all 5 together)

Nobody starts writing feature code until this is done; skipping it is what causes integration pain later.

1. **All 5:** Install Go 1.22+ locally (Windows and macOS installers both official, no cross-platform gotchas).
2. **All 5:** Clone the repo, confirm `go build ./...` succeeds on a bare scaffold on both OSes — this is the first real proof the "no CGO, cross-platform from day one" decision works.
3. **All 5:** Read and agree on `llmctl_API_INTERFACE_CONTRACT.md` — this is the most blocking document for parallel work; any disagreement on a signature needs to surface now, not in Week 5.
4. **P5:** Set up the SQLite migration scaffold (`internal/storage/migrations/0001_init.sql`) from the schema in the Technical Architecture Document, and a `go test` that just confirms the DB opens and migrates cleanly. Push this first — everyone else's mocks depend on the same table shapes existing.
5. **P1:** Confirm WSL2 is installed and working on their own machine specifically (not just Windows) — this is the one environment nobody else on the team can substitute for.
6. **All 5:** Set up `.gitignore` per the Git Workflow Document before any config/secrets code is written.

---

## Phase 1 — Foundations (Months 1–2)

Goal: every module has a real (not mocked) minimal implementation, and the pieces connect.

**P1 (shell/doctor, Windows/WSL):**
- Implement `shell.Detect()` — PowerShell vs. WSL/Bash detection.
- Implement `shell.Export()` for both shells.
- Stub out `doctor.Check` implementations (`wsl_check`, `network_check`) with real logic; `redis_check` and `auth_check` can return `StatusWarn` placeholders until P2's adapters exist.

**P2 (provider adapters, either OS):**
- Implement the `provider.Adapter` interface for Anthropic first (best-documented API), then OpenRouter (OpenAI-compatible, similar shape), then Ollama (local, no auth).
- Each adapter: `Validate()`, `ListModels()`, `GetEnvVars()` first — these unblock P1's `doctor.auth_check` and P5's config work. `SendMessage()` can follow once P4's `Message` type is finalized (should already be, from the interface contract).

**P3 (TUI, Mac-friendly):**
- Build static (non-functional) versions of each pane using the mockup from earlier in the project as the visual reference: provider list, transcript viewport, notes pane, status bar.
- Wire up Bubble Tea's root `Update()`/`View()` loop with hardcoded/fake data first — don't wait for real data sources. This is the fastest-moving module early on precisely because it doesn't need to wait on anyone.
- Once P5's repo interfaces exist (even as stubs), swap fake data for real reads.

**P4 (session/notes, either OS):**
- Finalize the `Message`, `Note`, `SwitchEvent` structs exactly as specified in the interface contract (do this in Week 1 if possible — everyone else imports these types).
- Implement a first-pass `notes.Extractor` — even a simple heuristic or single prompt-based version is enough to unblock `session/handoff.go` development; refine later.

**P5 (storage/config, either OS):**
- Implement all four repo interfaces (`SessionsRepo`, `MessagesRepo`, `NotesRepo`, `SwitchEventsRepo`) against SQLite.
- Implement `config.Store` (non-secret TOML) and `config.SecretStore` (age-encrypted). Resolve the "keyring holds passphrase, age holds keys" decision from the Technical Architecture Document here, concretely, in code.
- Start wiring `cmd/llmctl/main.go` to actually launch the Bubble Tea app with real (not mock) modules as they become available — this person becomes the continuous integrator from here on, not just at the end.

**Milestone check (end of Month 2):** `llmctl doctor` runs and reports at least WSL + network status; the TUI launches and displays a static provider list; at least one provider adapter (Anthropic) can send and receive a real message from the command line, even without the TUI wired to it yet.

---

## Phase 2 — Core Differentiator (Months 3–4) — mid-point demo target

Goal: the distillation-based handoff actually works end-to-end, live, with a real cost comparison. This is what gets demoed.

**P1:** Finish all `doctor` checks (`redis_check`, `auth_check` now that P2's `Validate()` is real); make the status bar checks continuous (background polling), not one-shot.

**P2:** Complete `SendMessage()` for all three adapters; implement `EstimateTokens()` per provider (needed for the cost-comparison screen).

**P3:** Build the `switchconfirm` modal (the cost-comparison confirmation screen) — this is the single most demo-critical UI piece. Wire the transcript view to render provider/model dividers on switch, using P4's attributed `Message` data.

**P4:** This is the core of the semester for this person — implement `session/handoff.go`: assembling the `HandoffPlan` (non-superseded notes + last N raw turns), and `costestimate` for the full-replay-vs-distilled comparison. Resolve the two open PRD questions that block this: extraction trigger timing, and default raw-turn count.
- **Hard requirement, not optional:** populate `links_to` and `superseded_by` on every `Note` where determinable, even though nothing reads them yet — this is the one shortcut that must not be taken (see project decisions log).

**P5:** Implement `SwitchEventsRepo` writes so every switch is logged with its real token estimates — this data is what P3's demo screen and the eventual capstone report's "token reduction" numbers both come from. Keep integrating: by now `main.go` should launch the full app with all real modules.

**Milestone check (end of Month 4 — mid-point demo):** Live demo of: start a session on one provider, have a short conversation, trigger a switch, see the cost-comparison modal with real numbers, confirm, see the conversation continue on the new provider with a visible divider in the transcript.

---

## Phase 3 — Diagnostics and Polish (Months 5–6)

Goal: the system is robust, not just demoable once.

**P1:** Harden diagnostics — handle WSL not installed, Redis absent (should this be optional per system?), network flakiness, without crashing the status bar.

**P2:** Error handling pass across all three adapters — rate limits, auth expiry mid-session, malformed responses. Make sure `doctor.auth_check` surfaces these clearly rather than the TUI just hanging.

**P3:** Session export view (Markdown/JSON), notes pane polish (filtering by tag), general visual polish pass — this is a good phase for the Mac teammate to also help review/port anything that turns out to render slightly differently across terminal emulators, since they're naturally testing on a different terminal stack (iTerm2/Ghostty on Mac vs. Windows Terminal) than the rest of the team.

**P4:** Refine note extraction quality based on real usage from Phase 2's demo sessions; tune the raw-turn-count default with real data instead of a guess.

**P5:** Full error-path testing on storage/config (corrupted DB, missing config file, wrong passphrase); this is also the phase to start the test plan and risk register documents if not already done.

**Milestone check (end of Month 6):** The team can each run a full multi-hour session without a crash; diagnostics correctly catch at least 3 induced failure states (per the SRS success metric).

---

## Phase 4 — Release Preparation (Months 7–8)

Goal: documentation, packaging, and (per the PRD) preparing for public release.

**All 5, split by existing ownership:**
- P1: Windows install instructions, WSL setup guide for the README.
- P2: Document how to add a new provider adapter (this is the extensibility story for the public release).
- P3: Record the demo GIF/asciinema capture for the README — natural fit given TUI ownership.
- P4: Write up the distillation approach for the report/README in plain terms — they know it best.
- P5: GoReleaser setup for cross-platform builds (even though only Windows ships as "supported" in v1, per PRD scope, having the build pipeline ready costs little now); final integration pass.

**All 5:** Final report writing, presentation prep, and incorporating any feedback from the mid-point review.

---

## Cross-cutting notes

- **Any interface change, at any phase**, follows the Git Workflow Document's interface-change protocol (Section 6) — tag every consumer, don't merge until acknowledged. This matters more as phases progress and modules depend on each other more heavily.
- **The Mac teammate should periodically pull `main` and confirm `go build ./...` still succeeds on macOS**, not just Windows — this is the cheapest possible check that "no Windows-only assumptions leaked outside `shell/`/`doctor/`" is actually holding, and it's a check only they can run.
- If the team falls behind, the module to protect first is **P4's handoff logic** — it's the mid-point demo's centerpiece and the project's actual technical contribution; diagnostics polish (P1/P5 in Phase 3) is the safer thing to compress if the timeline gets tight.
