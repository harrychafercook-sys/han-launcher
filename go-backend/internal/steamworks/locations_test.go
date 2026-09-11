package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSteamLibraryFolders(t *testing.T) {
	data := []byte(`
"libraryfolders"
{
    "0" { "path" "C:\\Program Files (x86)\\Steam" }
    "1" { "path" "E:\\Games\\SteamLibrary" }
}`)

	paths := parseSteamLibraryFolders(data)
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2: %v", len(paths), paths)
	}
	if paths[1] != `E:\Games\SteamLibrary` {
		t.Fatalf("second path = %q", paths[1])
	}
}

func TestFindAppInstallDirInLibraries(t *testing.T) {
	root := t.TempDir()
	steamApps := filepath.Join(root, "steamapps")
	dayZPath := filepath.Join(steamApps, "common", "DayZ")
	if err := os.MkdirAll(dayZPath, 0755); err != nil {
		t.Fatalf("create DayZ path: %v", err)
	}

	manifest := []byte(`"AppState" { "appid" "221100" "installdir" "DayZ" }`)
	manifestPath := filepath.Join(steamApps, fmt.Sprintf("appmanifest_%d.acf", 221100))
	if err := os.WriteFile(manifestPath, manifest, 0600); err != nil {
		t.Fatalf("create manifest: %v", err)
	}

	if got := findAppInstallDirInLibraries(221100, []string{root}); got != dayZPath {
		t.Fatalf("install path = %q, want %q", got, dayZPath)
	}
}
