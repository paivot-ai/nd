# nd Git Sync Architecture

How nd makes the backlog durable and consistent regardless of how many
agents, branches, and worktrees run in parallel.

## The problem this solves

Two failure modes existed before this design:

1. **Untracked backlog (default mode).** `.nd.yaml` and `issues/` were
   gitignored. The backlog lived only on local disk: `git clean -fdx`, a
   deleted checkout, or a re-clone destroyed it. `nd archive` was a manual,
   point-in-time export, not a durability mechanism.
2. **In-tree backlog (`--track-issues`).** Committing issue files to code
   branches gives every branch its own snapshot of the backlog. With n agents
   on n branches, the copies diverge immediately (every nd command rewrites
   `updated_at`), merges conflict constantly, and `git checkout` silently
   rolls issue state backward.

The root cause: the backlog is global, cross-branch state; code branches are
per-line-of-work state. Any design that stores the backlog IN code branches
couples the two and cannot survive parallelism.

## The design

Two invariants:

1. **One live vault per clone.** The live backlog is a single physical
   directory, resolved identically from every branch and worktree (nearest
   `.vault/`, or the git-common-dir shared vault in Paivot repos). Branch
   switches never touch it. Concurrency inside a clone is handled by the
   advisory vault lock (exclusive for writers, shared for readers), not by
   git.
2. **A dedicated backlog branch for durability and transport.** `nd sync`
   commits the vault to `nd/backlog` (configurable via `sync.branch`) using
   git plumbing: a throwaway index, `write-tree`, `commit-tree`,
   `update-ref`. The repository's working tree, index, and HEAD are never
   touched. Code branches never carry backlog files.

```text
   worktree A (feature/x) ---\
   worktree B (feature/y) ----> one live vault --> auto-snapshot --> nd/backlog --> origin
   worktree C (main)      ---/     (locked)         (every mutation)     (nd sync)
```

### Auto-snapshot

Every successful mutating command (`create`, `update`, `close`, `dep add`,
`claim`, ...) commits the vault to the local backlog branch, best effort. The
branch is therefore a continuous journal: a wiped vault loses at most the
single command that had not yet snapshotted. Disable with
`nd config set sync.auto off` or `ND_SYNC_AUTO=off`. Snapshots are
tree-deduplicated: an unchanged vault produces no commit.

### nd sync

`nd sync` = snapshot, fetch, reconcile, materialize, push:

- local ahead: push
- remote ahead: fast-forward the ref and materialize into the vault
- diverged: field-aware three-way merge (below), commit with both parents,
  materialize, push; on push race, refetch and re-merge (bounded retries)
- no remote: local-only (the branch is still the wipe-recovery point)

`nd sync --restore` materializes the branch into an empty or wiped vault,
fetching from the remote when the local branch is absent. This is the
recovery path for `git clean`, fresh clones, and second machines.

### Merge semantics (per issue file)

Issue files are structured, so nd merges them semantically instead of
textually:

| Field / section | Rule |
|---|---|
| scalar frontmatter (status, priority, assignee, ...) | changed on one side: take it; changed on both: latest `updated_at` wins, deterministic content tiebreak |
| list frontmatter (blocked_by, blocks, labels, related, follows, led_to, was_blocked_by) | three-way set merge: removals honored, additions unioned |
| `updated_at` | max of both sides |
| `closed_at` / `close_reason` | travel with the winning status |
| History | append-only union, timestamp ordered |
| Comments | append-only union of comment blocks, timestamp ordered |
| Notes | three-way line merge (deletions honored, additions unioned) |
| Description / Acceptance Criteria / Design | one side changed: take it; both changed: latest `updated_at` wins, resolution recorded in History as `sync-merge:` |
| Links | regenerated from merged frontmatter |
| `content_hash` | recomputed from the merged body |

File-level: delete vs modify keeps the modification; bodies with duplicated
canonical headings fall back to whole-file latest-wins with a note. Non-issue
files changed on both sides keep local with a warning. After a merge with
notes, run `nd doctor --fix` to reconcile any cross-issue bidirectional edges
affected by delete/modify races.

Why last-writer-wins is acceptable here: concurrent scalar conflicts on the
same issue are already a workflow anomaly (claims exist precisely so two
agents do not work one story). The merge exists so that anomalies degrade
gracefully and nothing is ever silently lost: History records every
transition from both sides, and the losing side of a Description conflict is
recoverable from the branch history.

### Safety guards

- **Mass-delete guard.** A snapshot that would drop every issue while the
  branch tip has issues is refused (a wiped vault looks exactly like this).
  `nd sync --restore` recovers; `nd sync --force` overrides intentionally.
- **Materialize never hard-deletes.** Issues pruned by a merge or restore go
  to `.trash/`.
- **Pre-merge commit.** Sync snapshots before merging, so the pre-merge local
  state is always in the branch history.
- **Atomic writes.** Vault materialization uses temp file + fsync + rename,
  matching vlt; ref updates use `update-ref` with expected-old-value.

### What is in a snapshot

Everything in the vault except runtime state: `.trash/`, `.guard/`,
`.vlt.lock`, loop/dispatcher state JSON, local archives, and vlt temp files
are excluded. Knowledge notes stored in the vault are included.

### Concurrency model within a clone

- Writers (`nd create`, `nd update`, `nd claim`, ...) take the exclusive
  vault lock (flock, auto-released on process death, 10s timeout via
  `VLT_LOCK_TIMEOUT`).
- Readers (`nd list`, `nd ready`, `nd show`, ...) take a shared lock and run
  concurrently; vlt's atomic renames guarantee they never see torn files.
- `nd claim` performs the read-check-write of claiming atomically under the
  exclusive lock, closing the `nd ready` then `nd update` race between
  agents.

### Modes

| Mode | Backlog location | Git durability | Use |
|---|---|---|---|
| default | `.vault/` (gitignored) or shared git-common-dir vault | `nd/backlog` branch + auto-snapshots | everything |
| `track_issues: true` | issue files on code branches | code branch commits | legacy single-agent repos; not recommended with parallel agents |

Switching modes with `nd config set track_issues true|false` reconciles the
vault `.gitignore` in place.
