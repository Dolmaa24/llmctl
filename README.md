# 🚀 llmctl (LLM Controller)

<div align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Windows-10%2F11-0078D6?style=for-the-badge&logo=windows&logoColor=white" alt="Windows Support" />
  <img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite Persistence" />
</div>

<br>

**llmctl** is a next-generation local-first terminal dashboard built to seamlessly manage multiple LLM providers (like Anthropic, OpenRouter, and local Ollama) right from your Windows/WSL terminal. 

The standout feature? **Cost-aware context handoffs**. 💸 When you switch from a cheap, fast model to an expensive, smart model mid-conversation, `llmctl` doesn't just blindly send your massive chat history. Instead, it continuously extracts key facts and decisions in the background, and seamlessly hands off a compressed "summary note" to the new model, saving you massive amounts of tokens and API costs!

*Built for the Capstone Project (DSN4091), Team No. 260, VIT Bhopal University.*

---

## ✨ What does it actually do? (In Easy Terms)

Imagine you are chatting with an AI for an hour about coding. You've been using a cheap, local model (like Ollama 7B). Suddenly, you hit a hard problem and need to switch to Claude 3.5 Sonnet. 

Normally, switching means you have to either start a brand new chat (losing all context) or send all 100 messages to Claude (which costs a ton of money).

**With `llmctl`:**
1. **Background Note-Taker:** As you chat, `llmctl` runs a lightweight model in the background that "takes notes" on your conversation.
2. **Persistent Memory (PR4 ✨):** It saves these notes safely into a local SQLite database (`extraction_runs`), so even if you close the app or restart your computer, it remembers everything!
3. **Smart Handoff:** When you click "Switch to Claude", `llmctl` shows you exactly how much money you are saving. It then sends Claude the summarized *notes* plus only the very last few messages, getting Claude up to speed instantly for pennies on the dollar!

### Key Features
- 🔌 **Provider Profiles:** Manage Anthropic, OpenRouter, and Ollama seamlessly. Keys are encrypted at rest!
- 🔀 **Instant Handoff:** Switch providers and models in one action.
- 💾 **Persistent Extraction Engine:** Background note extraction survives restarts.
- 🩺 **Doctor Panel:** Continuously reports the health of your local environment (WSL, network, auth).

---

## 🛠️ Status & Recent Updates

> **Current Status: Core Engine & Persistence (Phase 1 Complete!)** 🚀
> We just merged **PR4** which fully implemented the `ExtractionRun` persistence layer! The background engine now flawlessly saves its progress to the local SQLite database.

**What's Next?** Phase 2 will focus on tuning the system for local hardware (finding the best 7B-14B Ollama model for extraction) and finalizing the terminal UI.

---

## 💻 Requirements

- **Go 1.22+**
- **Windows 10/11 (build 19041+)** with WSL2 for shell/diagnostics features.
- *(Note: The core engine builds and runs on macOS and Linux as well!)*

## 🏗️ Development

```bash
make check    # gofmt, go vet, go test, OS isolation guard
make cross    # prove the zero-CGO cross-compile still works
make build    # binary into bin/
```

- `make check` must pass before a pull request is opened. 
- `make cross` proves a macOS machine can still produce a Windows binary without a C toolchain (keeping the multi-platform path open).

## 📁 Project Layout

| Path | Contents |
|---|---|
| `cmd/llmctl/` | Entrypoint and subcommands |
| `internal/session/` | Core Data Types (`Message`, `Session`, `Note`, `ExtractionRun`) |
| `internal/provider/` | API Adapters for Anthropic, Ollama, OpenRouter |
| `internal/notes/` | Background Note Extraction Engine |
| `internal/storage/` | SQLite database repositories & migrations |
| `internal/config/` | TOML config and age-encrypted secret store |
| `internal/shell/` | Windows/WSL shell detection and env rendering |
| `internal/costestimate/` | Token counting for the switch comparison UI |

---

## 🤝 Contributing

Before writing code, please read the **[`llmctl_API_INTERFACE_CONTRACT.md`](./llmctl_API_INTERFACE_CONTRACT.md)**! It is the ultimate source of truth for all cross-module signatures. Changing an interface requires updating that document in the same pull request.

Commits follow Conventional Commits (e.g., `feat(provider): implement Anthropic Validate()`).
