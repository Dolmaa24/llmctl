# llmctl — Team Setup & Collaboration Guide

**Who this is for:** Two audiences, covered separately in each section.
- **🔧 Setup person** (P5 — the integrator role) — the one creating, configuring, and sharing things
- **👤 Everyone else** — the one receiving access and getting their machine ready

Read your own perspective in each section. Both sides need to complete their steps before the team can work in parallel.

---

## Section 1: GitHub Repository

### 🔧 Setup person (P5)

1. Go to github.com → click your profile → **Your organizations** → **New organization** → choose the free plan → name it `llmctl-dev` (or similar).
2. Inside the org, click **New repository** → name it `llmctl` → set to **Private** → **do not** initialize with a README (you'll push one manually).
3. Clone it locally:
   ```
   git clone https://github.com/llmctl-dev/llmctl.git
   cd llmctl
   ```
4. Create the `.gitignore` file first, before anything else:
   ```
   *.db
   *.age
   config.toml
   .env
   .env.local
   secrets/
   ```
   Commit and push:
   ```
   git add .gitignore
   git commit -m "chore: add gitignore before any code"
   git push origin main
   ```
5. Go to the repo → **Settings** → **Collaborators and teams** → **Add people** → add each teammate by their GitHub username. Set role to **Write**.
6. Set up branch protection: **Settings** → **Branches** → **Add rule** → Branch name pattern: `main` → check:
   - ✅ Require a pull request before merging
   - ✅ Require approvals: **1**
   - ✅ Require status checks to pass (add the CI check name once CI is set up in Section 2)
   - ✅ Do not allow bypassing the above settings
7. Create GitHub labels for module ownership: **Issues** → **Labels** → **New label** — create one for each: `provider`, `notes`, `shell`, `doctor`, `ui`, `storage`, `config`, `docs`, `integration`.
8. Create four Milestones: **Issues** → **Milestones** → **New milestone**: `Phase 1 (Month 2)`, `Phase 2 — Demo (Month 4)`, `Phase 3 (Month 6)`, `Phase 4 — Release (Month 8)`.

### 👤 Everyone else

1. Accept the GitHub collaborator email invitation (check spam if not in inbox).
2. Install Git if not already installed: https://git-scm.com/downloads
3. Clone the repo:
   ```
   git clone https://github.com/llmctl-dev/llmctl.git
   cd llmctl
   ```
4. Confirm you can see the repo and that pushing to `main` directly is blocked (try `git push origin main` on an empty commit — it should be rejected). This confirms branch protection is working.
5. Set your Git identity if you haven't already:
   ```
   git config --global user.name "Your Name"
   git config --global user.email "your@email.com"
   ```

---

## Section 2: CI Pipeline (GitHub Actions)

### 🔧 Setup person (P5)

1. Inside the repo, create the directory and workflow file:
   ```
   mkdir -p .github/workflows
   ```
2. Create `.github/workflows/ci.yml` with this content:
   ```yaml
   name: CI

   on:
     pull_request:
       branches: [main]
     push:
       branches: [main]

   jobs:
     build-and-test:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4

         - name: Set up Go
           uses: actions/setup-go@v5
           with:
             go-version: '1.22'

         - name: Verify dependencies
           run: go mod verify

         - name: Build (Linux)
           run: go build ./...

         - name: Cross-compile for Windows
           run: GOOS=windows GOARCH=amd64 go build -o /dev/null ./...
           # This is the check that catches OS-specific code leaking outside internal/shell

         - name: Vet
           run: go vet ./...

         - name: Test
           run: go test ./...
   ```
3. Commit and push:
   ```
   git add .github/
   git commit -m "chore(ci): add GitHub Actions CI workflow"
   git push origin main
   ```
4. Go to **Settings** → **Branches** → edit the `main` branch rule → under **Require status checks** → search for `build-and-test` → add it. Now CI must be green before any PR can merge.

### 👤 Everyone else

1. Nothing to install or configure — CI runs automatically on GitHub's servers every time you open a PR.
2. When you open a PR, scroll to the bottom of the PR page — you'll see a **Checks** section. Wait for it to go green before asking for a review. If it goes red, click **Details** to see which step failed and fix it before tagging a reviewer.
3. **Mac teammate (P3) specifically:** after any merge to `main`, run `go build ./...` locally on your Mac. This is the one check CI doesn't catch (CI runs on Linux). If it fails on your Mac but passes on CI, that means OS-specific code leaked out of `internal/shell/` — flag it in `#interface-changes` immediately.

---

## Section 3: Go Project Scaffold

### 🔧 Setup person (P5)

1. Make sure Go 1.22+ is installed: https://go.dev/dl/
2. Initialize the module:
   ```
   go mod init github.com/llmctl-dev/llmctl
   ```
3. Create the full folder structure (empty `.gitkeep` files to hold directories):
   ```
   mkdir -p cmd/llmctl
   mkdir -p internal/{app,ui/{providerpane,transcript,notespane,statusbar,switchconfirm,styles}}
   mkdir -p internal/{provider/{anthropic,openrouter,ollama},shell,doctor}
   mkdir -p internal/{session,notes,storage/migrations,config,costestimate}
   mkdir -p docs scripts
   touch cmd/llmctl/main.go
   ```
4. Add the shared dependencies that every module will need:
   ```
   go get github.com/charmbracelet/bubbletea
   go get github.com/charmbracelet/lipgloss
   go get github.com/charmbracelet/bubbles
   go get github.com/spf13/cobra
   go get modernc.org/sqlite
   go get filippo.io/age
   go get github.com/zalando/go-keyring
   go get github.com/BurntSushi/toml
   go get github.com/hashicorp/go-retryablehttp
   go get github.com/stretchr/testify
   ```
5. Copy all project docs into the `docs/` folder, commit everything:
   ```
   git add .
   git commit -m "chore: scaffold project structure and add dependencies"
   git push origin main
   ```
6. Create the first migration file `internal/storage/migrations/0001_init.sql` with the full schema from the Technical Architecture Document. Then write a `storage/db.go` that opens the SQLite file and runs all embedded migrations on startup. This unblocks everyone else's mocks.

### 👤 Everyone else

1. Install Go 1.22+: https://go.dev/dl/ — verify with `go version` in your terminal.
2. Pull the latest main after P5 pushes the scaffold:
   ```
   git pull origin main
   ```
3. Run:
   ```
   go mod download
   go build ./...
   ```
   Both should complete with no errors. If `go build` fails on your machine but not someone else's, flag it immediately — it means a dependency or Go version mismatch.
4. **Mac teammate:** run `go build ./...` specifically. If it passes here, cross-platform compilation is working correctly from day one.

---

## Section 4: Branching and Daily Workflow

### 🔧 Setup person (P5)

No ongoing setup here — this is policy. Share the Git Workflow Document with the team and confirm everyone has read Section 2 (Branching Strategy) and Section 4 (PR Requirements) specifically.

The one thing to set up: create one example feature branch yourself, open a draft PR, and walk the team through what a green CI run looks like. Seeing it once is worth more than reading it.

### 👤 Everyone else

**Every time you start work on something new:**

1. Make sure your local `main` is up to date:
   ```
   git checkout main
   git pull origin main
   ```
2. Create your feature branch:
   ```
   git checkout -b feature/yourname-short-description
   ```
   Example: `feature/ayush-notes-repo`, `feature/priya-anthropic-adapter`

3. Do your work. Commit regularly using the Conventional Commits format:
   ```
   git commit -m "feat(provider): implement Anthropic Validate() and ListModels()"
   git commit -m "fix(shell): correct env var export when path contains spaces"
   ```
   Format is: `type(scope): description` — types are `feat`, `fix`, `refactor`, `test`, `docs`, `chore`.

4. Push your branch:
   ```
   git push origin feature/yourname-short-description
   ```

5. Go to GitHub → your branch will appear with a **Compare & pull request** button → click it → fill in:
   - Title: same as your commit message style
   - Description: which FR-IDs from the SRS this addresses (e.g. "Implements FR-2.1, FR-2.2")
   - Tag at least one reviewer

6. Wait for CI to go green. If red, fix locally, commit, push — CI re-runs automatically.

7. After approval + green CI → **Squash and merge** (not regular merge, not rebase merge — squash specifically, per the Git Workflow Document).

8. Delete your branch after merge (GitHub will offer a button for this).

---

## Section 5: Interface Change Protocol

This section is the most important one for integration. Read it carefully.

### 🔧 Setup person (P5) — one-time setup

Create a pinned message in your team Discord/WhatsApp with exactly this text:

> **⚠️ INTERFACE CHANGE RULE**
> If your PR changes any type, method signature, or interface in `llmctl_API_INTERFACE_CONTRACT.md` — even a field rename — you must:
> 1. Post here first with what you're changing and why
> 2. Wait for acknowledgement from every teammate whose module consumes that interface
> 3. Only then open the PR
> Do not merge interface changes without this. Silent interface changes are the #1 cause of integration pain.

### 👤 Everyone else

**If you need to change a shared interface or struct:**

1. Do not just make the change and open a PR.
2. Post in the team channel: "I need to change `[struct/method name]` in the interface contract. Current: `[paste current signature]`. Proposed: `[paste new signature]`. Reason: `[one line]`."
3. Wait for the owners of every consuming module to reply. Consuming modules are listed in `llmctl_API_INTERFACE_CONTRACT.md` under each interface's "Consumed by" field.
4. Once everyone has acknowledged → update `llmctl_API_INTERFACE_CONTRACT.md` in the same PR as your code change.
5. Mark the PR with the `integration` label.

**If someone posts an interface change that affects your module:**

1. Read it carefully.
2. Reply with either "✅ ok, no impact on my module" or "❌ this breaks [specific thing] — let's discuss."
3. Do not just leave it on read. An unanswered interface change notice means your module may break silently.

---

## Section 6: Communication Setup

### 🔧 Setup person (P5)

1. Create a Discord server (or use WhatsApp — whichever the team already uses) with at least these channels:
   - `#general` — daily chat
   - `#interface-changes` — only for interface/schema change notices
   - `#ci-alerts` — optional, pipe GitHub Actions failure notifications here via GitHub's Discord webhook integration
   - `#blockers` — post here if you're stuck and need someone else to unblock you

2. Connect GitHub to Discord (optional but useful): repo **Settings** → **Webhooks** → add a webhook pointing to your Discord channel's webhook URL. Set it to notify on pushes and PR events.

### 👤 Everyone else

1. Join the Discord/group.
2. Turn on notifications for `#interface-changes` specifically — this is the one channel where missing a message costs you real time later.
3. Weekly sync: show up, even if you have nothing to report. Knowing what other modules are doing is how you catch integration mismatches before they become bugs.

---

## Section 7: One-Time Checklist — Before Anyone Writes Feature Code

This is the gate. All 10 items should be done before Phase 1 work begins.

| # | Item | Who | Done? |
|---|---|---|---|
| 1 | GitHub repo created, all 5 invited | P5 | ☐ |
| 2 | `.gitignore` committed (no secrets ever committed) | P5 | ☐ |
| 3 | Branch protection on `main` active | P5 | ☐ |
| 4 | CI pipeline green on a test PR | P5 | ☐ |
| 5 | Go scaffold + dependencies committed | P5 | ☐ |
| 6 | `0001_init.sql` migration committed, `db.go` opens DB cleanly | P5 | ☐ |
| 7 | All 5 have cloned and run `go build ./...` successfully | Everyone | ☐ |
| 8 | Mac teammate confirmed `go build ./...` passes on macOS | P3 | ☐ |
| 9 | All 5 have read the API Interface Contract | Everyone | ☐ |
| 10 | Discord/WhatsApp set up, interface-change rule pinned | P5 | ☐ |

When all 10 are checked: Phase 1 begins.

