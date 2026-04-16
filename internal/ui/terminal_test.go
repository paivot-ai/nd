package ui

import (
	"os"
	"testing"
)

// resetColorState restores colorOverride and unsets color-related env vars.
func resetColorState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		colorOverride = ""
		os.Unsetenv("NO_COLOR")
		os.Unsetenv("CLICOLOR")
		os.Unsetenv("CLICOLOR_FORCE")
	})
}

func TestSetColorMode_Always(t *testing.T) {
	resetColorState(t)

	if err := SetColorMode("always"); err != nil {
		t.Fatalf("SetColorMode(always): %v", err)
	}
	if !ShouldUseColor() {
		t.Error("ShouldUseColor() should be true when mode is always")
	}
}

func TestSetColorMode_Never(t *testing.T) {
	resetColorState(t)

	if err := SetColorMode("never"); err != nil {
		t.Fatalf("SetColorMode(never): %v", err)
	}
	if ShouldUseColor() {
		t.Error("ShouldUseColor() should be false when mode is never")
	}
}

func TestSetColorMode_Auto_EnvVars(t *testing.T) {
	resetColorState(t)

	if err := SetColorMode("auto"); err != nil {
		t.Fatalf("SetColorMode(auto): %v", err)
	}

	// NO_COLOR=1 should suppress color.
	os.Setenv("NO_COLOR", "1")
	if ShouldUseColor() {
		t.Error("ShouldUseColor() should be false when NO_COLOR is set in auto mode")
	}
	os.Unsetenv("NO_COLOR")

	// CLICOLOR_FORCE=1 should force color.
	os.Setenv("CLICOLOR_FORCE", "1")
	if !ShouldUseColor() {
		t.Error("ShouldUseColor() should be true when CLICOLOR_FORCE is set in auto mode")
	}
	os.Unsetenv("CLICOLOR_FORCE")
}

func TestSetColorMode_Invalid(t *testing.T) {
	resetColorState(t)

	err := SetColorMode("bogus")
	if err == nil {
		t.Fatal("SetColorMode(bogus) should return an error")
	}
	want := "must be always, auto, or never"
	if got := err.Error(); !contains(got, want) {
		t.Errorf("error message %q should contain %q", got, want)
	}
}

func TestSetColorMode_FlagOverridesEnv(t *testing.T) {
	resetColorState(t)

	// Set NO_COLOR -- normally suppresses color.
	os.Setenv("NO_COLOR", "1")

	// But --color=always should override it.
	if err := SetColorMode("always"); err != nil {
		t.Fatalf("SetColorMode(always): %v", err)
	}
	if !ShouldUseColor() {
		t.Error("ShouldUseColor() should be true: --color=always overrides NO_COLOR")
	}
}

func TestSetColorMode_NeverOverridesEnv(t *testing.T) {
	resetColorState(t)

	// Set CLICOLOR_FORCE -- normally forces color.
	os.Setenv("CLICOLOR_FORCE", "1")

	// But --color=never should override it.
	if err := SetColorMode("never"); err != nil {
		t.Fatalf("SetColorMode(never): %v", err)
	}
	if ShouldUseColor() {
		t.Error("ShouldUseColor() should be false: --color=never overrides CLICOLOR_FORCE")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
