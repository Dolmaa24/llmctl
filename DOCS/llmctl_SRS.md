# Software Requirements Specification (SRS): llmctl

**Status:** Draft v1
**Format:** Loosely follows IEEE 830 structure, adapted for a capstone project
**Companion to:** llmctl_PRD.md, llmctl_TECHNICAL_ARCHITECTURE.md, llmctl_API_INTERFACE_CONTRACT.md

---

## 1. Introduction

### 1.1 Purpose
This document specifies the functional and non-functional requirements for llmctl, a locally-run terminal dashboard for managing multiple LLM providers on Windows/WSL. It translates the product goals described in the PRD into individually verifiable requirements, each with a unique ID, so that implementation can be checked against a specific, testable statement rather than a general description.

### 1.2 Scope
llmctl allows a developer to configure, switch between, and diagnose issues with multiple LLM providers (Anthropic, OpenRouter, Ollama) from a single terminal interface, preserving conversational context across a provider switch via a distillation-based handoff mechanism, without requiring any server-side component.

### 1.3 Intended Audience
Development team members (for implementation and self-verification), project supervisor (for review and approval), and future contributors evaluating the system post-release.

### 1.4 Definitions and Abbreviations

| Term | Definition |
|---|---|
| TUI | Terminal User Interface |
| Provider | An LLM API service (Anthropic, OpenRouter, or Ollama) |
| Handoff | The process of transferring conversational context to a new provider on switch |
| Distillation | Extracting structured, condensed notes from a conversation instead of using the raw transcript |
| Note | A single structured, tagged fact extracted from the conversation |
| Doctor | The diagnostics subsystem that checks environment health |
| WSL | Windows Subsystem for Linux |

### 1.5 References
llmctl_PRD.md, llmctl_TECHNICAL_ARCHITECTURE.md, llmctl_API_INTERFACE_CONTRACT.md

---

## 2. Overall Description

### 2.1 Product Perspective
llmctl is a standalone, locally-installed binary. It is not a companion to an existing system and has no server-side counterpart. It reads and writes only to the local filesystem (an encrypted config file and a SQLite database).

### 2.2 Product Functions (Summary)
- Manage provider configuration and credentials
- Switch active provider/model with correct shell environment propagation
- Preserve conversational context across a switch via distillation, with attribution
- Continuously monitor and report the health of the local AI development environment
- Export session transcripts and notes

### 2.3 User Characteristics
Primary users are individual developers on Windows using WSL, comfortable operating in a terminal and managing their own API credentials, but not necessarily systems programming experts. See PRD Section 2 for full detail.

### 2.4 Constraints
- Must run entirely locally; no server-side component (PRD Section 4)
- Must target Windows/WSL for this project; architecture must not preclude future macOS/Linux support (PRD Section 6)
- Must not implement retrieval-based (embedding/vector search) memory in this project (PRD Section 8)
- Built in Go using Bubble Tea, per the approved Technical Architecture Document

### 2.5 Assumptions and Dependencies
- The user has WSL2 installed and has at least one of the three supported providers accessible (a valid API key, or a running local Ollama instance)
- The user's terminal emulator supports 24-bit color and standard ANSI rendering

---

## 3. Functional Requirements

Each requirement is uniquely numbered and traceable to a PRD feature. Priority: **M** = Must-have (MVP), **N** = Nice-to-have (explicitly deferred).

### 3.1 Provider Configuration (PRD Feature 1)

| ID | Requirement | Priority |
|---|---|---|
| FR-1.1 | The system shall allow the user to add a new provider profile (Anthropic, OpenRouter, or Ollama) specifying an API key (where applicable), base URL, and default model. | M |
| FR-1.2 | The system shall encrypt all stored API keys at rest and never write them to disk in plaintext. | M |
| FR-1.3 | The system shall validate a provider's configuration (connectivity and auth) at the time it is added, and report success or a specific failure reason. | M |
| FR-1.4 | The system shall allow the user to edit or remove an existing provider profile. | M |
| FR-1.5 | The system shall list all configured providers and their current status in the TUI. | M |

### 3.2 Provider/Model Switching (PRD Feature 2)

| ID | Requirement | Priority |
|---|---|---|
| FR-2.1 | The system shall allow the user to switch the active provider and model with a single action from within the TUI. | M |
| FR-2.2 | The system shall detect whether it is running in a PowerShell or WSL/Bash shell context. | M |
| FR-2.3 | The system shall export the correct environment variables for the detected shell context upon switching providers. | M |
| FR-2.4 | The system shall validate that the target model identifier exists for the target provider before completing a switch, and reject the switch with a clear error if it does not. | M |

### 3.3 Distillation-Based Context Handoff (PRD Feature 3)

