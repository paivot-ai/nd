package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// hookInput is the JSON structure Claude Code sends to PreToolUse hooks via stdin.
type hookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type bashInput struct {
	Command string `json:"command"`
}

// knownCorrections maps hallucinated subcommands to their correct equivalents.
var knownCorrections = map[string]string{
	"status":   "stats (or nd epic status <id>)",
	"version":  "--version flag (nd --version)",
	"find":     "search",
	"get":      "show",
	"set":      "update (or nd config set)",
	"add":      "create",
	"remove":   "delete",
	"info":     "show",
	"describe": "show",
	"ls":       "list",
	"rm":       "delete",
	"mv":       "update --title",
	"log":      "path (execution log) or stats",
	"history":  "path (execution log) or show <id>",
	"inspect":  "show",
	"view":     "show",
}

// invalidFlags are flags that do not exist on most nd commands.
// --format and --output are intentionally excluded because nd archive uses them.
var invalidFlags = map[string]string{
	"--compact":   "nd does not have a --compact flag. nd list already shows compact output.",
	"--wide":      "nd does not have a --wide flag. Use nd show <id> for full detail.",
	"--color":     "nd does not have a --color flag.",
	"--no-header": "nd does not have a --no-header flag.",
}

// invalidFlagsForNonArchive are flags only valid on nd archive.
// If the nd command segment does NOT contain "archive", these are invalid.
var invalidFlagsForNonArchive = map[string]string{
	"--format": "Only nd archive supports --format. For other commands, use --json for JSON output.",
	"--output": "Only nd archive supports --output. For other commands, use --json for JSON output.",
}

// ndCmdPattern matches `nd <subcommand>` in command position:
// bare command, after pipes/&&/;/$(), or after another command like `pvg nd`.
// Requires whitespace or start-of-string before "nd" to avoid matching
// "nd" inside paths (e.g., /workspace/nd) or words (find, and, send).
var ndCmdPattern = regexp.MustCompile(`(?:^|[\s;|&(])nd\s+(\S+)`)

// pvgNDPattern matches `pvg nd <subcommand>` -- these are pvg subcommands
// routed through pvg's own command tree, not bare nd commands.
var pvgNDPattern = regexp.MustCompile(`pvg\s+nd\s+(\S+)`)

// ndFlagPattern matches `nd ... --flag` patterns
var ndFlagPattern = regexp.MustCompile(`nd\s+[^|;&]*?(--\S+)`)

var guardCmd = &cobra.Command{
	Use:    "guard",
	Short:  "Validate nd commands in Claude Code PreToolUse hooks",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var input hookInput
		if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
			// If we can't parse the input, allow the command (fail open).
			return nil
		}

		if input.ToolName != "Bash" {
			return nil
		}

		var bash bashInput
		if err := json.Unmarshal(input.ToolInput, &bash); err != nil {
			return nil
		}

		if bash.Command == "" {
			return nil
		}

		// Only validate commands that contain "nd " to avoid false positives.
		if !strings.Contains(bash.Command, "nd ") {
			return nil
		}

		// Build the set of valid subcommands from the cobra command tree.
		validCmds := buildValidCommandSet(rootCmd)

		// Collect pvg nd subcommands -- these are routed by pvg, not bare nd,
		// and may include pvg-specific subcommands (root, stats, etc.).
		pvgSubcmds := make(map[string]bool)
		for _, pm := range pvgNDPattern.FindAllStringSubmatch(bash.Command, -1) {
			pvgSubcmds[pm[1]] = true
		}

		// Find all nd subcommands in the bash command.
		matches := ndCmdPattern.FindAllStringSubmatch(bash.Command, -1)

		seen := make(map[string]bool)
		for _, match := range matches {
			subcmd := strings.TrimLeft(match[1], "-")
			if subcmd == "" || strings.HasPrefix(match[1], "-") {
				// It's a flag (like nd --version), not a subcommand. Skip.
				continue
			}
			if seen[subcmd] {
				continue
			}
			seen[subcmd] = true

			// Skip subcommands that are part of a `pvg nd` invocation --
			// pvg routes its own subcommands (root, stats, etc.).
			if pvgSubcmds[subcmd] {
				continue
			}

			if !validCmds[subcmd] {
				suggestion := ""
				if correct, ok := knownCorrections[subcmd]; ok {
					suggestion = fmt.Sprintf(" Did you mean: nd %s", correct)
				}
				fmt.Fprintf(os.Stderr, "nd guard: unknown command \"nd %s\".%s\nRun \"nd <command> --help\" for valid usage. Consult the nd skill's CLI_REFERENCE.md for the full command reference.\n", subcmd, suggestion)
				os.Exit(2)
			}
		}

		// Check for hallucinated flags in nd command segments.
		flagMatches := ndFlagPattern.FindAllStringSubmatch(bash.Command, -1)
		isArchive := strings.Contains(bash.Command, "nd archive") || strings.Contains(bash.Command, "nd  archive")
		for _, match := range flagMatches {
			flag := match[1]
			// Strip =value if present (e.g., --format=json -> --format)
			if idx := strings.Index(flag, "="); idx != -1 {
				flag = flag[:idx]
			}
			if msg, ok := invalidFlags[flag]; ok {
				fmt.Fprintf(os.Stderr, "nd guard: invalid flag %q. %s\nConsult the nd skill's CLI_REFERENCE.md for valid flags.\n", flag, msg)
				os.Exit(2)
			}
			if !isArchive {
				if msg, ok := invalidFlagsForNonArchive[flag]; ok {
					fmt.Fprintf(os.Stderr, "nd guard: invalid flag %q. %s\nConsult the nd skill's CLI_REFERENCE.md for valid flags.\n", flag, msg)
					os.Exit(2)
				}
			}
		}

		return nil
	},
}

// buildValidCommandSet returns the set of valid top-level nd subcommand names
// (including aliases). Only direct children of root are included -- nested
// subcommands (e.g., "status" under "epic") are NOT valid as top-level commands.
func buildValidCommandSet(root *cobra.Command) map[string]bool {
	cmds := make(map[string]bool)
	for _, child := range root.Commands() {
		cmds[child.Name()] = true
		for _, alias := range child.Aliases {
			cmds[alias] = true
		}
	}
	return cmds
}

func init() {
	rootCmd.AddCommand(guardCmd)
}
