# CLI Command Reference

**For:** AI agents and developers using the nd command-line interface
**Version:** 0.8.0+

## Quick Navigation

- [Initialization](#initialization)
- [Issue Management](#issue-management)
- [Finding Work](#finding-work)
- [Dependencies](#dependencies)
- [Execution Paths](#execution-paths)
- [Labels and Comments](#labels-and-comments)
- [Epics](#epics)
- [Visualization](#visualization)
- [Search and Stats](#search-and-stats)
- [Deferring Work](#deferring-work)
- [Configuration](#configuration)
- [AI Context and Health](#ai-context-and-health)
- [Migration](#migration)
- [Global Flags](#global-flags)

## Initialization

```bash
# Create a new nd vault
nd init                                  # Infers prefix from git remote or directory name
nd init --prefix=PROJ                    # Explicit prefix
nd init --prefix=PROJ --vault=/path      # Custom vault location
nd init --prefix=PROJ --author=alice     # Custom default author
```

When `--prefix` is omitted, nd infers it automatically:
1. Parse the git remote origin URL and extract the repo name (e.g., `my-project` -> `MP`)
2. Fallback: use the current directory basename (e.g., `tminus` -> `TMI`)

The inferred prefix is printed: `Inferred prefix: MP (from git remote "my-project")`

## Issue Management

### Create

```bash
# Basic creation (positional title)
nd create "Issue title" --type=task --priority=2

# Title via flag (useful for agents)
nd create --title="Issue title" --type=task --priority=2

# Full options
nd create "Title" \
  --type=bug|feature|task|epic|chore|decision \
  --priority=0-4 \
  --assignee=alice \
  --labels=auth,urgent \
  --description="Detailed description" \
  --parent=PROJ-a3f \
  --body-file=spec.md

# Short flags
nd create "Title" -t bug -p 1 -d "Description"

# Read description from stdin
echo "Long description" | nd create "Title" --body-file=-
```

Title can be provided as a positional argument or via `--title`. Using both is an error.

Output: `Created PROJ-a3f: Issue title`
With `--quiet`: just the ID
With `--json`: `{"id":"PROJ-a3f"}`

### Quick Capture (q)

```bash
# Create issue and output only the ID (for scripting)
nd q "Fix login bug"                              # Outputs: PROJ-a1b2
nd q --title="Fix login bug"                      # Same, via --title flag
nd q "Task" -t task -p 1                          # With type and priority
nd q "Bug" -t bug --labels=critical               # With labels

# Scripting examples
ISSUE=$(nd q "New feature")                       # Capture ID in variable
nd q "Task" | xargs nd show                       # Pipe to other commands
```

### Show

```bash
nd show PROJ-a3f              # Full detail view (rendered markdown)
nd show PROJ-a3f --short      # One-line summary
nd show PROJ-a3f --json       # JSON output
```

### Update

```bash
# Update fields (at least one required)
nd update PROJ-a3f --status=in_progress
nd update PROJ-a3f --priority=0 --assignee=bob
nd update PROJ-a3f --title="New title"
nd update PROJ-a3f --type=bug
nd update PROJ-a3f --append-notes="Found the root cause"
nd update PROJ-a3f -d "New description"
nd update PROJ-a3f --body-file=updated-spec.md

# Parent management
nd update PROJ-a3f --parent=PROJ-epic             # Set parent
nd update PROJ-a3f --parent=""                     # Clear parent

# Execution path management
nd update PROJ-a3f --follows=PROJ-b7c             # Add follows link (PROJ-a3f follows PROJ-b7c)
nd update PROJ-a3f --unfollow=PROJ-b7c            # Remove follows link

# Label management
nd update PROJ-a3f --set-labels=auth,urgent        # Replace all labels
nd update PROJ-a3f --add-label=security            # Add label(s)
nd update PROJ-a3f --remove-label=urgent           # Remove label(s)
nd update PROJ-a3f --set-labels=""                  # Clear all labels
```

When FSM is enabled, `--status` transitions are validated against the configured sequence and exit rules.

### Edit

```bash
nd edit PROJ-a3f       # Open issue file in $EDITOR ($VISUAL, defaults to vi)
```

After saving, nd refreshes the content hash and Links section automatically.

### Close and Reopen

```bash
# Close one or more issues
nd close PROJ-a3f                                 # Close single
nd close PROJ-a3f PROJ-b7c                        # Close multiple (batch)
nd close PROJ-a3f --reason="Implemented"          # With reason
nd close PROJ-a3f --suggest-next                  # Show next ready issue after closing
nd close PROJ-a3f --start=PROJ-b7c                # Close and start next issue (auto-links)

# Reopen a closed issue
nd reopen PROJ-a3f
```

**Auto-cascade on close**: When an issue is closed, nd automatically removes it from all dependents' `blocked_by` lists. This means you do NOT need to manually run `nd dep rm` after closing a blocker -- dependents are unblocked automatically. The historical relationship is preserved in `was_blocked_by`.

Output example:
```
Closed PROJ-a3f
  Unblocked PROJ-b7c (was blocked by PROJ-a3f)
  Unblocked PROJ-c8d (was blocked by PROJ-a3f)
```

When FSM is enabled, `nd close` requires the issue to be at the step immediately before `closed` in the sequence. `nd reopen` is always allowed.

`--start` transitions the specified issue to `in_progress`, triggering auto-follows detection which links the execution chain between the closed and started issues.

All state transitions are recorded in the issue's `## History` section with timestamps.

### Delete

```bash
nd delete PROJ-a3f                                # Soft delete (moves to .trash/)
nd delete PROJ-a3f --permanent                    # Permanent delete (no recovery)
nd delete PROJ-a3f --dry-run                      # Preview what would be deleted
nd delete PROJ-a3f PROJ-b7c                       # Delete multiple
```

Deleting cleans up all dependency references and follows/led_to links in other issues.

### List

```bash
# Default: non-closed issues sorted by priority
nd list

# Status filters
nd list --status=open                             # Single status
nd list --status=in_progress
nd list --status=all                              # All statuses
nd list --all                                     # Include closed

# Field filters
nd list --type=bug                                # Filter by type
nd list --assignee=alice                          # Filter by assignee
nd list --label=critical                          # Filter by label
nd list --priority=0                              # Filter by priority (0-4 or P0-P4)

# Hierarchy filters
nd list --parent=PROJ-a3f                         # Children of a specific parent
nd list --no-parent                               # Only issues without a parent

# Date filters (YYYY-MM-DD)
nd list --created-after=2026-02-01
nd list --created-before=2026-02-28
nd list --updated-after=2026-02-20
nd list --updated-before=2026-02-24

# Sorting and limits
nd list --sort=created                            # Sort: priority, created, updated, id
nd list --reverse                                 # Reverse sort order
nd list -n 10                                     # Limit results (0 = unlimited)
nd list --json                                    # JSON output

# Custom statuses (when configured)
nd list --status=review                           # Filter by custom status
```

## Finding Work

```bash
# Ready work (no blockers, not closed/deferred)
# nd ready supports ALL the same filter flags as nd list.
nd ready                                          # All ready issues
nd ready --parent=PROJ-a1b2                       # Ready issues in a specific epic
nd ready --assignee=alice                         # Filter by assignee
nd ready --label=auth                             # Filter by label
nd ready --priority=0                             # Filter by priority
nd ready --type=bug                               # Filter by type
nd ready --no-parent                              # Only parentless issues
nd ready --sort=created --reverse -n 5            # 5 most recently created
nd ready --created-after=2026-01-01               # Created this year

# Blocked work
nd blocked                                        # Show blocked issues
nd blocked --verbose                              # Include blocker details

# Stale issues (not updated recently)
nd stale                                          # Default: 30 days
nd stale --days=14                                # Custom threshold
```

## Dependencies

```bash
# Add dependency (A depends on B: B blocks A)
nd dep add PROJ-a3f PROJ-b7c

# Remove dependency
nd dep rm PROJ-a3f PROJ-b7c

# List all dependencies of an issue
nd dep list PROJ-a3f

# Related links (soft, bidirectional, no blocking effect)
nd dep relate PROJ-a3f PROJ-b7c                   # Add related link
nd dep unrelate PROJ-a3f PROJ-b7c                 # Remove related link

# Cycle detection
nd dep cycles                                     # Find circular dependencies

# Dependency tree
nd dep tree PROJ-a3f                              # Show dependency tree from issue
```

**Dependency semantics**: `nd dep add A B` means "A depends on B" (B must complete before A). This updates both files bidirectionally:
- Adds B to A's `blocked_by`
- Adds A to B's `blocks`

Removing a dependency cleans both sides and preserves history in `was_blocked_by`.

## Execution Paths

Execution paths track the temporal order in which issues were worked. Unlike dependencies (structural), execution paths capture the actual journey through the backlog.

```bash
# Add follows link (B was worked after A)
nd update PROJ-b7c --follows=PROJ-a3f

# Remove follows link
nd update PROJ-b7c --unfollow=PROJ-a3f

# View execution path from a specific issue
nd path PROJ-a3f                                  # Show chain from issue

# View all execution path roots
nd path                                           # All chain starting points
```

### Auto-Detection

When an issue transitions to `in_progress`, nd automatically detects predecessors:

1. **From was_blocked_by**: Closed issues that previously blocked this one
2. **From siblings**: Most recently closed sibling under the same parent epic

Auto-detected links appear in the `## History` section and in the `follows`/`led_to` frontmatter fields.

### Close-and-Start

The `--start` flag on `nd close` combines closing and starting in one operation:

```bash
nd close PROJ-a3f --start=PROJ-b7c
# Closes PROJ-a3f, starts PROJ-b7c, auto-links execution path
```

### History Log

Every state transition, dependency change, and auto-follow detection is recorded in the issue's `## History` section:

```
## History
- 2026-02-23T20:15:00Z status: open -> in_progress
- 2026-02-23T20:15:00Z auto-follows: linked to predecessor PROJ-a3f
- 2026-02-24T10:30:00Z dep_added: blocked_by PROJ-c4d
- 2026-02-24T15:00:00Z status: in_progress -> closed
```

Pre-existing issues without a `## History` section get one auto-created on the first write (self-healing).

## Labels and Comments

### Labels

```bash
nd labels add PROJ-a3f security                   # Add label
nd labels rm PROJ-a3f security                    # Remove label
nd labels list                                    # All labels with counts
```

### Comments

```bash
nd comments add PROJ-a3f "Comment text"           # Add timestamped comment
nd comments list PROJ-a3f                         # View comments
```

Comments are appended to the `## Comments` section in the issue file with RFC3339 timestamp and author.

## Epics

```bash
# Epic progress summary
nd epic status PROJ-a3f
# Output: Children count, open/in_progress/blocked/closed, progress %

# Epic tree view
nd epic tree PROJ-a3f
# Output: Hierarchical tree with status markers
#   [ ] open  [>] in_progress  [!] blocked  [x] closed  [-] deferred

# Find epics ready to close (all children closed)
nd epic close-eligible

# List children of a parent
nd children PROJ-a3f
```

## Visualization

```bash
# Terminal DAG of dependency graph
nd graph                                          # All root issues (no blockers)
nd graph PROJ-a3f                                 # Graph from specific issue

# Execution path tree (follows/led_to chains)
nd path                                           # All path roots (chain starting points)
nd path PROJ-a3f                                  # Execution chain from specific issue
```

`nd graph` renders the dependency graph (structural). `nd path` renders the execution path tree (temporal). Both accept an optional issue ID. Status icons: `[ ]` open, `[>]` in_progress, `[!]` blocked, `[-]` deferred, `[x]` closed.

## Search and Stats

```bash
# Full-text search across issues
nd search "authentication"                        # Returns matching lines with context

# Project statistics
nd stats                                          # Text summary by status, type, priority
nd stats --json                                   # JSON output

# Issue counts (for scripting)
nd count                                          # Default: by status
nd count --by=type                                # Group by: status, type, priority, assignee, label
nd count --status=open                            # Filter before counting
```

## Deferring Work

```bash
# Defer an issue (set status to deferred)
nd defer PROJ-a3f                                 # Defer indefinitely
nd defer PROJ-a3f --until=2026-03-01              # Defer until date

# Restore a deferred issue to its configured resume status
nd undefer PROJ-a3f
```

Deferred issues are excluded from `nd ready`.

## Configuration

```bash
# Manage vault-level settings
nd config list                                    # Show all config values
nd config get status.custom                       # Get specific value
nd config set status.custom "review,qa"           # Set custom statuses
```

### Available Config Keys

| Key | Description | Example |
|-----|-------------|---------|
| `status.custom` | Comma-separated custom statuses | `review,qa,rejected` |
| `status.sequence` | Ordered pipeline for FSM | `open,in_progress,review,qa,closed` |
| `status.fsm` | Enable/disable FSM enforcement | `true` / `false` |
| `status.exit_rules` | Restrict exits from statuses | `blocked:open,in_progress` |

### Custom Statuses

Define project-specific statuses beyond the 5 built-ins:

```bash
nd config set status.custom "review,qa"
```

Custom statuses work everywhere: `nd update --status=review`, `nd list --status=qa`, `nd stats`, `nd doctor`.

### FSM Enforcement (Opt-in)

Enable workflow enforcement with a configured sequence:

```bash
# Define the happy path
nd config set status.sequence "open,in_progress,review,qa,closed"

# Optionally restrict exits from specific statuses
nd config set status.exit_rules "blocked:open,in_progress"

# Enable enforcement
nd config set status.fsm true
```

When FSM is enabled:
- **Forward**: must advance exactly one step in the sequence (no skipping)
- **Backward**: can return to any earlier step (rework)
- **Off-sequence statuses** (like `blocked`): unrestricted entry/exit unless exit rules apply
- `nd close` requires the issue to be at the step immediately before `closed`
- `nd reopen` always works

## AI Context and Health

```bash
# AI context output
nd prime                                          # Structured summary for AI
nd prime --json                                   # Full project state as JSON

# Vault health check
nd doctor                                         # Validate integrity
nd doctor --fix                                   # Auto-fix problems
```

Doctor checks:
1. **HASH**: Content hash integrity (SHA-256 of body vs stored hash)
2. **DEP**: Bidirectional dependency consistency
3. **REF**: Reference validity (no orphan dep references)
4. **VALID**: Field validation (required fields, valid enums, custom statuses)
5. **LINKS**: Links section integrity (wikilinks match frontmatter relationships)

## Migration

```bash
# Import from beads JSONL (auto-initializes vault if needed)
nd migrate --from-beads .beads/issues.jsonl

# Re-run is idempotent (no-op if all issues exist)
nd migrate --from-beads .beads/issues.jsonl

# Force re-wire dependencies on an existing vault
nd migrate --from-beads .beads/issues.jsonl --force
```

If the vault is not initialized, `nd migrate` auto-initializes it before importing:
1. Peeks at the first JSONL line's `id` field to extract the prefix (e.g., `TM-a3f8` -> `TM`)
2. Falls back to git remote / directory name inference if no ID found
3. Prints: `Auto-initialized vault at .vault (prefix: TM, inferred from issue IDs)`

Three-pass import:
1. **Pass 1**: Creates all issues, preserving original IDs, timestamps, statuses (including custom), labels, notes, and design content
2. **Pass 2**: Wires dependencies (parent-child, blocks, related) and promotes parents to epics
3. **Pass 3**: Infers `follows`/`led_to` execution trajectories from `closed_at` timestamps -- sibling chains under shared parents, related orphan chains, and epic-to-epic chains

The import is idempotent: if Pass 1 imports zero new issues (all already exist), passes 2 and 3 are skipped and a message is printed. Use `--force` to run passes 2 and 3 regardless. After migration, `nd path` shows the full execution history. See `nd migrate --help` for details.

## Global Flags

All commands support these flags:

```bash
--version        # Print nd version and exit
--vault PATH     # Override vault directory (default: .vault, auto-discovered)
--json           # Output as JSON
--verbose        # Verbose output
--quiet          # Suppress non-essential output
```

Vault auto-discovery walks up the directory tree looking for `.vault/`.

## Priority System

| Value | Label | Use for |
|-------|-------|---------|
| 0 / P0 | Critical | Security, data loss, broken builds |
| 1 / P1 | High | Major features, important bugs |
| 2 / P2 | Medium | Standard work (default) |
| 3 / P3 | Low | Polish, optimization |
| 4 / P4 | Backlog | Future ideas |

## Status Values

### Built-in Statuses

- `open` -- Available to work on
- `in_progress` -- Currently being worked
- `blocked` -- Blocked by dependencies
- `deferred` -- Intentionally deferred (with optional target date)
- `closed` -- Completed

### Custom Statuses

Defined via `nd config set status.custom`. Custom statuses display with `diamond` icon and work in all commands.

## Aliases (Hidden Commands)

These commands don't appear in `nd --help` but work when called. They exist because agents frequently attempt these names:

```bash
nd resolve <issue> <dep>    # Alias for: nd dep rm <issue> <dep>
nd unblock <issue> <dep>    # Alias for: nd dep rm <issue> <dep>
nd block <issue> <dep>      # Alias for: nd dep add <issue> <dep>
nd start <issue>            # Alias for: nd update <issue> --status=in_progress
```

## Issue Types

`bug`, `feature`, `task`, `epic`, `chore`, `decision`
