# llmctl

A local-first terminal dashboard for managing multiple LLM providers on
Windows/WSL, with cost-aware context handoff when you switch mid-session.

Capstone Project (DSN4091), Team No. 260, VIT Bhopal University.

> **Status: scaffold.** Interfaces, schema and CI are in place. No feature is
> implemented yet. See `plan/architecture-llmctl-distillation-1.md`.

## What it does

- Manages provider profiles (Anthropic, OpenRouter, Ollama) with keys encrypted at rest
- Switches provider and model in one action, with shell-correct environment handling
- Carries conversation context across a switch as structured notes instead of a
  full transcript replay, showing the token comparison before you confirm
- Reports the health of the local environment continuously

## Requirements

- Go 1.22+
- Windows 10/11 (build 19041+) with WSL2 for the shell and diagnostics features;
  everything else builds and runs on macOS and Linux

## Development

```
make check    # gofmt, go vet, go test, OS isolation guard
make cross    # prove the zero-CGO cross-compile still works
make build    # binary into bin/
```

`make check` must pass before a pull request is opened. `make cross` proves a
macOS machine can still produce a Windows binary without a C toolchain — the
check that keeps the multi-platform path open.

## Layout

| Path | Contents |
|---|---|
| `cmd/llmctl/` | Entrypoint and subcommands |
| `internal/session/` | Canonical types: `Message`, `Session`, `Note`, `SwitchEvent` |
| `internal/provider/` | The `Adapter` interface and per-provider implementations |
| `internal/notes/` | Note extraction |
| `internal/storage/` | SQLite repositories and embedded migrations |
| `internal/config/` | TOML config and the age-encrypted secret store |
| `internal/shell/` | Shell detection and env rendering — OS-specific |
| `internal/doctor/` | Diagnostics checks — OS-specific |
| `internal/costestimate/` | Token counting for the switch comparison |
| `internal/ui/` | Bubble Tea panes |
| `internal/mock/` | Contract-conforming mocks for every interface |
| `eval/` | Benchmark corpus, comparison arms, results, analysis |
| `plan/` | Implementation plan |

Only `internal/shell/` and `internal/doctor/` may contain OS-specific
branching. `scripts/check_os_isolation.sh` enforces this in CI.

## Where llmctl keeps its data

| File | Holds | Default location |
|---|---|---|
| `config.toml` | Provider profiles. No secrets. | `%APPDATA%\llmctl\` |
| `secrets.age` | API keys, encrypted with `age` | `%APPDATA%\llmctl\` |
| `secrets.age.index` | Which providers have a key. No secrets. | `%APPDATA%\llmctl\` |
| `llmctl.db` | Sessions, messages, notes, switch events, extraction runs, and a mirror of the provider profiles | `%APPDATA%\llmctl\` |

The passphrase for `secrets.age` is created on first use and kept in Windows
Credential Manager, so there is nothing to type. Inside WSL the files live
under `~/.config/llmctl/` on the Linux filesystem; keep the database off
`/mnt/c`, where SQLite file locking is unreliable.

| Variable | Effect |
|---|---|
| `LLMCTL_CONFIG_DIR` | Directory for the config and secrets files |
| `LLMCTL_DB_PATH` | Path of the database file |
| `LLMCTL_NO_KEYRING` | Use a typed passphrase instead of the OS keyring (needed in WSL, which usually has no keyring service) |

## Contributing

Read `llmctl_API_INTERFACE_CONTRACT.md` first — it is the authority on every
cross-module signature. Changing one requires updating that document in the
same pull request and getting acknowledgement from every listed consumer, per
`llmctl_GIT_WORKFLOW.md` section 6.

Commits follow Conventional Commits with the package as scope, e.g.
`feat(provider): implement Anthropic Validate()`.
