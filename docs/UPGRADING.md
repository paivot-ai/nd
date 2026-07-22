# Upgrading nd

## One-command upgrade

```bash
nd upgrade                    # latest release: binary + skills for all agent hosts
nd upgrade --check            # report installed vs latest, change nothing
nd upgrade --version v0.11.0  # pin a release
nd upgrade --binary-only      # skip the skill refresh
nd upgrade --skills-only      # refresh skills without touching the binary
```

`nd upgrade` downloads the release from GitHub, verifies the published
SHA-256 checksum, validates that the downloaded binary reports the requested
version, atomically replaces the running executable, and then refreshes the
nd plugin (skills, hooks, guard) in every detected agent host through that
host's own plugin manager: Claude Code (`claude plugin update nd@nd`, all
scopes) and Codex when an nd plugin is installed there. Host plugin caches
are never edited directly.

The command is named `upgrade` because `nd update` mutates issues.

Order matters and is built in: the binary is replaced before skills are
refreshed. Updated skills reference commands (`nd sync`, `nd claim`,
`nd upgrade`) that the old binary's guard hook rejects, so refreshing skills
against an old binary would break agents mid-session.

## Compatibility with existing vaults (0.10.x to sync-era)

### Data format: fully backward compatible

- Issue files are unchanged: no new frontmatter fields, no body structure
  changes. Existing vaults open as-is; nothing is rewritten on upgrade.
- `.nd.yaml` gains three OPTIONAL keys (`sync_branch`, `sync_remote`,
  `sync_auto`). Old configs load with defaults; old binaries ignore the new
  keys. Caveat: an old binary running `nd config set` rewrites `.nd.yaml`
  without the sync keys, silently resetting any custom `sync.branch` or
  `sync.auto off` to defaults. Harmless unless you customized them; re-set
  after retiring the old binary.
- IDs, statuses, FSM config, archive format, and issue JSON output are
  unchanged. `nd config list` prints additional rows (`track_issues`,
  `sync.*`); scripts parsing that table by exact row set need updating.

### Behavior changes to expect (not breakage, but visible)

1. **A local `nd/backlog` branch appears** in the repo after the first
   mutating command: auto-snapshot is on by default. It is never pushed
   automatically; only `nd sync` pushes. If you use `git push --all`, be
   aware the backlog branch will be published. Opt out with
   `nd config set sync.auto off` or `ND_SYNC_AUTO=off`.
2. **A pre-existing branch named `nd/backlog`** is never taken over: nd
   refuses with a foreign-branch error. Point `sync.branch` at another name
   or force deliberately.
3. **Mutating commands pay a snapshot cost** (typically tens of
   milliseconds; the first snapshot of a very large vault is the biggest
   single hit). Read commands are unaffected and now run concurrently under
   shared locks.
4. **Vault `.gitignore` is reconciled, not append-only.** The default-mode
   comment line is updated, and, for `track_issues: true` vaults, the stale
   `.nd.yaml` and `issues/` ignore lines from the old bug are now removed:
   `git status` in those repos starts showing vault files, which is the
   tracked mode finally working as documented.
5. **Old binary + new skills is the one unsupported combination.** The old
   guard hook blocks `nd sync`, `nd claim`, `nd release`, `nd upgrade`, and
   `--epic`. Upgrade the binary first (or just run `nd upgrade`, which
   sequences this correctly). New binary + old skills is fine; agents simply
   do not know about the new commands yet.
6. **Mixed binary versions on one vault interoperate.** Locking composes
   through flock (old readers take exclusive locks, new readers shared), and
   the file format is identical. Only the config-set caveat above applies.

### Rollout for teams

1. Everyone runs `nd upgrade` (or the Paivot installer, which converges nd).
2. In each repo, run `nd sync` once to create and publish the backlog
   branch; teammates then run `nd sync` (or `nd sync --restore` on machines
   without a local vault) to converge.
3. Nothing else migrates: no vault rewrite, no `doctor --fix` required.
