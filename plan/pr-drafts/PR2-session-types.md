# feat(session): define Session and settle where Note lives

## ⚠️ Interface change

This adds a type to `llmctl_API_INTERFACE_CONTRACT.md` §2 and resolves a
contradiction in §2.2. Per `llmctl_GIT_WORKFLOW.md` §6, every consumer owner
must acknowledge before merge.

**Consumers to tag** (from the contract's "Consumed by" lists for §2.2, §7, §8):

- **Person 3** — `ui/notespane`, `ui/transcript`
- **Person 5** — `storage/sessions_repo`, `storage/notes_repo`
- **Person 4** — `session/handoff.go`, and owner of these types

## Problem 1 — `Session` is used but never defined

Four signatures in the contract take or return `session.Session`:

```go
SessionsRepo.Create(ctx, s *session.Session) error
SessionsRepo.Get(ctx, id string) (*session.Session, error)
SessionsRepo.List(ctx) ([]session.Session, error)
HandoffBuilder.BuildHandoff(ctx, sess *Session) (HandoffPlan, error)
```

§2 defines `Message`, `Note` and `SwitchEvent`, but not `Session`. Person 5
cannot implement `SessionsRepo` and Person 4 cannot implement `BuildHandoff`
without inventing it, and if both invent it independently they will not match.

**Resolution** — define it in `internal/session/session.go`, with fields
matching the `sessions` table in the architecture document §5:

```go
type Session struct {
    ID             string // UUID
    Title          string
    ActiveProvider string
    ActiveModel    string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

## Problem 2 — `Note` is in two packages at once

§2.2 says these types "live in `internal/session/` and `internal/notes/`" and
names Person 4's notes module as owner. But every signature that uses it writes
`session.Note` — §6, §8, and the `HandoffPlan` in §7.

This is not cosmetic. If `Note` lived in `package notes`, then `session`
(whose `HandoffPlan` holds `[]Note`) would import `notes`, while `notes`
already imports `session` for `[]session.Message`. Go rejects that as an import
cycle and neither package would compile.

**Resolution** — `Note` is canonical in `package session`. The dependency runs
one way: `notes` imports `session`, never the reverse. Ownership is unchanged —
Person 4 still owns the type and the extraction logic; only its file location
is settled.

## Also added

`Note.Active()` returns whether `SupersededBy == nil`, encoding the handoff rule
in one place so `ui/notespane` filtering and `session/handoff.go` assembly
cannot disagree about what "active" means.

No change to `Message` or `SwitchEvent`.

## Requirements addressed

- SRS **FR-3.2**, **FR-3.3** — provider/model attribution on every note and message
- SRS **NFR-10** — notes schema stays ready for a future retrieval upgrade
- Plan **TASK-002**, **TASK-003**

## Contract edits included in this PR

1. Add **§2.4 `Session`** with the struct above.
   **Owner:** Person 4. **Consumed by:** `storage/sessions_repo`,
   `session/handoff.go`, `ui/providerpane`, `app`.

2. Amend **§2.2**'s opening line from "They live in `internal/session/` and
   `internal/notes/`" to: "All canonical types live in `internal/session/`.
   `internal/notes/` imports them; the reverse would be an import cycle."

3. Add to §2.2's contract note:

   > `Note` is declared in `package session` despite being owned by the notes
   > module. Package location and ownership are separate concerns here — the
   > location is forced by Go's prohibition on import cycles.

## Checklist (`llmctl_GIT_WORKFLOW.md` §4)

- [x] Targets `main`
- [x] References the requirements it addresses
- [x] CI green
- [x] Contract document updated in this PR
- [ ] Acknowledged by Person 3 (`ui`)
- [ ] Acknowledged by Person 5 (`storage`)
- [ ] Acknowledged by Person 4 (owner)
