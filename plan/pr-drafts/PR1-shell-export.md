# refactor(shell): replace unimplementable Export with Snippet and Launch

## ⚠️ Interface change

This changes `shell.Exporter` in `llmctl_API_INTERFACE_CONTRACT.md` §4. Per
`llmctl_GIT_WORKFLOW.md` §6, every consumer owner must acknowledge before merge,
even if their module needs no code change yet.

**Consumers to tag** (from the contract's "Consumed by" list for §4):

- **Person 5** — `app` root model, which calls this after a switch
- **Person 1** — `doctor/wsl_check.go`, and owner of this interface

## What's wrong with the current contract

§4 specifies:

```go
// Export writes the given env vars into the current shell session in the
// correct syntax for the given Kind (e.g. `$env:X=` vs `export X=`).
Export(kind Kind, vars map[string]string) error
```

A process cannot do this. Environment variables are copied into a child at
creation; there is no call on Windows or Unix by which a child mutates its
parent's environment. `llmctl` is a child of the user's shell, so `Export` as
written cannot be implemented by anyone.

This matters beyond the signature: the Phase-I report §4.2.2 states the
mechanism already works ("automatically injecting it into the active
terminal"). That claim needs correcting alongside this change.

## What replaces it

Two mechanisms that do work, split by responsibility:

```go
type Renderer interface {
    // Snippet renders vars as assignments in the syntax of kind, for the
    // user's shell to evaluate itself.
    Snippet(kind Kind, vars map[string]string) (string, error)
}

type Launcher interface {
    // Launch starts name with args, inheriting the current environment plus
    // vars. The caller owns waiting on the returned command.
    Launch(ctx context.Context, vars map[string]string, name string, args ...string) (*exec.Cmd, error)
}
```

**Snippet** backs a new `llmctl env` subcommand. The shell performs the
mutation on itself:

```bash
eval "$(llmctl env)"          # bash / WSL
llmctl env | Invoke-Expression # PowerShell
```

**Launch** backs `llmctl run -- <cmd>`, starting a tool or agent with the
provider's variables already in its environment. This is the path that serves
the "so Claude Code can read `ANTHROPIC_API_KEY`" goal in the PRD without
requiring the impossible.

`Detect` and `Sync` are unchanged. `Syncer` is split out of the old `Exporter`
so a consumer that only needs to render a snippet does not depend on the
Windows↔WSL sync path.

## What was considered and rejected

Writing variables to a shell profile, `.bashrc`, or the Windows registry would
make them persist "for real". Rejected: it writes decrypted API keys to disk in
plaintext, violating SRS NFR-3 and NFR-4, and mutates user state outside the
session llmctl was invoked in.

## Requirements addressed

- SRS **FR-2.3** — export the correct environment variables for the detected shell
- SRS **NFR-3**, **NFR-4** — keys encrypted at rest, decrypted only in memory
- Plan **TASK-004**

## Contract edit included in this PR

Replace §4's `Exporter` block with the `Renderer` / `Launcher` / `Syncer`
interfaces above, and update the "Consumed by" line to:

> **Consumed by:** `cmd/llmctl` (the `env` and `run` subcommands), `app` root
> model (after a switch), `doctor/wsl_check.go`

Add to the contract note:

> A process cannot modify its parent shell's environment. Any future proposal
> that claims to "set variables in the user's shell" is invalid; the shell must
> evaluate a snippet, or llmctl must spawn the process itself.

## Checklist (`llmctl_GIT_WORKFLOW.md` §4)

- [x] Targets `main`
- [x] References the requirements it addresses
- [x] CI green — build, vet, gofmt, test, OS isolation guard, cross-compile
- [x] Contract document updated in this PR
- [ ] Acknowledged by Person 5 (`app`)
- [ ] Acknowledged by Person 1 (`doctor`, interface owner)
- [ ] Phase-I report §4.2.2 correction filed as a follow-up issue
