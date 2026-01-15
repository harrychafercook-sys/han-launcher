package steamworks

import (
	"fmt"
	"strconv"
	// syscall removed, using handle passed from parent
)

// Global pointers for interfaces
var (
	ptrSteamUGC  uintptr
	ptrSteamApps uintptr

	// Function pointers
	f_SubscribeItem         func(uintptr, uint64)
	f_UnsubscribeItem       func(uintptr, uint64)
	f_GetItemState          func(uintptr, uint64) uint32
	f_GetItemInstallInfo    func(uintptr, uint64, *uint64, *byte, uint32, *uint32) bool
	f_GetItemDownloadInfo   func(uintptr, uint64, *uint64, *uint64) bool
	f_DownloadItem          func(uintptr, uint64, bool) bool
	f_GetNumSubscribedItems func(uintptr) uint32
	f_GetSubscribedItems    func(uintptr, *uint64, uint32) uint32

	f_AppInstallDir func(uintptr, uint32, *byte, uint32) uint32
)

// InitManualBindings loads UGC/Apps symbols using the existing lib handle
func InitManualBindings(lib uintptr) {
	// 1. Get Interfaces
	var getUGC func() uintptr
	bindSafe(&getUGC, lib, "SteamAPI_SteamUGC")
	if getUGC == nil {
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v020")
	}
	if getUGC == nil {
		// Try matching version with Friends (v017)
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v017")
	}
	if getUGC == nil {
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v016")
	}
	if getUGC == nil {
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v015")
	}
	if getUGC == nil {
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v014")
	}
	if getUGC == nil {
		// DayZ official DLL uses v012? (Previous attempt failed, but keeping it)
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v012")
	}
	if getUGC == nil {
		bindSafe(&getUGC, lib, "SteamAPI_SteamUGC_v010")
	}
	if getUGC != nil {
		ptrSteamUGC = getUGC()
	}

	var getApps func() uintptr
	bindSafe(&getApps, lib, "SteamAPI_SteamApps")
	if getApps == nil {
		bindSafe(&getApps, lib, "SteamAPI_SteamApps_v008")
	}
	if getApps != nil {
		ptrSteamApps = getApps()
	}

	// 2. Bind Functions (Flat API)

	bindSafe(&f_SubscribeItem, lib, "SteamAPI_ISteamUGC_SubscribeItem")
	bindSafe(&f_UnsubscribeItem, lib, "SteamAPI_ISteamUGC_UnsubscribeItem")
	bindSafe(&f_GetItemState, lib, "SteamAPI_ISteamUGC_GetItemState")
	bindSafe(&f_GetItemInstallInfo, lib, "SteamAPI_ISteamUGC_GetItemInstallInfo")
	bindSafe(&f_GetItemDownloadInfo, lib, "SteamAPI_ISteamUGC_GetItemDownloadInfo")
	bindSafe(&f_DownloadItem, lib, "SteamAPI_ISteamUGC_DownloadItem")
	bindSafe(&f_GetNumSubscribedItems, lib, "SteamAPI_ISteamUGC_GetNumSubscribedItems")
	bindSafe(&f_GetSubscribedItems, lib, "SteamAPI_ISteamUGC_GetSubscribedItems")

	bindSafe(&f_AppInstallDir, lib, "SteamAPI_ISteamApps_GetAppInstallDir")

	if ptrSteamUGC != 0 {
		// UGC interface found
	}
}

// --- Wrappers ---

func SubscribeMod(modId string) error {
	if ptrSteamUGC == 0 || f_SubscribeItem == nil {
		return fmt.Errorf("UGC interface not available")
	}
	id, _ := strconv.ParseUint(modId, 10, 64)
	f_SubscribeItem(ptrSteamUGC, id)
	return nil
}

func UnsubscribeMod(modId string) error {
	if ptrSteamUGC == 0 || f_UnsubscribeItem == nil {
		return fmt.Errorf("UGC interface not available")
	}
	id, _ := strconv.ParseUint(modId, 10, 64)
	f_UnsubscribeItem(ptrSteamUGC, id)
	return nil
}

func GetItemState(modId string) uint32 {
	if ptrSteamUGC == 0 || f_GetItemState == nil {
		return 0
	}
	id, _ := strconv.ParseUint(modId, 10, 64)
	return f_GetItemState(ptrSteamUGC, id)
}

func GetDownloadInfo(modId string) (uint64, uint64) {
	if ptrSteamUGC == 0 || f_GetItemDownloadInfo == nil {
		return 0, 0
	}
	id, _ := strconv.ParseUint(modId, 10, 64)
	var current, total uint64
	if f_GetItemDownloadInfo(ptrSteamUGC, id, &current, &total) {
		return current, total
	}
	return 0, 0
}

func DownloadItem(modId string, highPriority bool) bool {
	if ptrSteamUGC == 0 || f_DownloadItem == nil {
		return false
	}
	id, _ := strconv.ParseUint(modId, 10, 64)
	return f_DownloadItem(ptrSteamUGC, id, highPriority)
}

func ResolveModPath(modId string) string {
	if ptrSteamUGC == 0 || f_GetItemInstallInfo == nil {
		return ""
	}
	id, _ := strconv.ParseUint(modId, 10, 64)

	var size uint64
	var timestamp uint32
	buf := make([]byte, 4096)

	// API: GetItemInstallInfo(id, &size, buf, bufSize, &timestamp)
	success := f_GetItemInstallInfo(ptrSteamUGC, id, &size, &buf[0], uint32(len(buf)), &timestamp)
	if !success {
		return ""
	}

	// C string to Go string
	n := 0
	for i, b := range buf {
		if b == 0 {
			n = i
			break
		}
	}
	return string(buf[:n])
}

func GetSubscribedItems() []uint64 {
	if ptrSteamUGC == 0 || f_GetNumSubscribedItems == nil || f_GetSubscribedItems == nil {
		return nil
	}

	count := f_GetNumSubscribedItems(ptrSteamUGC)
	if count == 0 {
		return nil
	}

	ids := make([]uint64, count)
	// API: GetSubscribedItems( destArray, maxEntries ) -> numReturned
	received := f_GetSubscribedItems(ptrSteamUGC, &ids[0], count)

	return ids[:received]
}

func GetAppInstallDir(appID uint32) string {
	if ptrSteamApps == 0 || f_AppInstallDir == nil {
		return ""
	}

	buf := make([]byte, 4096)

	written := f_AppInstallDir(ptrSteamApps, appID, &buf[0], uint32(len(buf)))
	if written > 0 && written < uint32(len(buf)) {
		// Usually pure length excluding null
		n := int(written)
		if n > len(buf) {
			n = len(buf)
		}
		// Verify null just in case
		if buf[n-1] == 0 {
			return string(buf[:n-1])
		}
		return string(buf[:n])
	}
	// fallback search
	n := 0
	for i, b := range buf {
		if b == 0 {
			n = i
			break
		}
	}
	return string(buf[:n])
}
