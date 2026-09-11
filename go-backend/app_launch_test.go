package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDayZLaunchExecutable(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		inputAsExe bool
	}{
		{name: "prefers BattlEye executable", executable: "DayZ_BE.exe"},
		{name: "falls back to x64 executable", executable: "DayZ_x64.exe"},
		{name: "accepts executable path", executable: "DayZ_x64.exe", inputAsExe: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			exePath := filepath.Join(dir, test.executable)
			if err := os.WriteFile(exePath, []byte("test"), 0600); err != nil {
				t.Fatalf("create executable: %v", err)
			}

			input := dir
			if test.inputAsExe {
				input = exePath
			}

			resolvedExe, resolvedDir, err := resolveDayZLaunchExecutable(input)
			if err != nil {
				t.Fatalf("resolve path: %v", err)
			}
			if resolvedExe != exePath {
				t.Fatalf("resolved executable = %q, want %q", resolvedExe, exePath)
			}
			if resolvedDir != dir {
				t.Fatalf("resolved directory = %q, want %q", resolvedDir, dir)
			}
		})
	}
}

func TestResolveDayZLaunchExecutableRejectsInvalidFolder(t *testing.T) {
	if _, _, err := resolveDayZLaunchExecutable(t.TempDir()); err == nil {
		t.Fatal("expected a folder without a DayZ executable to be rejected")
	}
}
