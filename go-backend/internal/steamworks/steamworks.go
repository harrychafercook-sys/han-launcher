package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows/registry"
)

var (
	initialized   bool
	initAttempted bool
	initFailure   error
	initMutex     sync.Mutex
	libHandle     uintptr

	// Core Functions
	f_Init         func() bool
	f_Shutdown     func()
	f_RunCallbacks func()

	// Friends Interface
	ptrSteamFriends             uintptr
	f_GetPersonaName            func(uintptr) string
	f_GetPersonaState           func(uintptr) int
	f_ActivateGameOverlayToUser func(uintptr, string, uint64)

	// User Interface
	ptrSteamUser uintptr
	f_GetSteamID func(uintptr) uint64
)

// Init initializes the Steamworks API manually.
func Init() error {
	initMutex.Lock()
	defer initMutex.Unlock()

	if initialized {
		return nil
	}
	if initAttempted {
		if initFailure != nil {
			return initFailure
		}
		return fmt.Errorf("SteamAPI_Init already failed; restart HAN to try again")
	}
	initAttempted = true

	// Load, bind and initialize once per HAN process. Failed SteamAPI_Init calls
	// retain native memory in the Steam DLL, so retrying in the same process
	// causes unbounded growth. Restarting HAN provides a clean retry after the
	// user starts or signs into Steam.
	if libHandle == 0 {
		dll, err := syscall.LoadLibrary("steam_api64.dll")
		if err != nil {
			fmt.Println("[Steamworks] Warning: steam_api64.dll not found. Init failed.")
			initFailure = err
			return err
		}
		libHandle = uintptr(dll)

		// Try the standard export first, then the internal fallback used by
		// some Steamworks SDK versions.
		bindSafe(&f_Init, libHandle, "SteamAPI_Init")
		if f_Init == nil {
			fmt.Println("[Steamworks] SteamAPI_Init not found, trying SteamInternal_SteamAPI_Init...")
			bindSafe(&f_Init, libHandle, "SteamInternal_SteamAPI_Init")
		}
		bindSafe(&f_RunCallbacks, libHandle, "SteamAPI_RunCallbacks")
		bindSafe(&f_Shutdown, libHandle, "SteamAPI_Shutdown")
	}

	// Call Init against the already-loaded module. A later retry can now
	// succeed without loading or rebinding the DLL again.
	if f_Init != nil {
		if f_Init() {
			initialized = true

			// 4. Bind Interfaces
			bindFriends()

			// 5. Initialize UGC/Apps (in ugc.go)
			InitManualBindings(libHandle)

			initFailure = nil
			return nil
		}
	} else {
		fmt.Println("[Steamworks] CRITICAL: Could not find SteamAPI_Init symbol.")
	}

	initFailure = fmt.Errorf("SteamAPI_Init returned false (or symbol missing)")
	return initFailure
}

// ActiveSession reads Steam's own per-user state without invoking Steamworks.
// Both values are zero while Steam is closed or waiting at its sign-in screen.
func ActiveSession() (uint64, bool) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Valve\Steam\ActiveProcess`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return 0, false
	}
	defer key.Close()

	activeUser, _, userErr := key.GetIntegerValue("ActiveUser")
	pid, _, pidErr := key.GetIntegerValue("pid")
	if userErr != nil || pidErr != nil || activeUser == 0 || pid == 0 {
		return 0, false
	}
	return activeUser, true
}

// EndSession releases a successful Steamworks session and resets the one-shot
// initialization guard. The next signed-out -> signed-in transition may then
// make one fresh initialization attempt without accumulating failed retries.
func EndSession() {
	initMutex.Lock()
	defer initMutex.Unlock()

	if initialized && f_Shutdown != nil {
		f_Shutdown()
	}
	initialized = false
	initAttempted = false
	initFailure = nil
	ptrSteamFriends = 0
	ptrSteamUser = 0
	ptrSteamUGC = 0
	ptrSteamApps = 0
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
		bindSafe(&f_ActivateGameOverlayToUser, libHandle, "SteamAPI_ISteamFriends_ActivateGameOverlayToUser")
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
	initMutex.Lock()
	defer initMutex.Unlock()
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
