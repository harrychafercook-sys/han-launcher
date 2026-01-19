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
	ptrSteamFriends   uintptr
	f_GetPersonaName  func(uintptr) string
	f_GetPersonaState func(uintptr) int

	// User Interface
	ptrSteamUser uintptr
	f_GetSteamID func(uintptr) uint64
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
		bindSafe(&f_GetPersonaState, libHandle, "SteamAPI_ISteamFriends_GetPersonaState")
		bindSafe(&f_GetFriendCount, libHandle, "SteamAPI_ISteamFriends_GetFriendCount")
		bindSafe(&f_GetFriendByIndex, libHandle, "SteamAPI_ISteamFriends_GetFriendByIndex")
		bindSafe(&f_GetFriendPersonaName, libHandle, "SteamAPI_ISteamFriends_GetFriendPersonaName")
		bindSafe(&f_GetFriendGamePlayed, libHandle, "SteamAPI_ISteamFriends_GetFriendGamePlayed")
		bindSafe(&f_GetFriendPersonaState, libHandle, "SteamAPI_ISteamFriends_GetFriendPersonaState")
	}

	// Try SteamInternal_CreateInterface (Modern/Internal way)
	var createInterface func(string) uintptr
	bindSafe(&createInterface, libHandle, "SteamInternal_CreateInterface")

	if createInterface != nil {
		// Try a few user versions
		userVersions := []string{
			"SteamUser023\x00", "SteamUser022\x00", "SteamUser021\x00", "SteamUser020\x00",
			"SteamUser019\x00", "SteamUser018\x00", "SteamUser017\x00", "SteamUser016\x00",
		}
		for _, v := range userVersions {
			ptr := createInterface(v)
			if ptr != 0 {
				ptrSteamUser = ptr
				break
			}
		}
	}

	// Fallback to old globals if CreateInterface failed or didn't find user
	if ptrSteamUser == 0 {
		var getUser func() uintptr
		// Valid export names to try
		userExports := []string{
			"SteamAPI_SteamUser",
			"SteamUser",
			"SteamAPI_SteamUser_v021",
			"SteamAPI_SteamUser_v020",
		}

		for _, name := range userExports {
			bindSafe(&getUser, libHandle, name)
			if getUser != nil {
				ptrSteamUser = getUser()
				break
			}
		}
	}

	if ptrSteamUser != 0 {
		bindSafe(&f_GetSteamID, libHandle, "SteamAPI_ISteamUser_GetSteamID")
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

// GetPersonaState returns the current user's state (0=Offline, 1=Online, etc.)
func GetPersonaState() int {
	if !initialized || ptrSteamFriends == 0 || f_GetPersonaState == nil {
		return 0
	}
	return f_GetPersonaState(ptrSteamFriends)
}

// GetSteamID returns the local user's 64-bit Steam ID
func GetSteamID() uint64 {
	if !initialized {
		return 0
	}

	// Primary: ISteamUser
	if ptrSteamUser != 0 && f_GetSteamID != nil {
		return f_GetSteamID(ptrSteamUser)
	}

	// Fallback: ISteamApps::GetAppOwner
	// Note: ptrSteamApps and f_GetAppOwner are defined in ugc.go
	if ptrSteamApps != 0 && f_GetAppOwner != nil {
		return f_GetAppOwner(ptrSteamApps)
	}

	return 0
}

// GetFriendPersonaState returns the state of a specific user
func GetFriendPersonaState(steamID uint64) int {
	if !initialized || ptrSteamFriends == 0 || f_GetFriendPersonaState == nil {
		return 0
	}
	return f_GetFriendPersonaState(ptrSteamFriends, steamID)
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
