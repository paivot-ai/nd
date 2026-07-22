# Pluggable Backend Design (Exploration)

Status: DESIGN. Nothing here is implemented yet except the local vlt store
and the git sync plane it builds on (docs/SYNC.md).

## Goal

`nd` with the vlt/markdown store is excellent for a solo developer plus n
agents. Teams already live in a system of record (SoR): Jira, Linear, or Tixx
(~/workspace/cibertrend/tixx, owned by us). nd must plug into those SoRs
without giving up what makes it work for agents: microsecond local reads, no
rate limits inside agent loops, offline operation, unlimited field sizes, and
survival across compaction.

## Core architectural decision: local-first sync, not pass-through

Two possible shapes:

1. **Pass-through adapter.** Every `nd` command becomes a remote API call.
2. **Local-first sync adapter.** The vault stays the working copy for agents;
   a sync engine reconciles it with the SoR bidirectionally.

Decision: **local-first sync**. Reasons:

- Agent loops issue hundreds of reads per session (`nd ready`, `nd show`,
  `nd prime`). Jira Cloud rate limits and 200-500ms latencies would dominate
  loop time; Linear likewise. Pass-through makes nd unusable exactly where it
  shines.
- Offline and partial-failure behavior: agents keep working when the SoR is
  down; sync catches up.
- nd stores 80-160KB design bodies. Jira text fields cap around 32KB. A
  pass-through cannot even represent nd's data; a sync adapter can project
  the team-visible subset and keep full fidelity locally.
- We already built the reconciliation machinery: the git sync engine's
  three-way, field-aware merge (base = last-synced state, local = vault,
  remote = SoR) is the same algorithm with a different transport.

### Two sync planes

```text
            plane 1: git (full fidelity)
  vault A  <------ nd/backlog branch ------>  vault B      (agents, machines)
     |
     | plane 2: SoR adapter (team projection)
     v
  Jira / Linear / Tixx                                     (humans, PMs, reporting)
```

- Plane 1 (exists today) moves the complete vault between machines and
  agents.
- Plane 2 projects the team-visible subset into the SoR and pulls team edits
  back. Fields the SoR cannot represent (follows/led_to execution paths,
  Design/Notes bodies beyond size limits, content_hash, History) remain
  vault-local and still propagate between agents via plane 1.

Field ownership resolves the dual-master question:

| Field class | Canonical |
|---|---|
| title, description, status, priority, assignee, labels, comments | SoR (team edits win on conflict) |
| dependencies | SoR when it has native links (all three do), else vault |
| follows/led_to, History, Design/Notes sections, content_hash | vault |
| epic/parent structure | SoR |

## Interfaces

Extraction step first: `cmd/` currently calls `*store.Store` directly. Define
`Tracker` as the interface the CLI consumes, with the vlt store as the
default implementation. Remote backends do NOT implement `Tracker`; they
implement `RemoteBackend`, consumed by the sync engine. This keeps agents on
the local store always.

```go
// backend/backend.go

// RemoteBackend is a system of record nd can sync with.
type RemoteBackend interface {
    Name() string                 // "jira", "linear", "tixx"
    Capabilities() Capabilities

    // Pull returns remote issues changed since the cursor, plus a new cursor.
    // Cursor semantics are backend-defined (updated-at watermark, event id).
    Pull(ctx context.Context, cursor string) ([]RemoteIssue, string, error)

    // Push applies a set of local changes. Implementations must be
    // idempotent (nd retries on partial failure).
    Push(ctx context.Context, changes []Change) ([]PushResult, error)

    // Transitions returns the legal status transitions for an issue, for
    // backends with gated workflows (Jira, Tixx). The sync engine path-finds
    // through them to reach a target status.
    Transitions(ctx context.Context, remoteID string) ([]Transition, error)
}

type Capabilities struct {
    Dependencies     bool // native issue links
    TypedLinks       bool // link vocabulary (blocks / relates / duplicates)
    Epics            bool // parent hierarchy
    NestedEpics      bool // hierarchy deeper than epic->story
    CustomFields     bool // spillover target for nd-native fields
    GatedWorkflow    bool // status must move via named transitions
    Comments         bool
    Webhooks         bool // push-based change feed available
    MaxBodyBytes     int  // 0 = unlimited
    PriorityLevels   int
}
```

Identity mapping lives in issue frontmatter, so it travels with plane 1:

```yaml
external: tixx:0198c9b2-...      # <backend>:<stable remote id>
external_key: TIX-482            # human display key
external_rev: "37"               # etag / version for optimistic concurrency
external_synced_at: 2026-07-22T14:00:00Z
```

The sync base (last-synced snapshot per issue) is stored under
`<vault>/.sync/<backend>/`, excluded from plane 1 snapshots (per-clone
state). Credentials never enter the vault: environment variables or
`~/.config/nd/credentials.yaml`, referenced by backend name.

### Sync algorithm (per issue)

Same three-way discipline as git sync:

1. base = `.sync/<backend>/<id>` (state at last sync)
2. local = vault issue, remote = SoR issue (mapped to the nd model)
3. Field-aware merge with the ownership table above deciding both-changed
   conflicts (SoR-owned fields: remote wins; vault-owned: local wins).
4. Write merged to vault; push SoR-owned deltas via `Push`; update base and
   cursor. Status pushes go through `Transitions` path-finding when
   `GatedWorkflow` is set; unreachable targets surface as sync warnings, not
   silent failures.

