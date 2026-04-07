package cmd

import (
	"testing"
)

func TestPvgNDPattern_MatchesPvgSubcommands(t *testing.T) {
	tests := []struct {
		command  string
		expected map[string]bool
	}{
		{"pvg nd root --ensure", map[string]bool{"root": true}},
		{"pvg nd show AMA-y0ge", map[string]bool{"show": true}},
		{"pvg nd update AMA-y0ge --add-label delivered", map[string]bool{"update": true}},
		{"pvg  nd  stats", map[string]bool{"stats": true}},
		{"nd show AMA-y0ge", map[string]bool{}}, // bare nd, not pvg nd
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			matches := pvgNDPattern.FindAllStringSubmatch(tt.command, -1)
			got := make(map[string]bool)
			for _, m := range matches {
				got[m[1]] = true
			}
			for k := range tt.expected {
				if !got[k] {
					t.Errorf("expected pvg subcmd %q in %q, not found", k, tt.command)
				}
			}
			for k := range got {
				if !tt.expected[k] {
					t.Errorf("unexpected pvg subcmd %q in %q", k, tt.command)
				}
			}
		})
	}
}

func TestGuardSkipsPvgNDSubcommands(t *testing.T) {
	// "pvg nd root" should NOT be flagged as unknown.
	// The ndCmdPattern will match "nd root", but pvgNDPattern also matches it,
	// so the guard should skip validation for "root".
	command := "pvg nd root --ensure"

	pvgSubcmds := make(map[string]bool)
	for _, pm := range pvgNDPattern.FindAllStringSubmatch(command, -1) {
		pvgSubcmds[pm[1]] = true
	}

	matches := ndCmdPattern.FindAllStringSubmatch(command, -1)
	for _, match := range matches {
		subcmd := match[1]
		if subcmd == "" {
			continue
		}
		if pvgSubcmds[subcmd] {
			// This is the expected path -- pvg nd root should be skipped.
			return
		}
		t.Errorf("expected %q to be skipped as pvg subcommand", subcmd)
	}
}

func TestGuardStillCatchesBareNDErrors(t *testing.T) {
	// "pvg nd root && nd hallucinated_cmd" should still catch hallucinated_cmd.
	command := "pvg nd root && nd hallucinated_cmd foo"

	pvgSubcmds := make(map[string]bool)
	for _, pm := range pvgNDPattern.FindAllStringSubmatch(command, -1) {
		pvgSubcmds[pm[1]] = true
	}

	validCmds := buildValidCommandSet(rootCmd)

	matches := ndCmdPattern.FindAllStringSubmatch(command, -1)
	foundInvalid := false
	for _, match := range matches {
		subcmd := match[1]
		if subcmd == "" || subcmd[0] == '-' {
			continue
		}
		if pvgSubcmds[subcmd] {
			continue
		}
		if !validCmds[subcmd] {
			foundInvalid = true
			if subcmd != "hallucinated_cmd" {
				t.Errorf("expected hallucinated_cmd to be caught, got %q", subcmd)
			}
		}
	}
	if !foundInvalid {
		t.Error("expected guard to catch 'hallucinated_cmd' but it was not flagged")
	}
}
