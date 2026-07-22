package cmd

import (
	"github.com/paivot-ai/nd/internal/selfupdate"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade nd and refresh agent skills in one command",
	Long: `Download the requested nd release from GitHub, verify its published SHA-256
checksum, validate the binary's reported version, atomically replace this
executable, then refresh the nd plugin (skills, hooks, guard) in every
detected agent host through its own plugin manager: Claude Code
(claude plugin update nd@nd, all scopes) and Codex when an nd plugin is
installed there. Host-owned plugin caches are never edited directly.

Named "upgrade" because "nd update" mutates issues.

  nd upgrade                    # latest release, binary + skills
  nd upgrade --check            # report installed vs latest, change nothing
  nd upgrade --version v0.11.0  # pin a specific release
  nd upgrade --binary-only      # skip the plugin/skill refresh
  nd upgrade --skills-only      # refresh plugins without touching the binary`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		versionFlag, _ := cmd.Flags().GetString("version")
		repo, _ := cmd.Flags().GetString("repo")
		installDir, _ := cmd.Flags().GetString("install-dir")
		check, _ := cmd.Flags().GetBool("check")
		binaryOnly, _ := cmd.Flags().GetBool("binary-only")
		skillsOnly, _ := cmd.Flags().GetBool("skills-only")

		_, err := selfupdate.Run(selfupdate.Options{
			Version:        versionFlag,
			Repo:           repo,
			InstallDir:     installDir,
			CurrentVersion: version,
			Check:          check,
			BinaryOnly:     binaryOnly,
			SkillsOnly:     skillsOnly,
			Out:            cmd.OutOrStdout(),
		})
		return err
	},
}

func init() {
	upgradeCmd.Flags().String("version", "latest", "release tag to install, or latest")
	upgradeCmd.Flags().String("repo", "", "source repo owner/name (default paivot-ai/nd)")
	upgradeCmd.Flags().String("install-dir", "", "binary destination directory (default: replace the running executable)")
	upgradeCmd.Flags().Bool("check", false, "report installed vs latest version and exit")
	upgradeCmd.Flags().Bool("binary-only", false, "update the binary but skip the agent skill refresh")
	upgradeCmd.Flags().Bool("skills-only", false, "refresh agent skills without replacing the binary")
	rootCmd.AddCommand(upgradeCmd)
}
