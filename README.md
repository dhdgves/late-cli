# Late: AI Dev Team on 5GB VRAM

[English](README.md) | [中文](README.zh-CN.md)

> Orchestrate an entire AI dev team on 5GB VRAM. Ephemeral subagents, exact-match diffs. Single static binary, any model. Zero config, zero context bloat.

[![Release](https://img.shields.io/github/v/release/mlhher/late-cli)](https://github.com/mlhher/late-cli/releases) [![Homebrew](https://img.shields.io/badge/Homebrew-tap-brightgreen.svg)](https://github.com/mlhher/homebrew-late) [![Go Report Card](https://goreportcard.com/badge/github.com/mlhher/late)](https://goreportcard.com/report/github.com/mlhher/late) [![DeepWiki](https://img.shields.io/badge/DeepWiki-docs-blue.svg)](https://deepwiki.com/mlhher/late-cli)

**Drop into any project and start building.** Get to your first prompt in less than 10 seconds.

```bash
# Linux / macOS
brew tap mlhher/late && brew install late

# Universal Fallback (Linux / macOS / Windows WSL)
curl -sfL https://raw.githubusercontent.com/mlhher/late-cli/main/install.sh | bash

cd your-project
late
```

> **Other Installation Methods**
> - **Arch Linux:** `yay -S late-cli-bin`
> - **Linux / macOS / Native Windows:** Download the [latest binary](https://github.com/mlhher/late-cli/releases) and drop it in your PATH. *(macOS manual download: if blocked, run `xattr -d com.apple.quarantine /path/to/late`)*
> 
> **Connecting to Cloud Models?**
> Local models (llama.cpp on `:8080`, the default for llama-server) work out-of-the-box. No configuration required. For cloud providers (DeepSeek, Claude, Gemini, OpenRouter), set your `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `OPENAI_MODEL` environment variables.

![Late Orchestrator planning a multi-phase implementation and spawning the first subagent](assets/late-subagent-handoff.png)
*Lead Architect forming a plan and spawning an atomic subagent for a surgical edit.*

|  | Late | Claude Code | OpenCode | The Weekly Clone |
| --- | --- | --- | --- | --- |
| **Workflow** | **Autonomous Orchestration** | Manual toggling | Manual toggling | Blind execution/Manual toggling |
| **Implementations** | **Ephemeral subagents (Context destroyed)** | Floods main context window | Floods main context window | Floods main context window |
| **KV-Cache** | **Ruthless KV cache management** | Brute-force context dumping | Brute-force context dumping | Brute-force context dumping |
| **System Prompt** | **~1,000 tokens (Always planning workflow)** | 10,000+ tokens | 10,000+ tokens | ~300-1000+ tokens (No-workflow lobotomy) |
| **Dependencies** | **Zero-dependency static binary** | Node.js | Node.js | Python / Node.js |
| **Setup required** | **None (OOTB `llama-server` support)** | Anthropic OAuth / Sign-in | Mandatory JSON tweaking | Flavor of the week JSON/YAML/TOML configs |
| **Built For** | **Builders wanting 10x throughput** | Enterprise expense accounts | Tinkering with settings | Chasing GitHub stars |

> *"The same model feels smarter with Late."* — Reddit

> *"Late-CLI is mindblowing... I'm shocked that the token usage is so minimal, I keep expecting a big bill from DeepSeek's API."* — GitHub Discussions

> [Outperforming Claude Code and Codex for Local LLM Workflows](https://agentnativedev.medium.com/outperforming-claude-code-and-codex-for-local-llm-workflows-5de0e2b1add5) — Agent Native

> **Built with Late:** Late is primarily developed inside Late itself.

Works with **Claude, DeepSeek, Qwen, Gemma (including thinking support for Gemma)**, and any OpenAI-compatible API. See the [Quickstart Guide](docs/quickstart.md) for hybrid model routing, keybindings, MCP setup, Skills and more.

---

## This Fork — Enhanced for Local Deployments

This fork adds Windows-first robustness and ruthless context management for KV-cache-constrained local inference backends (LM Studio, llama.cpp). All changes are tested against real multi-hour coding sessions.

### Context Compaction (inspired by [Terax AI](https://github.com/crynta/terax-ai))

Late now applies a compaction pipeline before every API call to keep the context window lean:

- **dropSupersededReads** — always-on, lossless. When `write_file` or `target_edit` modifies a file, any prior `read_file` result for that same file is elided. The model never sees stale data.
- **batchElideToolResults** — triggers when tokens exceed 70% of the context limit. Old tool-results are elided from the conversation head, preserving the last 24 messages (KEEP_TAIL).
- **Token estimation** — lightweight heuristic (`bytes/4`), no real tokenizer overhead.

Effect on a real 121-message coding session: **105K → 37K tokens (65% reduction)** under a 50% context limit.

### Windows & Encoding Robustness

- **Shell fallback chain**: `pwsh.exe → powershell.exe → cmd.exe` with graceful degradation.
- **GBK→UTF-8 transcoding**: Shell output on Chinese Windows defaults to GBK (CP936). A per-line `DetectAndConvert` pipeline with 12 test functions ensures the TUI renderer never sees invalid UTF-8.
- **`PYTHONIOENCODING=utf-8`**: Injected into every child process to prevent Python from emitting GBK in the first place.
- **Panic recovery**: Shell tool and script execution are wrapped with `defer/recover` to prevent platform-specific crashes from taking down the agent.

### Skill System

- **`SKILL_DIR` compatibility**: Supports both `${{SKILL_DIR}}` (late-cli style) and bare `SKILL_DIR` (WorkBuddy style) in skill instructions.
- **Script invocation via bash tool**: Skill scripts are no longer registered as separate tools. The agent invokes them through the regular `bash` tool, respecting workstation constraints (conda environments, custom interpreters).

### Session Awareness

- **`session_id` injection**: Every API request carries the orchestrator ID as `session_id` in `extra_body`, helping cache-aware backends associate consecutive requests.

### System Prompt

- **Current date injection**: The model always knows the current date without needing a tool call.

---

## How It Works

Standard coding agents do all their work, whether it's planning, implementing, retrying failed edits, or self-healing, in one shared context window. Every retry, every failed implementation, every repair loop pollutes the context the model reasons from. It degrades. You blame the model. The model is fine.

Late separates concerns. A lean orchestrator (~1,000 token system prompt) reads your codebase, forms a plan, and delegates individual implementation tasks to ephemeral subagents. Each subagent gets a fresh isolated context containing only its one task and nothing else. When it completes, that context is destroyed. The orchestrator only ever sees outcomes.

Late manages the KV cache and context window carefully, leaving more room for reasoning. The orchestrator's context grows only from what matters: your instructions and the agent's decisions. Everything the subagent did to get there is gone with it. This is why the same model feels sharper in Late. It reasons from signal, not noise.

---

## Features

- **Hybrid Model Routing:** Architect the plan with a massive reasoning model (e.g., DeepSeek V4), then spawn subagents to execute it using blazing-fast, cheap local models (e.g., Gemma 4).
- **Exact-Match Diffs:** Strict `search`/`replace` blocks with autonomous self-healing on mismatch. Edits fail loud. We never silently corrupt your files.
- **Human-in-the-Loop:** Read-only commands are auto-approved for velocity. Mutations hard-stop for `[y/N]`. Features Session, Project, and Global trust scopes with TTL decay.
- **Stateful Resilience:** The Orchestrator maintains continuous session history on disk. Close your terminal, reboot your machine, and pick up exactly where you left off.
- **MCP Integration:** Natively map external Model Context Protocol servers directly into Late via standard I/O.
- **Agent Skills:** Drop in reusable sets of instructions and scripts. Zero configuration or boilerplate required.
- **Git Worktree Support:** Run independent, parallel agent instances across multiple branches without context bleeding.
- **Gemma 4 Thinking Mode:** Standard wrappers just pipe text to an API, which means they can't trigger Gemma's reasoning. Late includes a dedicated flag to inject the exact tokens required to actually make it think.

---

## License

Built to create engineering leverage, not to supply free infrastructure for AI startups.

**Free for builders:** Use Late freely to write code for any project, including commercial ones. Your output is yours.

**Commercial restrictions:** You may not monetize Late itself. Wrapping the orchestration engine into a paid service or deploying it as enterprise infrastructure requires a commercial agreement.

Late converts to GPLv2 on February 21, 2030. Full license in [LICENSE](LICENSE).
