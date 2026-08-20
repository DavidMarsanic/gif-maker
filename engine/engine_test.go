package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLookPathFallback verifies that lookPath finds a tool that isn't on
// PATH but does exist in one of fallbackToolDirs' locations. This is the
// exact scenario that breaks a GUI-launched build: LaunchServices/
// securexe-launcher hand the process a minimal PATH that excludes
// Homebrew's install directory, so exec.LookPath alone fails even though
// the tool is genuinely installed.
func TestLookPathFallback(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("fallback dir layout under test is POSIX-specific")
	}

	dir := t.TempDir()
	toolName := "totally-fake-tool-for-test"
	toolPath := filepath.Join(dir, toolName)
	if err := os.WriteFile(toolPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("writing fake tool: %v", err)
	}

	restore := fallbackToolDirsFunc
	fallbackToolDirsFunc = func() []string { return []string{dir} }
	defer func() { fallbackToolDirsFunc = restore }()

	got, err := lookPath(toolName)
	if err != nil {
		t.Fatalf("lookPath(%q) = error %v, want success via fallback dir", toolName, err)
	}
	if got != toolPath {
		t.Errorf("lookPath(%q) = %q, want %q", toolName, got, toolPath)
	}
}

// TestLookPathFallbackSkipsNonExecutable ensures a same-named file that
// isn't executable (e.g. a stray non-executable file in a fallback dir)
// is not mistaken for the real tool.
func TestLookPathFallbackSkipsNonExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("exec-bit check under test is POSIX-specific")
	}

	dir := t.TempDir()
	toolName := "totally-fake-nonexec-tool"
	toolPath := filepath.Join(dir, toolName)
	if err := os.WriteFile(toolPath, []byte("not executable"), 0644); err != nil {
		t.Fatalf("writing fake non-executable file: %v", err)
	}

	restore := fallbackToolDirsFunc
	fallbackToolDirsFunc = func() []string { return []string{dir} }
	defer func() { fallbackToolDirsFunc = restore }()

	if _, err := lookPath(toolName); err == nil {
		t.Errorf("lookPath(%q) unexpectedly succeeded for a non-executable file", toolName)
	}
}

// TestLookPathMissingEverywhere ensures a genuinely absent tool still
// produces an error rather than a false positive.
func TestLookPathMissingEverywhere(t *testing.T) {
	restore := fallbackToolDirsFunc
	fallbackToolDirsFunc = func() []string { return []string{t.TempDir()} }
	defer func() { fallbackToolDirsFunc = restore }()

	if _, err := lookPath("definitely-does-not-exist-anywhere-tool"); err == nil {
		t.Error("lookPath succeeded for a tool that exists nowhere")
	}
}
