package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

var (
	libraryPathPattern = regexp.MustCompile(`(?i)"path"\s+"((?:\\.|[^"])*)"`)
	installDirPattern  = regexp.MustCompile(`(?i)"installdir"\s+"((?:\\.|[^"])*)"`)
)

func decodeVDFString(value string) string {
	decoded, err := strconv.Unquote(`"` + value + `"`)
	if err == nil {
		return decoded
	}
	return strings.ReplaceAll(value, `\\`, `\`)
}

func parseSteamLibraryFolders(data []byte) []string {
	matches := libraryPathPattern.FindAllSubmatch(data, -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := filepath.Clean(decodeVDFString(string(match[1])))
		if path != "." && path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func steamInstallPathFromRegistry() string {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Valve\Steam`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer key.Close()

	for _, valueName := range []string{"SteamPath", "InstallPath"} {
		value, _, valueErr := key.GetStringValue(valueName)
		if valueErr == nil && strings.TrimSpace(value) != "" {
			return filepath.Clean(value)
		}
	}
	return ""
}

func steamLibraryRoots() []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			return
		}
		key := strings.ToLower(path)
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, path)
	}

	steamPath := steamInstallPathFromRegistry()
	if steamPath != "" {
		add(steamPath)
		libraryFile := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
		if data, err := os.ReadFile(libraryFile); err == nil {
			for _, path := range parseSteamLibraryFolders(data) {
				add(path)
			}
		}
	}

	return roots
}

func findAppInstallDirInLibraries(appID uint32, roots []string) string {
	manifestName := fmt.Sprintf("appmanifest_%d.acf", appID)
	for _, root := range roots {
		manifestPath := filepath.Join(root, "steamapps", manifestName)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		match := installDirPattern.FindSubmatch(data)
		if len(match) < 2 {
			continue
		}
		installDir := decodeVDFString(string(match[1]))
		candidate := filepath.Join(root, "steamapps", "common", installDir)
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// FindAppInstallDir uses Steamworks first, then reads Steam's configured library
// folders. Unlike fixed drive-letter guesses, this works with libraries on any drive.
func FindAppInstallDir(appID uint32) string {
	if nativePath := GetAppInstallDir(appID); nativePath != "" {
		if info, err := os.Stat(nativePath); err == nil && info.IsDir() {
			return nativePath
		}
	}
	return findAppInstallDirInLibraries(appID, steamLibraryRoots())
}
