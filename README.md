# nd - Vault-backed Issue Tracker

[![CI](https://github.com/RamXX/nd/actions/workflows/ci.yml/badge.svg)](https://github.com/RamXX/nd/actions/workflows/ci.yml)
[![Release](https://github.com/RamXX/nd/actions/workflows/release.yml/badge.svg)](https://github.com/RamXX/nd/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/RamXX/nd)](https://goreportcard.com/report/github.com/RamXX/nd)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**nd** (short for `node` as node in a graph) is a Git-native issue tracker that stores issues as Obsidian-compatible markdown files with YAML frontmatter. No database server. No field size limits. Plain files you can read, grep, and version with git.

<p align="center">
  <img src="graph.png" alt="Graph view of a backlog managed with nd" width="835">
</p>

## Why nd Exists

We built [beads](https://github.com/steveyegge/beads) (`bd`) into the backbone of our AI-assisted development workflow. It worked. Persistent memory across compaction, dependency graphs, epic hierarchies. We love beads. But extremely fast development cycles, breaking changes, and the latest adoption of a new storage backend (Dolt SQL) was too much for us. It became the weakest link:

- **65KB TEXT field limit.** We routinely store 80-160KB in issue descriptions, design notes, and acceptance criteria. Dolt silently truncates at 65KB. Data loss you don't notice until you need it. We believe in agents that are single-use, so beads carry all context. This limitation was too much.
- **Running server required.** `dolt sql-server` must be running or bd falls back to embedded mode with different behavior. Configuration confusion is constant.
- **Migration headaches.** Schema migrations, repo fingerprint mismatches, shared server database confusion, JSONL import gaps. Every other session starts with `bd doctor --fix`.
- **Not inspectable.** Issues live in a binary database. You can't `cat` an issue, `grep` across your backlog, or diff changes in a PR review.

`nd` solves all of this by storing issues as plain markdown files in a directory. The storage layer is [vlt](https://github.com/RamXX/vlt), an Obsidian-compliant vault management library, used as an importable Go library. vlt handles file I/O, frontmatter parsing, search, file locking, and content patching. `nd` adds issue-tracker semantics on top.

## nd vs beads

| Capability | beads (`bd`) | nd |
|---|---|---|
| Storage | Dolt SQL (binary database) | Markdown files in a directory |
| Field size | 65KB limit (Dolt TEXT) | Unlimited (filesystem) |
| Server | `dolt sql-server` required | None |
| Inspectability | SQL queries only | `cat`, `grep`, `git diff` |
| Obsidian compatible | No | Yes -- wikilinks, frontmatter |
| Custom statuses | `status.custom` comma-separated config | Same, plus opt-in FSM enforcement |
| Status enforcement | None | Configurable: sequence, exit rules |
| Dependencies | Flat `depends_on` | Bidirectional blocks/blocked_by with history |
| Execution paths | None | Follows/led_to chains with auto-detection |
| History | None | Append-only history log per issue |
| Epics | Basic parent-child | Tree traversal, status rollup, close-eligible |
| Content integrity | None | SHA-256 content hashing |
| Import from beads | N/A | `nd import --from-beads` preserves IDs, timestamps, and infers execution trajectories |
| DAG visualization | None | `nd graph` terminal DAG, `nd path` execution chains |

Both tools use the same ID format (`PREFIX-HASH`, 4 base36 chars from SHA-256) for interoperability.

### What vlt Provides

[vlt](https://github.com/RamXX/vlt) (`github.com/RamXX/vlt`) is an Obsidian-compatible vault CLI and Go library. nd imports it directly for:

| Capability | vlt API | nd usage |
|---|---|---|
| Open vault | `vlt.Open(dir)` | Store initialization |
| Create file | `v.Create()` | Issue creation |
| Read file | `v.Read(title, heading)` | Issue reads, section-scoped reads |
| Write file | `v.Write()` | Body replacement |
| Append | `v.Append()` | Adding comments |
| Patch | `v.Patch(title, PatchOptions)` | Surgical heading-level edits |
| Frontmatter | `v.PropertySet()`, `v.PropertyRemove()` | Single-field updates |
| Search | `v.Search()`, `v.SearchWithContext()` | Full-text search |
| File listing | `v.Files(folder, ext)` | Issue enumeration |
| Locking | `vlt.LockVault(dir, exclusive)` | Acquired at Store.Open(), released at Store.Close() |
| Delete | `v.Delete()` | Soft delete to .trash/ |

nd adds: issue model with validation, collision-resistant ID generation, dependency graph computation (ready/blocked/cycles), execution path tracking (follows/led_to with auto-detection), append-only history logging, content hashing, epic tree traversal, colored CLI output, markdown rendering, configurable FSM enforcement, and custom status support.

## Installation

```bash
git clone https://github.com/RamXX/nd.git
cd nd
make build
make install    # Installs to ~/go/bin/nd
```

Requires Go 1.26+.

## Quick Start

```bash
# Initialize a vault in your project
nd init --prefix=PROJ

# Create issues
nd create "Implement user auth" --type=feature --priority=1 --assignee=alice
nd create "Fix login crash" --type=bug --priority=0 -d "App crashes on special chars"

# Find work
nd ready                    # Show actionable issues (no blockers)
nd list --status=open       # All open issues

# Work on something
nd update PROJ-a3f8 --status=in_progress
nd comments add PROJ-a3f8 "Root cause found: missing input sanitization"

# Manage dependencies
nd dep add PROJ-b7c2 PROJ-a3f8    # PROJ-b7c2 depends on PROJ-a3f8
nd blocked                         # See what's stuck

# Complete work
nd close PROJ-a3f8 --reason="Fixed with input validation"
nd ready                           # PROJ-b7c2 is now unblocked
```

## Custom Statuses

nd ships with 5 built-in statuses: `open`, `in_progress`, `blocked`, `deferred`, `closed`. You can extend these with project-specific statuses via configuration.

```bash
nd config set status.custom "review,qa"
nd config get status.custom
nd config list
```

Custom statuses work everywhere: `nd update`, `nd list --status`, `nd stats`, `nd doctor`.

## Status FSM (Workflow Enforcement)

nd includes an opt-in finite state machine that enforces status transitions. The engine is fully generic -- all rules come from configuration, nothing is hardcoded.

### Configuration

Three config keys control the FSM:

```bash
# 1. Define your pipeline (the ordered happy path)
nd config set status.sequence "open,in_progress,review,qa,closed"

# 2. Optionally restrict exits from specific statuses
nd config set status.exit_rules "blocked:open,in_progress"

# 3. Enable enforcement
nd config set status.fsm true
```

### Sequence Rules

When FSM is enabled, statuses that appear in the sequence follow two rules:

- **Forward**: must advance exactly one step (no skipping)
- **Backward**: can return to any earlier step (rework)

Statuses NOT in the sequence (like `rejected`) are unrestricted -- they can be entered from or exited to any state. This is the escape hatch for off-pipeline states.

### Exit Rules

`status.exit_rules` restricts where specific statuses can transition to. Format: `status:target1,target2;status:target1,target2`

When a status has an exit rule, it can ONLY go to the listed targets -- sequence rules do not apply to it.

### Example: Kanban with Review Gate

```bash
nd config set status.custom "review,qa,rejected"
nd config set status.sequence "open,in_progress,review,qa,closed"
nd config set status.exit_rules "blocked:open,in_progress;rejected:in_progress"
nd config set status.fsm true
```

This enforces:
- Work flows `open -> in_progress -> review -> qa -> closed` (one step at a time)
- Any step can go backward (e.g. `qa -> in_progress` for rework)
- `blocked` can only unblock to `open` or `in_progress`
- `rejected` can only go back to `in_progress`
- `blocked` and `rejected` can be entered from any non-closed state (off-sequence)
- `nd close` requires the issue to be at `qa` (the step before `closed`)
- `nd reopen` always works (goes to `open`)

### Example: Simple Three-Stage Pipeline

```bash
nd config set status.sequence "open,in_progress,closed"
nd config set status.fsm true
```

No custom statuses, no exit rules. Just enforces that work goes `open -> in_progress -> closed` without skipping steps.

### Without FSM

When `status.fsm` is `false` (the default), all transitions are allowed. Custom statuses still work for labeling -- you just don't get enforcement.

## CLI Output

nd uses colored output with the Ayu theme for terminal display. Status icons provide at-a-glance scannability:

```
○  open         Available to work
◐  in_progress  Active work (yellow)
●  blocked      Needs attention (red)
✓  closed       Completed (muted)
❄  deferred     Scheduled for later (muted)
```

Custom statuses display with `◇` and the status name.

List output uses a compact format with colored priority and type indicators:

```
○ PROJ-a3f8 [P1] [feature] @alice - Implement user auth
● PROJ-b7c2 [P0] [bug] @bob - Fix login crash
```

Color is automatically disabled for piped output (non-TTY). Respects `NO_COLOR`, `CLICOLOR=0`, and `CLICOLOR_FORCE` environment variables.

## ID Format

Issue IDs use a `PREFIX-HASH` format where the hash is 4 base36 characters (0-9, a-z) derived from SHA-256. This matches the beads (`bd`) ID format for interoperability.

Examples: `PROJ-a3f8`, `TM-uzg6`, `API-00k2`

Children use dot notation: `PROJ-a3f8.1`, `PROJ-a3f8.2`.

When importing from beads JSONL, original IDs are preserved verbatim.

## Storage Format

Issues live as markdown files in `<vault>/issues/`, where `<vault>` is resolved by `nd`:

- `--vault PATH` wins when provided
- otherwise `ND_VAULT_DIR` wins when set
- in normal repos, nd walks up from the current directory and uses the nearest `.vault/`
- in repos configured with `.vault/.nd-shared.yaml`, nd uses the repository's git common dir and resolves the live vault there instead of using a branch-local `.vault/`

That shared-worktree mode keeps the live backlog branch-independent across worktrees.

Example issue file:

```yaml
---
id: PROJ-a3f8
title: "Implement user authentication"
status: in_progress
priority: 1
type: feature
assignee: alice
labels: [security, milestone]
blocks: [PROJ-d9e1]
blocked_by: [PROJ-b3c0]
follows: [PROJ-c4d2]
led_to: [PROJ-e5f3]
created_at: 2026-02-23T20:15:00Z
created_by: alice
updated_at: 2026-02-24T10:30:00Z
content_hash: "sha256:a3f8c9d2..."
---

## Description
Implement OAuth 2.0 authentication with JWT tokens...

## Acceptance Criteria
- [ ] Login endpoint returns JWT
- [ ] Token refresh works
- [ ] Rate limiting on auth endpoints

## Design
Using bcrypt with 12 rounds per OWASP recommendation...

## Notes
Spike complete. Chose Authorization Code flow over Implicit.

## History
- 2026-02-23T20:15:00Z status: open -> in_progress
- 2026-02-23T20:15:00Z auto-follows: linked to predecessor PROJ-c4d2

## Links
- Blocks: [[PROJ-d9e1]]
- Blocked by: [[PROJ-b3c0]]
- Follows: [[PROJ-c4d2]]
- Led to: [[PROJ-e5f3]]

## Comments

### 2026-02-23T20:15:00Z alice
Started implementation. Base models done.
```

Every issue is a file you can read with `cat`, search with `grep`, and edit with any text editor. In tracked mode you can also diff issue changes with `git diff`. In the default local mode, use `nd archive` when you want a git-committable snapshot. No database required.

### Vault Layout

Typical local layout:

```
.vault/
  .nd.yaml            # Config: version, prefix, statuses, FSM rules (tracked when using --track-issues)
  issues/             # One .md file per issue (tracked when using --track-issues)
    PROJ-a3f8.md
    PROJ-b7c2.md
    PROJ-d9e1.md
  .trash/             # Soft-deleted issues
  .vlt.lock           # Advisory file lock
```

Repos using shared worktree state instead keep the live nd vault under the repo's git common dir and track a small resolver file in the worktree:

```yaml
# .vault/.nd-shared.yaml
mode: git_common_dir
path: shared/nd-vault
```

With that config, the live nd vault lives under:

```text
<git-common-dir>/
  <shared-nd-vault>/
    .nd.yaml
    issues/
    .trash/
    .vlt.lock
```

### Configuration File

`.nd.yaml` stores all vault-level settings:

```yaml
version: "1"
prefix: PROJ
created_by: alice
track_issues: true
status_custom: "review,qa,rejected"
status_sequence: "open,in_progress,review,qa,closed"
status_fsm: true
status_exit_rules: "blocked:open,in_progress;rejected:in_progress"
```

Manage it via `nd config set/get/list` or edit directly.

## Command Reference

### Initialization

```bash
nd init --prefix=PROJ [--vault=PATH] [--author=NAME] [--track-issues]
```

Creates the resolved vault directory structure and `.nd.yaml` config. Prefix is required -- it becomes part of every issue ID (e.g., `PROJ-a3f8`).

If you do not pass `--vault`, `nd init` uses the same vault resolution rules described above. In a repo with `.vault/.nd-shared.yaml`, that means initialization happens in the shared git-common-dir vault, not in a branch-local `.vault/`.

By default, `nd` ignores live issue files and `.nd.yaml`, which keeps the mutable tracker local and makes `nd archive` the git-friendly export path. Use `--track-issues` to keep `.nd.yaml` and `issues/` in git for repos that want markdown issues to be the tracked system of record.

### Configuration

```bash
nd config set <key> <value>   # Set a config value
nd config get <key>            # Get a config value
nd config list                 # List all config values
```

Available keys:

| Key | Description | Example |
|-----|-------------|---------|
| `status.custom` | Comma-separated custom statuses | `review,qa,rejected` |
| `status.sequence` | Ordered pipeline for FSM | `open,in_progress,review,qa,closed` |
| `status.fsm` | Enable/disable FSM enforcement | `true` / `false` |
| `status.exit_rules` | Restrict exits from statuses | `blocked:open,in_progress` |

Validation rules:
- Custom status names must be lowercase alphanumeric/underscore and not collide with built-ins
- Sequence statuses must be defined (built-in or custom), no duplicates
- Enabling FSM requires a non-empty sequence
- Exit rule statuses and targets must be valid (built-in or custom)

### Issue Creation

```bash
nd create "Title" [flags]
nd create --title="Title" [flags]
  --title            Issue title (alternative to positional argument)
  -t, --type         bug|feature|task|epic|chore|decision (default: task)
  -p, --priority     0-4, where 0=critical (default: 2)
  -d, --description  Issue description body
  --assignee         Assignee name
  --labels           Comma-separated labels
  --parent           Parent issue ID (for epic children)
  --body-file        Read description from file (- for stdin)
```

Title can be provided as a positional argument or via `--title`. Using both is an error.

### Quick Capture

```bash
nd q "Title" [flags]   # Alias for create with minimal flags
nd q --title="Title" [flags]
```

### Listing and Filtering

```bash
nd list [flags]
  -s, --status       Filter: open, in_progress, blocked, deferred, closed, all, or any custom status
  --type             Filter by issue type
  -a, --assignee     Filter by assignee
  -l, --label        Filter by label
  -p, --priority     Filter by priority (0-4 or P0-P4)
  --parent           Filter by parent issue ID
  --no-parent        Show only issues without a parent
  --sort             Sort by: priority (default), created, updated, id
  -r, --reverse      Reverse sort order
  -n, --limit        Max results (default: 50, 0 for unlimited)
  --all              Show all issues including closed
  --created-after    Filter by creation date
  --created-before   Filter by creation date
  --updated-after    Filter by update date
  --updated-before   Filter by update date
```

Default view shows non-closed issues sorted by priority.

### Viewing Issues

```bash
nd show <id> [--short] [--json]
```

`--short` gives a one-line summary. `--json` outputs the full issue as JSON. Default view renders the issue body as formatted markdown in the terminal.

### Updating Issues

```bash
nd update <id> [flags]
  --status          New status (built-in or custom)
  --title           New title
  --priority        New priority (0-4 or P0-P4)
  --assignee        New assignee
  --type            New type
  -d, --description New Description section content
  --body-file       Read Description section content from file (- for stdin)
  --append-notes    Append text to Notes section
  --parent          Set parent issue ID (empty to clear)
  --follows         Add follows link to predecessor issue
  --unfollow        Remove follows link from predecessor issue
  --set-labels      Replace all labels (comma-separated, empty to clear)
  --add-label       Add label(s)
  --remove-label    Remove label(s)
```

`--description` and `--body-file` update the `## Description` section only; they do not replace the full issue body.

When FSM is enabled, `--status` transitions are validated against the configured sequence and exit rules.

### Editing Issues

```bash
nd edit <id>   # Open issue in $EDITOR
```

Opens the issue file in your editor. After saving, nd refreshes the content hash and Links section.

### Closing and Reopening

```bash
nd close <id> [id...] [--reason="explanation"] [--suggest-next] [--start=<next-id>]
nd reopen <id>
```

Close accepts multiple IDs for batch operations. Closing sets `closed_at` and optionally `close_reason`, then **automatically removes the closed issue from all dependents' `blocked_by` lists** (cascading unblock). Reopening clears both and sets status to `open`.

`--start` transitions another issue to `in_progress` after closing, triggering auto-follows detection to link the execution chain.

When FSM is enabled, `nd close` requires the issue to be at the step immediately before `closed` in the sequence. `nd reopen` is always allowed.

`--suggest-next` shows the next ready issue after closing.

### Aliases

These hidden commands are available as shortcuts for common operations:

```bash
nd resolve <issue> <dep>    # Alias for: nd dep rm <issue> <dep>
nd unblock <issue> <dep>    # Alias for: nd dep rm <issue> <dep>
nd block <issue> <dep>      # Alias for: nd dep add <issue> <dep>
nd start <issue>            # Alias for: nd update <issue> --status=in_progress
```

These don't appear in `nd --help` but work when called directly.

### Deferring

```bash
nd defer <id> [--until=YYYY-MM-DD]
nd undefer <id>
```

Deferring sets status to `deferred` with an optional target date. `nd undefer`
restores to `open` by default, or to the first FSM-allowed deferred exit target
when `status.fsm` and `status.exit_rules` specify a different resume state.

### Dependencies

```bash
nd dep add <issue> <depends-on>    # issue depends on depends-on
nd dep rm <issue> <depends-on>     # Remove dependency
nd dep list <id>                   # Show all deps for an issue
nd dep relate <a> <b>              # Bidirectional related link
nd dep unrelate <a> <b>            # Remove related link
nd dep cycles                      # Detect dependency cycles
nd dep tree <id>                   # Show dependency tree
```

Dependencies are bidirectional: `nd dep add A B` adds B to A's `blocked_by` AND A to B's `blocks`. Removing moves the reference to `was_blocked_by` for historical tracking. All dependency changes are logged in the `## History` section of both issues.

### Finding Work

```bash
nd ready [flags]
nd blocked [--verbose]
nd stale [--days=N]
```

`ready` shows issues with no open blockers. It supports the same filter flags as `nd list` for scoping results:

```bash
nd ready                                    # All ready issues
nd ready --parent=PROJ-a1b2                 # Ready issues in a specific epic
nd ready --label=auth --assignee=alice      # Ready auth issues assigned to alice
nd ready --priority=0                       # Ready critical issues
nd ready --type=bug                         # Ready bugs
nd ready --no-parent                        # Ready issues without a parent
nd ready --created-after=2026-01-01         # Ready issues created this year
nd ready --sort=created --reverse -n 5      # 5 most recently created ready issues
```

`blocked` shows issues waiting on dependencies. `stale` shows issues not updated in N days (default: 14).

### Search

```bash
nd search <query>
```

Full-text search across all issue files. Returns matching lines with 2 lines of context. Delegates to vlt's search engine.

### Labels

```bash
nd labels add <id> <label>
nd labels rm <id> <label>
nd labels list                # All labels with counts
```

### Comments

```bash
nd comments add <id> "Comment text"
nd comments list <id>
```

Comments are appended to the `## Comments` section with RFC3339 timestamps and author attribution.

### Epics

```bash
nd epic status <id>        # Progress summary (open/closed/blocked counts, %)
nd epic tree <id>          # Hierarchical tree view with status markers
nd epic close-eligible     # List epics where all children are closed
nd children <id>           # List child issues of a parent
```

Epic children are found by matching the `parent` field. Tree view uses status markers: `[ ]` open, `[>]` in progress, `[!]` blocked, `[x]` closed.

### Statistics

```bash
nd stats [--json]
nd count [--status=STATUS]
```

`stats` shows aggregate counts by status (including custom statuses), type, and priority. `count` returns a single number for scripting.

### DAG Visualization

```bash
nd graph [--status=STATUS] [--all]
```

Renders the dependency graph as a terminal DAG with status-colored nodes and directed edges.

### Execution Path Visualization

```bash
nd path            # Show all execution chain roots
nd path <id>       # Show execution chain from a specific issue
```

Renders the execution path tree following `follows`/`led_to` edges. Shows the temporal order in which work was completed.

### AI Context

```bash
nd prime [--json]
```

Outputs a structured summary for AI context injection: total counts, ready work, blocked work, in-progress items. JSON mode includes all issues.

### Import from Beads

```bash
nd import --from-beads <path-to-jsonl>
nd import --from-beads <path-to-jsonl> --force  # Re-wire deps even if all issues exist
```

Three-pass import from beads JSONL: (1) creates all issues preserving original IDs, timestamps, statuses, labels, notes, and design content; (2) wires dependencies (parent-child inferred from dotted IDs and cross-references, blocks, related) and promotes parents to epics; (3) infers `follows`/`led_to` execution trajectories from `closed_at` timestamps -- sibling chains, related orphan chains, and epic-to-epic chains. After migration, `nd path` shows the full execution history. The import is idempotent: if Pass 1 finds that all issues already exist, passes 2 and 3 are skipped automatically. Use `--force` to re-run dependency wiring and trajectory inference on an already-imported vault.

### Vault Health

```bash
nd doctor [--fix]
```

Validates:
1. Content hash integrity (SHA-256 of body matches stored hash)
2. Bidirectional dependency consistency (A blocks B <-> B blocked_by A)
3. Reference validity (no deps pointing to nonexistent issues)
4. Field validation (required fields present, enums valid, custom statuses recognized)
5. Links section integrity (## Links present with correct wikilinks)

With `--fix`, automatically repairs hash mismatches, broken dependency references, missing Links sections, and History section content hash drift.

### Deleting Issues

```bash
nd delete <id> [--permanent]
```

Soft-deletes to `.trash/` by default. `--permanent` removes the file entirely. Cleans up dependency references and follows/led_to links on both sides.

### Global Flags

All commands support:

```
--vault PATH    Override vault directory explicitly
--json          Output as JSON
--verbose       Verbose output
--quiet         Suppress non-essential output
```

`ND_VAULT_DIR` provides the same override via environment variable. Without either override, nd auto-discovers the nearest local `.vault/`, except in repos with `.vault/.nd-shared.yaml` where it resolves the shared git-common-dir vault.

## Priority System

| Priority | Label | Use for |
|----------|-------|---------|
| P0 | Critical | Security, data loss, broken builds |
| P1 | High | Major features, important bugs |
| P2 | Medium | Standard work (default) |
| P3 | Low | Polish, optimization |
| P4 | Backlog | Future ideas |

## Status Lifecycle

### Built-in Statuses

```
open --> in_progress --> closed
  |          |
  v          v
deferred   blocked --> open
```

These 5 statuses are always available. Closed issues can only transition back to `open` via `nd reopen`.

### With Custom Statuses and FSM

When you configure custom statuses and enable the FSM, the lifecycle is defined by your sequence and exit rules. For example, a kanban pipeline:

```
open --> in_progress --> review --> qa --> closed
              |           |        |
              v           v        v
           blocked    (backward to any earlier step)
              |
              v
   open or in_progress (exit rule)
```

The FSM is entirely configuration-driven. No workflow is hardcoded.

## Issue Types

| Type | Use for |
|------|---------|
| `bug` | Defects and broken behavior |
| `feature` | New functionality |
| `task` | General work items |
| `epic` | Large initiatives with child issues |
| `chore` | Maintenance, tooling, housekeeping |
| `decision` | Architectural decision records |

## Architecture

```
nd (Go CLI, cobra)
  |
  cmd/               -- One file per command
  |
  internal/
    model/           -- Issue struct, Status/Priority/Type enums, validation
    idgen/           -- SHA-256 + base36 collision-resistant ID generation
    store/           -- Wraps vlt.Vault for issue CRUD, deps, filtering, FSM
    graph/           -- In-memory dependency graph: ready, blocked, cycles, epics, DAG, execution paths
    enforce/         -- Content hashing, validation rules
    format/          -- Table, detail, JSON, prime context output
    ui/              -- Terminal styling (Ayu theme), markdown rendering, TTY detection
```

**Dependencies**: `cobra`, `vlt`, `lipgloss`, `glamour`.

## Testing

```bash
make test     # Unit + integration tests
make vet      # Go vet
make build    # Build binary
```

Unit tests cover model validation, ID generation, content hashing, graph traversal (dependency trees and execution paths), config round-tripping, and FSM transition enforcement (forward steps, backward rework, exit rules, sequence skipping). Integration tests create real temp vaults and run full workflows with no mocks: init, create, dep, ready, close, follows/led_to linking, history logging, auto-follows detection, custom status transitions, and FSM enforcement.

## License

Apache License 2.0. See [LICENSE](LICENSE).
