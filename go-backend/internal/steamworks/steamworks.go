package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ebitengine/purego"
)

var (
	initialized bool
	libHandle   uintptr

	// Core Functions
	f_Init         func() bool
	f_RunCallbacks func()

	// Friends Interface
	ptrSteamFriends  uintptr
	f_GetPersonaName func(uintptr) string
)

// Init initializes the Steamworks API manually.
func Init() error {
	if initialized {
		return nil
	}

	// 1. Load Library
	dll, err := syscall.LoadLibrary("steam_api64.dll")
	if err != nil {
		fmt.Println("[Steamworks] Warning: steam_api64.dll not found. Init failed.")
		return err
	}
	libHandle = uintptr(dll)

	// 2. Bind Core Functions
	// Try standard name first
	bindSafe(&f_Init, libHandle, "SteamAPI_Init")
	// Fallback to internal name (seen in some versions)
	if f_Init == nil {
		fmt.Println("[Steamworks] SteamAPI_Init not found, trying SteamInternal_SteamAPI_Init...")
		bindSafe(&f_Init, libHandle, "SteamInternal_SteamAPI_Init")
	}

	bindSafe(&f_RunCallbacks, libHandle, "SteamAPI_RunCallbacks")

	// 3. Call Init
	if f_Init != nil {
		if f_Init() {
			initialized = true

			// 4. Bind Interfaces
			bindFriends()

			// 5. Initialize UGC/Apps (in ugc.go)
			InitManualBindings(libHandle)

			return nil
		}
	} else {
		fmt.Println("[Steamworks] CRITICAL: Could not find SteamAPI_Init symbol.")
	}

	return fmt.Errorf("SteamAPI_Init returned false (or symbol missing)")
}

func bindFriends() {
	var getFriends func() uintptr
	// Try standard name
	bindSafe(&getFriends, libHandle, "SteamAPI_SteamFriends")
	if getFriends == nil {
		// Fallback to versioned name (SDK 1.60+)
		bindSafe(&getFriends, libHandle, "SteamAPI_SteamFriends_v017")
	}

	if getFriends != nil {
		ptrSteamFriends = getFriends()
		bindSafe(&f_GetPersonaName, libHandle, "SteamAPI_ISteamFriends_GetPersonaName")
	}
}

func bindSafe(dest interface{}, lib uintptr, name string) {
	defer func() {
		if r := recover(); r != nil {
			// Silent fail - symbol not found
		}
	}()
	purego.RegisterLibFunc(dest, lib, name)
}

// RunCallbacks process Steam events.
func RunCallbacks() {
	if initialized && f_RunCallbacks != nil {
		f_RunCallbacks()
	}
}

// GetPersonaName returns the current user's display name.
func GetPersonaName() string {
	if !initialized || ptrSteamFriends == 0 || f_GetPersonaName == nil {
		return ""
	}
	return f_GetPersonaName(ptrSteamFriends)
}

// IsInitialized returns the current state.
func IsInitialized() bool {
	return initialized
}

// --- UTILS (Unchanged) ---

// IsModFolderValid - ported from JS
func IsModFolderValid(folderPath string) bool {
	if folderPath == "" {
		return false
	}
	info, err := os.Stat(folderPath)
	if os.IsNotExist(err) || !info.IsDir() {
		return false
	}

	// Simple check: does it have any files?
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return false
	}

	// For speed, let's just check if there's > 0 content.
	for _, e := range entries {
		if e.IsDir() {
			if IsModFolderValid(filepath.Join(folderPath, e.Name())) {
				return true
			}
		} else {
			i, _ := e.Info()
			if i.Size() > 0 {
				return true
			}
		}
	}
	return false
}
