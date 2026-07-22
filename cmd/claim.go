package cmd

import (
	"fmt"
	"os"
	"os/user"

	"github.com/paivot-ai/nd/internal/format"
	"github.com/paivot-ai/nd/internal/store"
	"github.com/spf13/cobra"
)

var claimCmd = &cobra.Command{
	Use:   "claim <id>",
	Short: "Atomically claim an issue (assignee + in_progress)",
	Long: `Claim an issue for an agent: sets the assignee and moves it to in_progress
in one atomic operation under the vault lock. If another agent already claimed
the issue, the claim fails -- this is the race-free alternative to
"nd ready" followed by "nd update --status=in_progress" when multiple agents
pick work concurrently.

The agent name comes from --agent, then the ND_AGENT environment variable,
then the OS username.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agent, _ := cmd.Flags().GetString("agent")
		force, _ := cmd.Flags().GetBool("force")
		if agent == "" {
			agent = agentIdentity()
		}

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issue, err := s.ClaimIssue(args[0], agent, force)
		if err != nil {
			return err
		}
		if jsonOut {
			return format.JSONSingle(os.Stdout, issue)
		}
		if !quiet {
			fmt.Printf("Claimed %s for %s\n", issue.ID, agent)
		}
		return nil
	},
}

var releaseCmd = &cobra.Command{
	Use:   "release <id>",
	Short: "Release a claimed issue back to open",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agent, _ := cmd.Flags().GetString("agent")
		force, _ := cmd.Flags().GetBool("force")
		if agent == "" {
			agent = agentIdentity()
		}

		s, err := store.Open(resolveVaultDir())
		if err != nil {
			return err
		}
		defer s.Close()

		issue, err := s.ReleaseIssue(args[0], agent, force)
		if err != nil {
			return err
		}
		if jsonOut {
			return format.JSONSingle(os.Stdout, issue)
		}
		if !quiet {
			fmt.Printf("Released %s\n", issue.ID)
		}
		return nil
	},
}

// agentIdentity resolves the calling agent's name: ND_AGENT wins, then the
// OS user.
func agentIdentity() string {
	if v := os.Getenv("ND_AGENT"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}

func init() {
	claimCmd.Flags().String("agent", "", "agent name (default: ND_AGENT env, then OS user)")
	claimCmd.Flags().Bool("force", false, "steal the claim even if held by another agent or blocked")
	releaseCmd.Flags().String("agent", "", "agent name (default: ND_AGENT env, then OS user)")
	releaseCmd.Flags().Bool("force", false, "release a claim held by another agent")
	rootCmd.AddCommand(claimCmd)
	rootCmd.AddCommand(releaseCmd)
}
