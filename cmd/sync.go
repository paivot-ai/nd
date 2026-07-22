package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/paivot-ai/nd/internal/gitsync"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the backlog with the git backlog branch",
	Long: `Persist the vault to the backlog branch (default nd/backlog), reconcile
with the remote, and push.

The backlog never lives on code branches: the live vault is one physical
directory shared by every branch and worktree, and this command commits it to
a dedicated branch for durability and cross-clone sync. Divergent histories
are merged field-by-field per issue (status by latest update, dependency and
label lists as set unions, history and comments as append unions).

  nd sync             snapshot, pull/merge, push
  nd sync --no-push   snapshot and pull/merge only
  nd sync --status    show sync position, change nothing
  nd sync --restore   rebuild a wiped or freshly cloned vault from the branch`,
	RunE: func(cmd *cobra.Command, args []string) error {
		statusOnly, _ := cmd.Flags().GetBool("status")
		restore, _ := cmd.Flags().GetBool("restore")
		noPush, _ := cmd.Flags().GetBool("no-push")
		force, _ := cmd.Flags().GetBool("force")

		dir := resolveVaultDir()

		if restore {
			return runRestore(dir)
		}

		s, err := store.Open(dir)
		if err != nil {
			return err
		}
		defer s.Close()

		opts := gitsync.SyncOptions{
			Branch: s.SyncBranch(),
			Remote: s.SyncRemote(),
			NoPush: noPush,
			Force:  force,
		}

		if statusOnly {
			st, err := gitsync.Status(dir, opts)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(st)
			}
			printSyncStatus(st)
			return nil
		}

		res, err := gitsync.Sync(dir, opts)
		if err != nil {
			return err
		}
		if jsonOut {
			return printJSON(res)
		}
		printSyncResult(res)
		return nil
	},
}

func runRestore(dir string) error {
	// Restore intentionally does not require an initialized vault: its whole
	// point is recovering one. Lock only when the directory already exists.
	if _, err := os.Stat(dir); err == nil {
		s, err := store.Open(dir)
		if err == nil {
			defer s.Close()
			commit, rerr := gitsync.Restore(dir, gitsync.SyncOptions{Branch: s.SyncBranch(), Remote: s.SyncRemote()})
			if rerr != nil {
				return rerr
			}
			if !quiet {
				fmt.Printf("Restored vault %s from %s (%s)\n", dir, s.SyncBranch(), commit[:8])
			}
			return nil
		}
		// Vault dir exists but is not an initialized vault (no .nd.yaml):
		// fall through to the unlocked restore below.
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	commit, err := gitsync.Restore(dir, gitsync.SyncOptions{})
	if err != nil {
		return err
	}
	if !quiet {
		fmt.Printf("Restored vault %s from %s (%s)\n", dir, gitsync.DefaultBranch, commit[:8])
	}
	return nil
}

func printSyncStatus(st *gitsync.StatusResult) {
	fmt.Printf("Branch:  %s\n", st.Branch)
	if !st.BranchExists {
		fmt.Println("State:   no backlog branch yet (run `nd sync` to create it)")
		return
	}
	fmt.Printf("Commit:  %s\n", st.Commit[:8])
	if st.Dirty {
		fmt.Println("Vault:   has changes not yet snapshotted")
	} else {
		fmt.Println("Vault:   in sync with branch tip")
	}
	if st.RemoteAbsent {
		fmt.Println("Remote:  none configured (local-only durability)")
		return
	}
	fmt.Printf("Remote:  %d ahead, %d behind\n", st.Ahead, st.Behind)
}

func printSyncResult(res *gitsync.SyncResult) {
	if quiet {
		return
	}
	var parts []string
	if res.Snapshotted {
		parts = append(parts, "snapshotted")
	}
	if res.Merged {
		parts = append(parts, "merged")
	} else if res.Pulled {
		parts = append(parts, "pulled")
	}
	if res.Pushed {
		parts = append(parts, "pushed")
	}
	if len(parts) == 0 {
		parts = append(parts, "up to date")
	}
	suffix := ""
	if res.RemoteAbsent {
		suffix = " (no remote; local branch only)"
	}
	fmt.Printf("Sync %s: %s (%s)%s\n", res.Branch, strings.Join(parts, ", "), res.Commit[:8], suffix)
	for _, n := range res.Notes {
		errorf("merge note: %s", n)
	}
	if len(res.Notes) > 0 {
		errorf("run `nd doctor --fix` to verify cross-issue consistency after this merge")
	}
}

// autoSnapshot records a best-effort backlog snapshot after a mutating
// command. Failures are warnings: the user's command already succeeded, and
// the next mutation will retry the snapshot.
func autoSnapshot(cmdPath string) {
	if os.Getenv("ND_SYNC_AUTO") != "" {
		switch strings.ToLower(os.Getenv("ND_SYNC_AUTO")) {
		case "off", "false", "0", "no":
			return
		}
	}

	dir := resolveVaultDirPath()
	if _, err := os.Stat(filepath.Join(dir, ".nd.yaml")); err != nil {
		return // no vault; nothing to snapshot
	}

	s, err := store.Open(dir)
	if err != nil {
		return // vault busy; next mutation snapshots
	}
	defer s.Close()
	if !s.SyncAutoEnabled() {
		return
	}
	branch := s.SyncBranch()

	if _, err := gitsync.DiscoverRepo(dir); err != nil {
		return // standalone vault outside git; sync not applicable
	}

	msg := "nd: " + strings.TrimPrefix(cmdPath, "nd ")
	if _, err := gitsync.Snapshot(dir, branch, msg, false); err != nil && !quiet {
		errorf("auto-snapshot failed: %v", err)
	}
}

func init() {
	syncCmd.Flags().Bool("status", false, "show sync status without changing anything")
	syncCmd.Flags().Bool("restore", false, "materialize the backlog branch into the vault")
	syncCmd.Flags().Bool("no-push", false, "do not push to the remote")
	syncCmd.Flags().Bool("force", false, "bypass the mass-delete safety guard")
	rootCmd.AddCommand(syncCmd)
}
