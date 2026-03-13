---
name: nd
description: >
  Vault-backed issue tracker storing issues as Obsidian-compatible markdown files.
  Use for multi-session work, dependency tracking, and persistent context that
  survives conversation compaction. No database server. No size limits. Git-native.
allowed-tools: "Read,Bash(nd:*)"
version: "0.10.3"
author: "Ramiro Salas <https://github.com/RamXX>"
license: "Apache-2.0"
---

# nd -- Persistent Issue Memory for AI Agents

Vault-backed issue tracker that stores issues as plain markdown files with YAML frontmatter. Built on [vlt](https://github.com/RamXX/vlt) (Obsidian vault library). Issues survive compaction, sync via git, and have no size limits.

## nd vs TodoWrite

| nd (persistent) | TodoWrite (ephemeral) |
|-----------------|----------------------|
| Multi-session work | Single-session tasks |
| Complex dependencies | Linear execution |
| Survives compaction | Conversation-scoped |
| Git-backed, plain files | Local to session |
| Unlimited content | Lightweight |

**Decision test**: "Will I need this context after compaction?" -- YES = nd

**Use nd when**:
- Work spans multiple sessions or days
- Tasks have dependencies or blockers
- Need to survive conversation compaction
- Exploratory/research work with fuzzy boundaries
- Issue content exceeds what fits in a TodoWrite item

**Use TodoWrite when**:
- Single-session linear tasks
- Simple checklist for immediate work
- All context is in current conversation

## Prerequisites

```bash
nd --version  # Verify nd is installed and in PATH
```

- **nd CLI** installed (`make install` from source)
- The vault uses [vlt](https://github.com/RamXX/vlt) for all file operations. If you need deeper vault manipulation (frontmatter surgery, wikilinks, templates), consult the **vlt skill** for its full API.

## Shell Usage

Do NOT redirect stderr on nd commands. Specifically:
- No `2>&1` -- merging streams causes duplicate error display in Claude Code
- No `2>/dev/null` -- swallowing stderr hides errors you need to see

Claude Code's Bash tool already captures and displays stderr separately. Just run nd commands bare.

## CLI Reference

**Run `nd prime`** for AI-optimized project context (auto-loaded by hooks).
**Run `nd <command> --help`** for specific command usage.

Essential commands: `nd ready`, `nd create`, `nd show`, `nd update`, `nd close`, `nd dep`

## Session Protocol

1. `nd ready` -- Find unblocked work
2. `nd show <id>` -- Get full context
3. `nd start <id>` -- Claim work (alias for `nd update <id> --status=in_progress`)
4. Work. Add notes as you go: `nd update <id> --append-notes "..."`
5. `nd close <id> --reason="..."` -- Complete task (auto-unblocks dependents)
6. `git push` -- Sync to remote (issues are files in git)

## Storage

Issues are markdown files in `<vault>/issues/`. By default nd uses the nearest local `.vault/`, but `--vault` and `ND_VAULT_DIR` override that, and repos using shared worktree state resolve the live vault from the repo's git common dir so worktrees share one backlog. Each issue file has YAML frontmatter (id, status, priority, type, deps, follows/led_to) and markdown body (Description, Acceptance Criteria, Design, Notes, History, Links, Comments). You can `cat`, `grep`, and `git diff` them directly.

For the full storage format specification, see [STORAGE.md](resources/STORAGE.md).

## Core Operations

| Operation | Command | Resource |
|-----------|---------|----------|
| Find work | `nd ready`, `nd blocked`, `nd stale` | [WORKFLOWS.md](resources/WORKFLOWS.md) |
| Create issues | `nd create`, `nd q` (quick capture) | [ISSUE_CREATION.md](resources/ISSUE_CREATION.md) |
| Dependencies | `nd dep add/rm/relate/cycles/tree` | [DEPENDENCIES.md](resources/DEPENDENCIES.md) |
| Execution paths | `nd path`, `--follows`, `--start` | [DEPENDENCIES.md](resources/DEPENDENCIES.md) |
| Epics | `nd epic tree/status/close-eligible` | [EPICS.md](resources/EPICS.md) |
| Visualization | `nd graph` (dep DAG), `nd path` (exec chains) | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| Custom statuses | `nd config set status.custom` | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| FSM enforcement | `nd config set status.fsm true` | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| Defer work | `nd defer/undefer` | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| Statistics | `nd stats`, `nd count` | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| Aliases | `nd start`, `nd block`, `nd resolve`, `nd unblock` | [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) |
| Search | `nd search "query"` | -- |
| Health | `nd doctor [--fix]` | [TROUBLESHOOTING.md](resources/TROUBLESHOOTING.md) |
| AI context | `nd prime [--json]` | -- |
| Import | `nd import --from-beads` | [MIGRATION.md](resources/MIGRATION.md) |

## Resources

| Resource | Content |
|----------|---------|
| [CLI_REFERENCE.md](resources/CLI_REFERENCE.md) | Complete command syntax and flags |
| [WORKFLOWS.md](resources/WORKFLOWS.md) | Session start, compaction recovery, handoff |
| [ISSUE_CREATION.md](resources/ISSUE_CREATION.md) | When and how to create issues |
| [DEPENDENCIES.md](resources/DEPENDENCIES.md) | Dependency semantics and epic planning |
| [EPICS.md](resources/EPICS.md) | Epic hierarchies and tree views |
| [STORAGE.md](resources/STORAGE.md) | File format, frontmatter schema, vault layout |
| [MIGRATION.md](resources/MIGRATION.md) | Migrating from beads (bd) to nd |
| [TROUBLESHOOTING.md](resources/TROUBLESHOOTING.md) | Common problems and fixes |
| [PATTERNS.md](resources/PATTERNS.md) | Usage patterns for AI agents |

## Full Documentation

- **nd prime**: AI-optimized workflow context
- **GitHub**: [github.com/RamXX/nd](https://github.com/RamXX/nd)
- **vlt** (underlying vault library): [github.com/RamXX/vlt](https://github.com/RamXX/vlt)
