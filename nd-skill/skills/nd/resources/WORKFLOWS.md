# Workflows and Checklists

Step-by-step workflows for common nd usage patterns.

## Contents

- [Session Start](#session-start)
- [Compaction Survival](#compaction-survival)
- [Session Handoff](#session-handoff)
- [Unblocking Work](#unblocking-work)
- [Execution Path Tracking](#execution-path-tracking)
- [Integration with TodoWrite](#integration-with-todowrite)

## Session Start {#session-start}

**nd is available when** the project has a `.vault/` directory with `.nd.yaml`.

**Automatic checklist at session start:**

```
Session Start (when nd is available):
- [ ] Run nd ready
- [ ] Report: "X items ready to work on: [summary]"
- [ ] If none ready, check nd blocked
- [ ] Suggest next action based on findings
```

**Pattern**: Always run `nd ready` when starting work. Report status immediately to establish shared context.

## Compaction Survival {#compaction-survival}

**Critical**: After compaction, conversation history is gone but nd state persists. Issues are your only memory.

**Post-compaction recovery checklist:**

```
After Compaction:
- [ ] Run nd prime (auto-loaded by hooks)
- [ ] Run nd list --status=in_progress to see active work
- [ ] Run nd show <id> for each in_progress issue
- [ ] Read Notes and Comments sections to understand:
      COMPLETED, IN PROGRESS, BLOCKERS, KEY DECISIONS
- [ ] Check dependencies: nd dep list <id> for context
- [ ] Reconstruct TodoWrite list from notes if needed
```

**Writing notes for compaction survival:**

Good note (enables recovery):
```bash
nd update PROJ-a3f --append-notes "COMPLETED: JWT token generation with 1hr expiry,
refresh token endpoint using rotating tokens. IN PROGRESS: Password reset flow.
NEXT: Add rate limiting to reset endpoint. KEY DECISION: bcrypt 12 rounds per OWASP."
```

Bad note (insufficient for recovery):
```bash
nd update PROJ-a3f --append-notes "Working on auth. Made progress."
```

The good note contains specific accomplishments, current state, concrete next step, and key decisions with rationale.

## Session Handoff {#session-handoff}

### At Session End

```
Session End:
- [ ] Notice work reaching stopping point
- [ ] Update notes with current state:
      nd update <id> --append-notes "COMPLETED: X. IN PROGRESS: Y. NEXT: Z"
- [ ] Ensure all code changes are committed and pushed
- [ ] nd sync -- persist the backlog branch to the remote (issue files are
      NOT on code branches; auto-snapshots cover local durability, nd sync
      covers the remote)
```

### At Session Start (resuming)

```
Session Resume:
- [ ] nd prime (auto-loaded) gives overview
- [ ] nd list --status=in_progress shows active work
- [ ] nd show <id> gives full context including notes
- [ ] Resume where you left off
```

**Pattern**: Update notes at logical stopping points. Notes field is the "read me first" guide for resuming.

### Notes Format

Write notes for someone with zero conversation context:
- Current state only (what's done, what's in progress)
- Specific accomplishments (not vague progress)
- Concrete next step (not "continue working")
- Blockers and key decisions if relevant

## Unblocking Work {#unblocking-work}

**When ready list is empty:**

```
Unblocking Workflow:
- [ ] nd blocked to see what's stuck
- [ ] nd dep list <id> on each blocked issue to identify blockers
- [ ] Choose: work on blocker, or reassess dependency
- [ ] If incorrect dependency: nd dep rm <issue> <dep>
- [ ] If real blocker: work on it, nd close it
- [ ] Closing a blocker automatically unblocks dependent issues
- [ ] nd ready to verify newly unblocked work
```

**Pattern**: nd automatically maintains ready state. Closing a blocker makes blocked work ready. No manual status updates needed.

## Execution Path Tracking {#execution-path-tracking}

Execution paths record the temporal order of work -- which issue was worked after which. This creates connected chains in Obsidian's graph view instead of disconnected islands.

**Automatic tracking (recommended)**:

Most execution path links are auto-detected. The typical workflow:

```
Execution Path Workflow:
- [ ] nd dep add B A                              # B depends on A
- [ ] Work on A, then nd close A
- [ ] nd dep rm B A                               # Resolve dep (archives to was_blocked_by)
- [ ] nd update B --status=in_progress            # Auto-follows detects A as predecessor
- [ ] nd path A                                   # View: A -> B chain
```

**Close-and-start shortcut**:

```bash
nd close PROJ-a3f --start=PROJ-b7c               # Close A, start B, auto-link
```

**Manual linking**:

```bash
nd update PROJ-b7c --follows=PROJ-a3f            # B follows A
nd update PROJ-b7c --unfollow=PROJ-a3f           # Remove link
```

**Viewing execution paths**:

```bash
nd path                                           # All chain starting points
nd path PROJ-a3f                                  # Chain from specific issue
```

**Pattern**: Let auto-detection handle most links. Use `--follows` for manual corrections or cross-epic linking.

## Integration with TodoWrite

**Using both tools in one session:**

```
Hybrid Workflow:
- [ ] nd ready to find high-level work
- [ ] nd show <id> for full context
- [ ] nd update <id> --status=in_progress to claim
- [ ] Create TodoWrite from acceptance criteria for execution steps
- [ ] Work through TodoWrite items
- [ ] Update nd notes as you learn
- [ ] When TodoWrite complete, nd close <id>
```

**Why hybrid**: nd provides persistent structure across sessions. TodoWrite provides visible within-session progress tracking. Use nd for the "what" and TodoWrite for the "how."