Deletes: SoR archive/delete moves the vault issue to `.trash/`; vault delete
archives remotely when the API allows, else labels it `nd-deleted`.

## Backend notes

### Tixx (first adapter)

We own both sides; it is also the cleanest agent surface of the three.

- API: JSON:API at `/api/v1` (AshJsonApi), API keys `tixx_pk_*` with typed
  scopes and dual identity (operator + agent), which maps directly onto
  `ND_AGENT` attribution. Dry-Run and Idempotency headers exist, both useful
  for the sync engine.
- IDs: UUID (stable, store in `external:`) plus Jira-style display key
  (`external_key`). Keys are server-assigned; nd IDs remain canonical in the
  vault.
- Dependencies: first-class typed directed links (`issue_links` +
  per-tenant `link_types` seeded from YAML). `blocks` maps 1:1 to nd
  blocked_by/blocks. Server-side cycle detection exists.
- Status: gated AshStateMachine (todo, in_progress, in_review,
  ready_for_deploy, in_production, done); the adapter must drive named
  transitions (`/issues/:id/transitions`), and the deploy edge requires a
  four-eyes approval an agent can request but never self-approve. nd custom
  statuses + FSM config mirror this well (`status.sequence` generated from
  the Tixx machine).
- Change feed: outbound webhooks (HMAC, retries, DLQ) for push; `updated_at`
  watermark polling as fallback. Cursor = event id.
- Gaps to design around: no `start_date`; per-tenant vocabularies
  (issue types, link types, labels) must be resolved or provisioned, not
  assumed; link create/delete does not currently emit `issue_events` (audit
  gap on Tixx side worth fixing while we are in there).
- Since we own Tixx: candidates to add for nd, in order of leverage: a
  knowledge/pages resource (see Knowledge axis), an `nd`-shaped bulk sync
  endpoint (batch upsert with idempotency), and first-class
  follows/led_to link types in the seeded vocabulary (then execution paths
  become team-visible instead of vault-local).

### Linear

- GraphQL API, webhooks, OAuth or API key.
- Native issue relations (blocks, related, duplicate) map cleanly.
- Priorities are 0-4 like nd (0 = none vs nd 0 = critical; needs an explicit
  mapping table, not passthrough).
- Epics: Linear projects or parent issues both work; recommend parent issues
  for the tree and projects for milestone epics.
- Knowledge: Linear Documents attach to projects; adequate for epic-level
  knowledge, thin for a general repository.

### Jira

- REST v3, ADF rich text (description/comments must be rendered from
  markdown), per-project workflow schemes make `GatedWorkflow` mandatory,
  issue links (`Blocks`) native, epics via parent.
- Text fields cap ~32KB: bodies get truncated projections with a marker
  linking back ("full context in nd"); full body stays vault-local, synced
  between agents on plane 1.
- Highest integration demand, highest friction; do it after the model is
  proven on Tixx and Linear.

## Knowledge repository axis

Knowledge (decisions, patterns, conventions) is a separate concern from issue
tracking and gets its own small interface, independently pluggable:

```go
type KnowledgeStore interface {
    Name() string
    PutNote(ctx context.Context, note Note) (remoteID string, err error)
    GetNote(ctx context.Context, remoteID string) (Note, error)
    List(ctx context.Context, since string) ([]NoteRef, string, error)
}
```

Vault notes (`knowledge/` in Paivot projects) already ride plane 1. The
KnowledgeStore mirrors them for team visibility:

| Tracker | Knowledge options | Recommendation |
|---|---|---|
| vlt (solo) | vault folders (today) | keep |
| Tixx | none today | build a Tixx knowledge component (we own it); ProseMirror docs + same tenancy/audit model; alternatively a shared Postgres schema owned by nd until it lands |
| Linear | Linear Documents | acceptable; Notion if the team already uses it |
| Jira | Confluence | Confluence pages, one space per project, ADF rendering shared with the Jira adapter |

Notion and Google Docs remain generic `KnowledgeStore` implementations if a
customer demands them; they are not on the critical path.

## Phasing

1. **Extract the `Tracker` interface** over `store.Store` and move `cmd/` to
   it. Pure refactor, no behavior change. This is also when the sync engine's
   merge functions move from `gitsync` into a shared `merge` package.
2. **Sync engine core**: `.sync/<backend>/` base store, cursor handling,
   field-ownership merge, `nd backend add/list/sync` CLI (`nd sync` runs
   plane 1 then plane 2).
3. **Tixx adapter** (owned, JSON:API, webhooks, dual identity). Add the Tixx
   knowledge component in parallel.
4. **Linear adapter** (clean API, native relations, validates the second
   implementation against the interface).
5. **Jira + Confluence adapter** (ADF rendering, workflow path-finding,
   body projection).

## Open questions

- Multi-backend per vault (Jira for tracking + Notion for knowledge is
  clearly needed; two trackers on one vault is not; propose: one
  RemoteBackend + one KnowledgeStore per vault).
- Whether plane 2 sync runs inside `nd sync` (simple, recommended) or as a
  daemon with webhook ingestion (later, for large teams).
- Mapping nd's 4-char hash IDs into SoR-side references: store the nd ID in
  a custom field / label on the remote side so links survive re-import.
