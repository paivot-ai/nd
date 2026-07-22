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
	"status":      "stats (or nd epic status <id>)",
	"version":     "--version flag (nd --version)",
	"find":        "search",
	"get":         "show",
	"set":         "update (or nd config set)",
	"add":         "create",
	"remove":      "delete",
	"info":        "show",
	"describe":    "show",
	"ls":          "list",
	"rm":          "delete",
	"mv":          "update --title",
	"log":         "path (execution log) or stats",
	"history":     "path (execution log) or show <id>",
	"inspect":     "show",
	"view":        "show",
	"pull":        "sync",
	"push":        "sync",
	"selfupdate":  "upgrade",
	"self-update": "upgrade",
	"commit":      "sync (the backlog lives on the nd/backlog branch, not code branches)",
	"assign":      "claim (atomic) or update --assignee",
	"take":        "claim",
	"grab":        "claim",
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

// ndCmdPattern matches `nd <subcommand>` only when `nd` is in command
// position: at the start of the command, or immediately after a shell control
// operator (`;`, `|`, `&`, `(`, or `$(`), optionally separated by spaces/tabs.
//
// A bare space or newline is deliberately NOT a boundary. That way `nd`
// appearing inside a quoted string, a comment, a heredoc or commit message, or
// as an argument to another command (`echo "... nd release"`, `grep "nd foo"`,
// `command -v nd`, `/path/to/nd`) is not misread as an nd invocation. The
// captured token stops at the next whitespace or shell metacharacter so a
// trailing `)`, `>`, etc. is not swallowed into the subcommand name.
var ndCmdPattern = regexp.MustCompile(`(?:^|[;|&(])[ \t]*nd[ \t]+([^\s;|&()<>]+)`)

// pvgNDPattern matches `pvg nd <subcommand>` -- these are pvg subcommands
// routed through pvg's own command tree, not bare nd commands.
var pvgNDPattern = regexp.MustCompile(`pvg\s+nd\s+(\S+)`)

// ndFlagPattern matches `nd ... --flag`, anchored to a command-position `nd`
// (same boundary rules as ndCmdPattern) so flags mentioned inside strings or
// other commands are not flagged.
var ndFlagPattern = regexp.MustCompile(`(?:^|[;|&(])[ \t]*nd[ \t]+[^|;&\n]*?(--\S+)`)

// subcmdShape matches a plausible nd subcommand token: a lowercase identifier
// optionally containing digits, hyphens, or underscores. Tokens that are not
// subcommand-shaped (punctuation, redirections, `===`, `stack:`) are never
// treated as subcommands.
var subcmdShape = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

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

		// Validate each command-position nd subcommand.
		for _, subcmd := range extractNDSubcommands(bash.Command) {
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

// extractNDSubcommands returns the bare-nd subcommand names in a shell command
// that the guard should validate. It ignores flags, pvg-routed subcommands
// (handled by pvg's own command tree), tokens that are not subcommand-shaped,
// and duplicates. Order of first appearance is preserved.
func extractNDSubcommands(command string) []string {
	// pvg nd <subcommand> is routed by pvg, not bare nd -- collect those to skip.
	pvgSubcmds := make(map[string]bool)
	for _, pm := range pvgNDPattern.FindAllStringSubmatch(command, -1) {
		pvgSubcmds[pm[1]] = true
	}

	var subcmds []string
	seen := make(map[string]bool)
	for _, match := range ndCmdPattern.FindAllStringSubmatch(command, -1) {
		token := match[1]
		if strings.HasPrefix(token, "-") {
			continue // a flag (e.g. nd --version), not a subcommand
		}
		if !subcmdShape.MatchString(token) {
			continue // punctuation, redirection, etc. -- not a subcommand
		}
		if seen[token] || pvgSubcmds[token] {
			continue
		}
		seen[token] = true
		subcmds = append(subcmds, token)
	}
	return subcmds
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