| ID | Requirement | Priority |
|---|---|---|
| FR-3.1 | The system shall extract structured notes from the conversation as the session progresses. | M |
| FR-3.2 | Each extracted note shall be tagged with the provider and model that produced the source content. | M |
| FR-3.3 | Each message in the transcript shall be tagged with the provider and model that produced it. | M |
| FR-3.4 | Upon a provider switch, the system shall assemble a handoff consisting of accumulated notes plus a configurable number of recent raw messages, rather than the full raw transcript. | M |
| FR-3.5 | Before confirming a switch, the system shall display an estimated token count for both a full-transcript replay and the distilled handoff, so the user can compare cost. | M |
| FR-3.6 | The transcript view shall display a visible divider at each point where the active provider/model changed. | M |
| FR-3.7 (deferred) | The system shall support retrieval-based (embedding and semantic search) selection of relevant notes at handoff time, instead of sending all accumulated notes. | N — explicitly out of scope; see PRD Section 8 |

### 3.4 Diagnostics ("Doctor") (PRD Feature 4)

| ID | Requirement | Priority |
|---|---|---|
| FR-4.1 | The system shall continuously check and display the status of the WSL environment while the TUI is open. | M |
| FR-4.2 | The system shall continuously check and display the reachability of each configured provider's API endpoint. | M |
| FR-4.3 | The system shall continuously check and display the validity of stored authentication for each configured provider. | M |
| FR-4.4 | The system shall check the status of relevant local services (e.g. Redis, if configured) as part of the diagnostics panel. | M |
| FR-4.5 | The system shall provide a non-interactive `llmctl doctor` command that runs all diagnostics checks once and prints a report, for use outside the TUI. | M |

### 3.5 Session Transcript and Notes (PRD Feature 5)

| ID | Requirement | Priority |
|---|---|---|
| FR-5.1 | The system shall display a scrollable, attributed transcript of the current session. | M |
| FR-5.2 | The system shall display a separate, browsable view of the notes extracted for the current session. | M |
| FR-5.3 | The system shall allow the user to export the current session's transcript and notes as a Markdown or JSON file. | M |

---

## 4. Non-Functional Requirements

| ID | Category | Requirement |
|---|---|---|
| NFR-1 | Performance | The TUI shall remain responsive (input latency under 100ms) during background operations such as note extraction and diagnostics checks. |
| NFR-2 | Performance | Diagnostics checks shall individually time out after 2 seconds and report a warning state rather than blocking the UI indefinitely. |
| NFR-3 | Security | API keys shall be encrypted at rest using a well-reviewed encryption library (age); no custom cryptography shall be implemented. |
| NFR-4 | Security | Decrypted API keys shall exist in memory only for the duration needed and shall not be logged, even at debug log level. |
| NFR-5 | Reliability | A failure in one provider adapter (e.g. a network error) shall not crash the application or affect other configured providers. |
| NFR-6 | Reliability | The application shall not lose session data (messages, notes) in the event of an unexpected termination; writes to SQLite shall be committed incrementally, not batched in memory until exit. |
| NFR-7 | Portability | No module outside `shell/` and `doctor/` shall contain OS-specific branching logic, to preserve the future macOS/Linux extension path described in the PRD. |
| NFR-8 | Usability | All destructive actions (removing a provider profile, clearing a session) shall require explicit confirmation. |
| NFR-9 | Maintainability | Every provider integration shall be implemented solely by satisfying the `provider.Adapter` interface, with no modification to code outside the `provider/` package required. |
| NFR-10 | Portability | The notes storage schema shall not require restructuring to support a future retrieval-based memory system (see PRD Section 8 and the Technical Architecture Document's schema definition). |
| NFR-11 | Installability | The application shall be distributable as a single static binary requiring no separate runtime installation. |

---

## 5. External Interface Requirements

### 5.1 User Interfaces
Terminal-based UI only, rendered via Bubble Tea. No graphical (windowed) interface, no web interface. Full specification of panes and layout is covered by the UI/Wireframe reference document.

### 5.2 Software Interfaces
- Anthropic API (HTTPS/REST)
- OpenRouter API (HTTPS/REST, OpenAI-compatible)
- Ollama local API (HTTP, typically `localhost:11434`)
- Windows Credential Manager / OS keyring (via `go-keyring`)

### 5.3 Hardware Interfaces
None beyond standard terminal I/O; no special hardware access required.

---

## 6. Traceability Note

Every functional requirement in Section 3 maps to a PRD feature (noted in each subsection heading) and to an interface defined in llmctl_API_INTERFACE_CONTRACT.md. When implementing a requirement, confirm the corresponding interface contract first — the SRS states *what* the system must do; the interface contract states *how* modules will cooperate to do it.

---

## 7. Open Items

Carried forward from the PRD's open questions, restated here as requirements-level decisions still to be made:

1. FR-3.1's extraction trigger (continuous vs. switch-triggered) is not yet finalized — see PRD open question #1.
2. FR-3.4's "configurable number of recent raw messages" needs a default value decided before implementation — see PRD open question #5.
3. The minimum required set of FR-4.x checks for the mid-point demo vs. full launch needs to be finalized — see PRD open question #3.
