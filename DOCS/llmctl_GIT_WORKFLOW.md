# llmctl — Git Workflow Document

**Status:** Draft v1
**Applies to:** All 5 team members, from the first commit

---

## 1. Repository Structure

Single repository (monorepo), matching the folder structure defined in the Technical Architecture Document. No separate repos per module — the module boundaries are enforced by Go package structure and the interface contract, not by repository separation.

---

## 2. Branching Strategy

**Model:** Trunk-based with short-lived feature branches.

- `main` — always buildable and passing tests. Nobody commits directly to `main`.
- `feature/<person>-<short-description>` — e.g. `feature/ayush-notes-repo`, `feature/priya-anthropic-adapter`. Branched from `main`, merged back via pull request.
- `fix/<short-description>` — for bug fixes discovered after a feature has merged.

**Rules:**
- Keep feature branches short-lived (aim to merge within 3–5 days) to avoid large, hard-to-review diffs and to reduce integration risk given the interface-heavy, parallel-development structure of this project.
- Rebase your feature branch on `main` before opening a PR if `main` has moved significantly — don't let branches drift for weeks.
- Delete branches after merge.

---

## 3. Commit Convention

Use **Conventional Commits** format:

```
<type>(<scope>): <short summary>

<optional longer description>
```

**Types:** `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `style`

**Scope:** the package/module affected, matching the folder structure — e.g. `provider`, `notes`, `shell`, `doctor`, `ui`, `storage`, `config`.

**Examples:**
```
feat(provider): implement Anthropic adapter Validate() and ListModels()
fix(shell): correct WSL env var export when path contains spaces
docs(api-contract): update Note struct to include SupersededBy field
test(notes): add unit tests for extractor with mocked adapter
```

**Why this matters for this project specifically:** with 5 people working across distinct modules, a scoped commit history makes it trivial to answer "what's the current state of the provider adapters" or "has anyone touched the notes schema this week" without asking around.

---

## 4. Pull Request Requirements

Every PR must:

1. **Target `main`**, never another feature branch.
2. **Have a description** stating which requirement(s) it addresses — reference the FR-ID from the SRS where applicable (e.g. "Implements FR-2.2, FR-2.3").
3. **Pass CI** (build + `go vet` + `go test ./...`) before it can be merged — no merging with a red build, no exceptions.
4. **Have at least one approving review** from someone other than the author. For changes to a shared interface defined in llmctl_API_INTERFACE_CONTRACT.md, the review must come from the owner of at least one *consuming* module, not just another contributor.
5. **Update documentation in the same PR** if it changes a documented interface, schema, or requirement — the API contract, SRS, or architecture doc should never fall out of sync with the code.

**PR size guidance:** if a PR touches more than ~400 lines excluding tests, consider whether it should be split. Large PRs are where review quality drops and integration bugs hide.

---

## 5. Merge Strategy

**Squash and merge** into `main` for all feature/fix branches. This keeps `main`'s history readable — one commit per logical unit of work — while allowing messy, iterative commits on the feature branch itself during development.

---

## 6. Interface Change Protocol

Because this project depends heavily on interfaces defined in llmctl_API_INTERFACE_CONTRACT.md being stable enough for parallel work, any PR that changes a method signature, struct field, or interface listed in that document must:

1. Flag this explicitly in the PR description ("⚠️ interface change").
2. Tag every module owner listed as a consumer of that interface (see the contract document's per-interface "Consumed by" list) for review, not just the usual reviewer.
3. Not be merged until all tagged consumers have acknowledged, even if their own module doesn't need code changes yet.

This is slightly heavier than the normal one-approval rule, deliberately — interface breakage is the most likely source of wasted work in a 5-person parallel-build structure, and it's cheaper to slow down here than to debug a silent mismatch later.

---

## 7. Issue Tracking

Use GitHub Issues (or whatever tracker the team agrees on) with labels matching module scopes: `provider`, `notes`, `shell`, `doctor`, `ui`, `storage`, `config`, `docs`. Every feature branch should reference an issue number in its PR description for traceability back to the SRS/PRD.

---

## 8. Release Tagging (for later, once public release approaches)

Once the project reaches a state intended for public release: tag releases as `v0.1.0`, `v0.2.0`, etc. (semantic versioning, staying in `0.x` until the tool is considered stable). Each tagged release should have a corresponding entry in `CHANGELOG.md`, generated from the squashed commit history since the previous tag. Not required during the capstone development phase — only relevant once approaching the open-source launch described in the PRD.

---

## 9. What NOT to commit

Per the Technical Architecture Document's `.gitignore` guidance, the following must never be committed, from the very first commit:

```
*.db
*.age
config.toml
.env
.env.local
```

Set up `.gitignore` in the initial repository scaffold, before any module owner starts writing code that touches config or storage.
