package main

import (
	"context"
	"dayz-launcher-go/internal/a2s"
	"dayz-launcher-go/internal/dayz"
	"syscall"

	"dayz-launcher-go/internal/steamworks"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const UseNativeSteamworks = true

// App struct
type App struct {
	ctx               context.Context
	httpClient        *http.Client
	lastPersonaName   string
	priorityCooldowns map[string]time.Time
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		priorityCooldowns: make(map[string]time.Time),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	if UseNativeSteamworks {
		// Initialize Native Steamworks
		// Ticker loop for callbacks
		go func() {
			if err := steamworks.Init(); err != nil {
				fmt.Println("[App] Native Steamworks Init Failed:", err)
			}

			// Ticker for callbacks (Reduced from 33ms to 500ms for lower CPU)
			ticker := time.NewTicker(500 * time.Millisecond)
			// Ticker for download updates (Reduced from 1s to 2s)
			dlTicker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			defer dlTicker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if !steamworks.IsInitialized() {
						// Attempt to initialize if not already connected
						if err := steamworks.Init(); err != nil {
							// Still failed, just ignore until next tick
						} else {
							fmt.Println("[App] Native Steamworks Initialized via Loop")
						}
					} else {
						// Only run callbacks if initialized
						steamworks.RunCallbacks()
					}
				case <-dlTicker.C:
					// Poll downloads and emit event if active
					if steamworks.IsInitialized() {
						data := a.GetActiveDownloads() // Reusing the method which formats correctly
						if mapData, ok := data.(map[string]interface{}); ok {
							if list, ok := mapData["data"].([]map[string]interface{}); ok && len(list) > 0 {
								// Emit 'download-update' event to frontend
								runtime.EventsEmit(ctx, "download-update", data)
							}
						}
					}
				}
			}
		}()
	} else {
		// Native Steamworks disabled - this path is not used
		fmt.Println("[App] UseNativeSteamworks is false - sidecar removed, this mode is deprecated")
	}
}

// -- HTTP METHODS (BattleMetrics Proxy) --

type BMResponse struct {
	Success   bool              `json:"success"`
	Data      interface{}       `json:"data,omitempty"`
	Error     string            `json:"error,omitempty"`
	RateLimit map[string]string `json:"rateLimit,omitempty"`
}

func (a *App) BattleMetricsFetch(url string) BMResponse {
	resp, err := a.httpClient.Get(url)
	if err != nil {
		return BMResponse{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return BMResponse{Success: false, Error: err.Error()}
	}

	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return BMResponse{Success: false, Error: "JSON Parse Error: " + err.Error()}
	}

	rateLimit := map[string]string{
		"remaining": resp.Header.Get("X-Rate-Limit-Remaining"),
		"limit":     resp.Header.Get("X-Rate-Limit-Limit"),
	}

	return BMResponse{
		Success:   true,
		Data:      data,
		RateLimit: rateLimit,
	}
}

// -- UDP METHODS --

func (a *App) FetchServerInfo(ip string, port int) (map[string]interface{}, error) {
	info, err := a2s.QueryInfo(ip, port)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{
		"success":     true,
		"name":        info.Name,
		"map":         info.Map,
		"players":     info.Players,
		"maxPlayers":  info.MaxPlayers,
		"environment": info.Environment,
		"password":    info.Password,
		"version":     info.Version,
		"tags":        info.Tags,
		"ping":        info.Latency,
	}, nil
}

func (a *App) FetchServerRules(ip string, port int) (map[string]interface{}, error) {
	rules, err := a2s.QueryRules(ip, port)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{
		"success": true,
		"rules":   rules,
	}, nil
}

func (a *App) FetchServerPlayers(ip string, port int) (map[string]interface{}, error) {
	players, err := a2s.QueryPlayers(ip, port)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{
		"success": true,
		"count":   len(players),
		"players": players,
	}, nil
}

// -- STEAM METHODS --

// -- STEAM METHODS --

func (a *App) LoginSteam() (interface{}, error) {
	// 1. Try Init if not initialized
	if !steamworks.IsInitialized() {
		fmt.Println("[App] LoginSteam: Steam not initialized, attempting init...")
		if err := steamworks.Init(); err != nil {
			fmt.Printf("[App] LoginSteam: Init failed: %v\n", err)
			return map[string]interface{}{"success": false, "error": "Could not connect to Steam"}, nil
		}
		// If success, we should likely wait a tiny bit or just proceed
		fmt.Println("[App] LoginSteam: Init successful!")
	}

	// 2. Fetch Name
	name := steamworks.GetPersonaName()
	if name == "" {
		return map[string]interface{}{"success": false}, nil
	}

	// 3. Update Cache
	a.lastPersonaName = name

	// Emit Event so Frontend updates immediately (if called from other modals)
	runtime.EventsEmit(a.ctx, "steam-connected", map[string]interface{}{"connected": true, "name": name})

	return map[string]interface{}{"success": true, "name": name}, nil
}

func (a *App) GetSteamStatus() interface{} {
	// Check if Steamworks is initialized
	isInit := steamworks.IsInitialized()
	if !isInit {
		return map[string]interface{}{"success": false, "connected": false, "error": "Steam not running"}
	}

	name := steamworks.GetPersonaName()
	if name != "" {
		a.lastPersonaName = name
	}

	// Fallback to cached name to prevent flicker
	finalName := a.lastPersonaName
	if finalName == "" {
		finalName = "Survivor" // Default until fetched
	}

	return map[string]interface{}{"success": true, "connected": true, "name": finalName}
}

func (a *App) SubscribeWorkshop(modId string) (interface{}, error) {
	err := steamworks.SubscribeMod(modId)
	if err != nil {
		fmt.Printf("[App] SubscribeWorkshop Failed: %v\n", err)
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{"success": true}, nil
}

func (a *App) PrioritizeWorkshop(modId string) (interface{}, error) {
	// Force Download Priority
	steamworks.DownloadItem(modId, true)
	return map[string]interface{}{"success": true}, nil
}

func (a *App) UnsubscribeWorkshop(modId string) (interface{}, error) {
	err := steamworks.UnsubscribeMod(modId)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{"success": true}, nil
}

func (a *App) UnsubscribeAll() (interface{}, error) {
	if UseNativeSteamworks {
		fmt.Println("[App] Unsubscribing all mods (ACTIVE DOWNLOADS WILL BE STOPPED)...")

		// 1. API Unsubscribe (Steamworks)
		items := steamworks.GetSubscribedItems()
		count := 0
		errors := 0
		for _, id := range items {
			if err := steamworks.UnsubscribeMod(fmt.Sprintf("%d", id)); err == nil {
				count++
			} else {
				errors++
				fmt.Printf("[App] Failed to unsubscribe %d: %v\n", id, err)
			}
		}

		return map[string]interface{}{"success": true, "count": count, "errors": errors, "method": "steam-unsub"}, nil
	}
	return map[string]interface{}{"success": false, "error": "Native Steamworks disabled"}, nil
}

func (a *App) DeleteAllModFiles() (interface{}, error) {
	fmt.Println("[App] Nuke Mode: Deleting all mod files from filesystem...")
	workshopPath := getWorkshopPath()
	count := 0
	errors := 0

	if workshopPath != "" {
		fmt.Printf("[App] Found workshop dir: %s\n", workshopPath)

		// A. Delete Content Folder (221100)
		entries, err := os.ReadDir(workshopPath)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					// Check if numeric (Mod ID)
					if regexp.MustCompile(`^\d+$`).MatchString(entry.Name()) {
						fullPath := filepath.Join(workshopPath, entry.Name())
						err := os.RemoveAll(fullPath)
						if err != nil {
							fmt.Printf("[App] Failed to delete %s: %v\n", fullPath, err)
							errors++
						} else {
							count++
						}
					}
				}
			}
		}

		// B. Delete Download Cache (steamapps/downloading)
		// workshopPath is .../steamapps/workshop/content/221100
		// We want .../steamapps/downloading
		steamAppsDir := filepath.Dir(filepath.Dir(filepath.Dir(workshopPath))) // Up 3 levels
		downloadingDir := filepath.Join(steamAppsDir, "downloading")

		if _, err := os.Stat(downloadingDir); err == nil {
			fmt.Printf("[App] Clearing Download Cache: %s\n", downloadingDir)
			entries, err := os.ReadDir(downloadingDir)
			if err == nil {
				for _, entry := range entries {
					fullPath := filepath.Join(downloadingDir, entry.Name())
					// Nuke everything in downloading
					err := os.RemoveAll(fullPath)
					if err != nil {
						fmt.Printf("[App] Failed to delete cache item %s: %v\n", fullPath, err)
					} else {
						fmt.Printf("[App] Deleted cache item: %s\n", entry.Name())
					}
				}
			}
		} else {
			fmt.Printf("[App] Download cache dir not found: %s\n", downloadingDir)
		}

		// C. Delete Workshop Temporary Downloads (steamapps/workshop/downloads/221100)
		// workshopPath is .../steamapps/workshop/content/221100
		workshopRootDir := filepath.Dir(filepath.Dir(workshopPath)) // .../steamapps/workshop
		workshopDownloadsDir := filepath.Join(workshopRootDir, "downloads", "221100")

		if _, err := os.Stat(workshopDownloadsDir); err == nil {
			fmt.Printf("[App] Clearing Workshop Temp Downloads: %s\n", workshopDownloadsDir)
			err := os.RemoveAll(workshopDownloadsDir)
			if err != nil {
				fmt.Printf("[App] Failed to delete workshop downloads dir %s: %v\n", workshopDownloadsDir, err)
				errors++
			} else {
				fmt.Printf("[App] Deleted workshop downloads dir: %s\n", workshopDownloadsDir)
				count++
			}
		} else {
			fmt.Printf("[App] Workshop downloads dir not found: %s\n", workshopDownloadsDir)
		}

		// D. Delete Workshop Temp (steamapps/workshop/temp/221100)
		workshopTempDir := filepath.Join(workshopRootDir, "temp", "221100")

		if _, err := os.Stat(workshopTempDir); err == nil {
			fmt.Printf("[App] Clearing Workshop Temp: %s\n", workshopTempDir)
			err := os.RemoveAll(workshopTempDir)
			if err != nil {
				fmt.Printf("[App] Failed to delete workshop temp dir %s: %v\n", workshopTempDir, err)
				errors++
			} else {
				fmt.Printf("[App] Deleted workshop temp dir: %s\n", workshopTempDir)
				count++
			}
		} else {
			fmt.Printf("[App] Workshop temp dir not found: %s\n", workshopTempDir)
		}

	} else {
		fmt.Println("[App] Warning: Could not determine workshop directory. fileschecks failed.")
		return map[string]interface{}{"success": false, "error": "Could not determine workshop directory"}, nil
	}

	return map[string]interface{}{"success": true, "count": count, "errors": errors, "method": "fs-delete"}, nil
}

// getWorkshopPath tries to find the DayZ workshop content directory
func getWorkshopPath() string {
	// 1. Try Registry (SteamPath)
	// We execute "reg query" similar to nodejs sidecar, but pure Go has x/sys/windows/registry.
	// For simplicity and avoiding external deps, let's use the CLI method or just common paths first.
	// Actually, Wails app might be running in a context where we can just check common paths easily.

	commonPaths := []string{
		`C:\Program Files (x86)\Steam\steamapps\workshop\content\221100`,
		`C:\Program Files\Steam\steamapps\workshop\content\221100`,
		`D:\SteamLibrary\steamapps\workshop\content\221100`,
		`E:\SteamLibrary\steamapps\workshop\content\221100`,
		`F:\SteamLibrary\steamapps\workshop\content\221100`,
		`C:\Steam\steamapps\workshop\content\221100`,
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. Try to derive from executable path if possible?
	// (Not reliable if launcher is standalone)

	return ""
}

func (a *App) GetActiveDownloads() interface{} {
	if UseNativeSteamworks {
		// Poll subscribed items for download status
		items := steamworks.GetSubscribedItems()
		var activeDownloads []map[string]interface{}

		for _, id := range items {
			sid := fmt.Sprintf("%d", id)
			state := steamworks.GetItemState(sid)
			// Flags: 4=Installed, 8=NeedsUpdate, 16=Downloading, 32=Downloaded
			// We check for Downloading (16) or NeedsUpdate(8)

			isDownloading := (state & 16) != 0
			isQueued := (state&8) != 0 || ((state&1) != 0 && (state&4) == 0) // Subscribed but not installed

			if isDownloading || isQueued {

				current, total := steamworks.GetDownloadInfo(sid)
				var progress float64
				if total > 0 {
					progress = (float64(current) / float64(total)) * 100
				}

				status := "queued"
				if isDownloading {
					status = "downloading"
				}

				activeDownloads = append(activeDownloads, map[string]interface{}{
					"id":         sid,
					"name":       fmt.Sprintf("Mod %s", sid),
					"status":     status,
					"progress":   progress,
					"current":    current,
					"total":      total,
					"stateFlags": state,
				})
			}
		}

		name := steamworks.GetPersonaName()
		if len(activeDownloads) > 0 {
		}
		// Emulate Sidecar Event Structure
		return map[string]interface{}{
			"type": "download-update",
			"data": activeDownloads,
			"meta": map[string]interface{}{"connected": true, "name": name},
		}
	}
	return nil
}

func (a *App) CheckMod(modId string, verify bool) (interface{}, error) {
	state := steamworks.GetItemState(modId)
	status := "unknown"

	if (state & 16) != 0 {
		status = "downloading"
	} else if ((state & 8) != 0) || ((state & 32) != 0) {
		status = "queued"
	} else if (state & 4) != 0 {
		// Installed
		// Verification
		path := steamworks.ResolveModPath(modId)
		if path != "" && steamworks.IsModFolderValid(path) {
			status = "installed"
		} else {
			status = "unknown"
		}
	} else if (state & 1) != 0 {
		status = "queued"
	}

	var progress float64
	if status == "downloading" {
		c, t := steamworks.GetDownloadInfo(modId)
		if t > 0 {
			progress = (float64(c) / float64(t)) * 100
		}
	}

	return map[string]interface{}{"success": true, "status": status, "progress": progress, "stateFlags": state}, nil
}

func (a *App) FetchModDetails(modIds []string) (interface{}, error) {
	// Use Steam Web API to fetch details
	apiURL := "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"

	form := url.Values{}
	form.Add("itemcount", fmt.Sprintf("%d", len(modIds)))
	for i, id := range modIds {
		form.Add(fmt.Sprintf("publishedfileids[%d]", i), id)
	}

	resp, err := a.httpClient.PostForm(apiURL, form)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}

	type PublishedFileDetails struct {
		PublishedFileId string      `json:"publishedfileid"`
		Title           string      `json:"title"`
		FileSize        interface{} `json:"file_size"` // Can be string or int? usually int in JSON but string in form? API varies. sidecar says simple JSON parse.
		TimeUpdated     interface{} `json:"time_updated"`
	}
	type ResponseContainer struct {
		Response struct {
			PublishedFileDetails []PublishedFileDetails `json:"publishedfiledetails"`
		} `json:"response"`
	}

	var data ResponseContainer
	if err := json.Unmarshal(body, &data); err != nil {
		return map[string]interface{}{"success": false, "error": "JSON Parse Error: " + err.Error()}, nil
	}

	// Transform to match sidecar format (it just passes "details: []")
	// The sidecar mapped it: title, publishedfileid, etc.
	// Our struct does that.
	return map[string]interface{}{"success": true, "details": data.Response.PublishedFileDetails}, nil
}

func (a *App) OpenModFolder(modId string) (interface{}, error) {
	path := steamworks.ResolveModPath(modId)
	if path != "" {
		exec.Command("explorer", path).Start()
		return map[string]interface{}{"success": true}, nil
	}
	return map[string]interface{}{"success": false, "error": "Path not found"}, nil
}

func (a *App) DeleteMod(modId string) (interface{}, error) {
	err := steamworks.UnsubscribeMod(modId)
	// Force delete files
	path := steamworks.ResolveModPath(modId)
	if path != "" {
		os.RemoveAll(path)
	}
	return map[string]interface{}{"success": true}, err
}

func (a *App) GetDayZVersion() (interface{}, error) {
	// Use PowerShell to get DayZ version
	path := steamworks.GetAppInstallDir(221100)
	if path == "" {
		return map[string]interface{}{"success": false, "error": "DayZ not found"}, nil
	}
	exePath := filepath.Join(path, "DayZ_x64.exe")

	cmd := exec.Command("powershell", "-Command", fmt.Sprintf("(Get-Item '%s').VersionInfo.ProductVersion", exePath))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{"success": true, "version": strings.TrimSpace(string(out))}, nil
}

func (a *App) IcmpPing(ip string) (interface{}, error) {
	// Use Windows ping command for ICMP
	cmd := exec.Command("ping", "-n", "1", "-w", "2000", ip)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return map[string]interface{}{"success": false, "error": "unreachable"}, nil
	}
	// Parse output: "time=12ms" or "time<1ms"
	s := string(out)
	re := regexp.MustCompile(`time[=<](\d+)ms`)
	matches := re.FindStringSubmatch(s)
	latency := 0
	if len(matches) > 1 {
		fmt.Sscanf(matches[1], "%d", &latency)
		return map[string]interface{}{"success": true, "latency": latency}, nil
	}
	return map[string]interface{}{"success": false, "error": "timeout"}, nil
}

func (a *App) CheckTwitchStream(channel string) (interface{}, error) {
	return map[string]interface{}{"success": false, "error": "Disabled"}, nil
}

// -- SYSTEM METHODS --

// LaunchGame
func (a *App) LaunchGame(ip string, port int, mods []string, name string, launchParams string) (interface{}, error) {
	fmt.Printf("[App] LaunchGame Called: IP=%s Port=%d Mods=%d Name=%s Params=%s\n", ip, port, len(mods), name, launchParams)

	// Default to Steam Name if custom name is empty
	if name == "" {
		// Use cached name or get from native Steamworks
		if a.lastPersonaName != "" {
			name = a.lastPersonaName
		} else {
			name = steamworks.GetPersonaName()
		}

		if name != "" {
			fmt.Printf("[App] Using Steam Name: %s\n", name)
		}
	}

	// 1. Resolve Mods (while sidecar is running)
	var modStr, gamePath string

	if UseNativeSteamworks {
		gamePath = steamworks.GetAppInstallDir(221100)
		// Fallback paths
		if gamePath == "" {
			common := []string{
				`C:\Program Files (x86)\Steam\steamapps\common\DayZ`,
				`C:\Program Files\Steam\steamapps\common\DayZ`,
				`D:\SteamLibrary\steamapps\common\DayZ`,
			}
			for _, p := range common {
				if _, err := os.Stat(p); err == nil {
					gamePath = p
					break
				}
			}
		}

		if len(mods) > 0 {
			var modPaths []string
			for _, id := range mods {
				p := steamworks.ResolveModPath(id)
				if p != "" {
					modPaths = append(modPaths, p)
				}
			}
			if len(modPaths) > 0 {
				modStr = fmt.Sprintf(`"-mod=%s"`, filepath.Join(modPaths...))
				// Wait, Join joins with separator? No, filepath.Join uses path separator.
				// DayZ expects semi-colon separated list.
				// filepath.ListSeparator in windows is ';'.
				// But filepath.Join() joins path components (folder/subfolder).
				// We need strings.Join
				modStr = fmt.Sprintf(`"-mod=%s"`, joinMods(modPaths))
			}
		}
	}

	fmt.Printf("[App] Resolved Mods: %s | Game Path: %s\n", modStr, gamePath)

	// 3. Launch Logic
	if gamePath != "" {
		// DIRECT LAUNCH (Requested by User)
		exePath := filepath.Join(gamePath, "DayZ_BE.exe")
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			// Fallback to x64 if BE missing (unlikely but safe)
			exePath = filepath.Join(gamePath, "DayZ_x64.exe")
		}

		fmt.Printf("[App] Launching EXE: %s\n", exePath)

		// Construct Args
		args := []string{
			fmt.Sprintf("-connect=%s", ip),
			fmt.Sprintf("-port=%d", port),
			"-noSplash",
			"-noPause",
			"-skipIntro",
			"-world=empty",
			"-noBenchmark",
		}

		if name != "" {
			args = append(args, fmt.Sprintf("-name=%s", name))
		}

		if modStr != "" {
			unquote := regexp.MustCompile(`^"(.*)"$`)
			cleanMod := unquote.ReplaceAllString(modStr, "$1")
			args = append(args, cleanMod)
		}

		// Append Custom Launch Parameters (User Defined)
		if launchParams != "" {
			// Helper to check for existence
			contains := func(slice []string, item string) bool {
				for _, s := range slice {
					if s == item {
						return true
					}
				}
				return false
			}

			// Split by space to handle multiple params
			params := regexp.MustCompile(`\s+`).Split(launchParams, -1)
			for _, p := range params {
				if p != "" {
					// Deduplicate: Don't add if already in args
					if !contains(args, p) {
						args = append(args, p)
					} else {
						fmt.Printf("[App] Skipping duplicate param: %s\n", p)
					}
				}
			}
			fmt.Printf("[App] Custom Params Processed. Final Args: %v\n", args)
		}

		cmd := exec.Command(exePath, args...)
		cmd.Dir = gamePath // Important for BattlEye

		fmt.Printf("[App] Exec Arguments: %v\n", args)

		if err := cmd.Start(); err != nil {
			fmt.Printf("[App] Failed to launch EXE: %v\n", err)
			return nil, err
		}

	} else {
		// FALLBACK TO STEAM PROTOCOL (Old Method)
		fmt.Println("[App] Game path not found, falling back to Steam Protocol...")

		nameArg := ""
		if name != "" {
			nameArg = fmt.Sprintf(" \"-name=%s\"", name)
		}

		launchUrl := fmt.Sprintf("steam://run/221100//-connect=%s -port=%d%s -noSplash -noPause -skipIntro -world=empty -noBenchmark %s", ip, port, nameArg, modStr)
		fmt.Printf("[App] Launching URL: %s\n", launchUrl)

		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", launchUrl)
		if err := cmd.Start(); err != nil {
			fmt.Printf("[App] Failed to launch steam protocol: %v\n", err)
			return nil, err
		}
	}

	return map[string]interface{}{"success": true}, nil
}

func (a *App) VerifyServerMods(ip string, port int) (interface{}, error) {
	// Attempt 1: 2 second timeout
	res := dayz.VerifyMods(ip, port, 2)

	if res.Success {
		return res, nil
	}

	// Attempt 2: 3 second timeout
	res = dayz.VerifyMods(ip, port, 3)

	if res.Success {
		return res, nil
	}

	// Both attempts failed
	return map[string]interface{}{"success": false, "error": res.Error}, nil
}

// -- MAP CACHE METHODS --

func ensureMapCacheDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	cacheDir := filepath.Join(configDir, "dayz-launcher-go", "maps")
	os.MkdirAll(cacheDir, 0755)
	return cacheDir
}

// sanitizeMapName removes characters that are invalid in filenames
func sanitizeMapName(name string) string {
	// Keep alphanumeric, dash, underscore, space
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-\ ]+`)
	return reg.ReplaceAllString(name, "")
}

func (a *App) GetMapCache(mapName string) (map[string]interface{}, error) {
	if mapName == "" {
		return map[string]interface{}{"success": false}, nil
	}

	dir := ensureMapCacheDir()
	if dir == "" {
		return map[string]interface{}{"success": false, "error": "No cache dir"}, nil
	}

	cleanName := sanitizeMapName(mapName)
	path := filepath.Join(dir, cleanName+".jpg")

	// Check if exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return map[string]interface{}{"success": false}, nil
	}

	// Read
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}

	// Encode Base64
	b64 := base64.StdEncoding.EncodeToString(data)
	return map[string]interface{}{
		"success": true,
		"data":    "data:image/jpeg;base64," + b64,
	}, nil
}

func (a *App) SaveMapCache(mapName string, url string) (map[string]interface{}, error) {
	if mapName == "" || url == "" {
		return map[string]interface{}{"success": false}, nil
	}

	go func() {
		dir := ensureMapCacheDir()
		if dir == "" {
			return
		}

		cleanName := sanitizeMapName(mapName)
		path := filepath.Join(dir, cleanName+".jpg")

		// Download
		resp, err := http.Get(url)
		if err != nil {
			fmt.Println("Failed to download map img:", err)
			return
		}
		defer resp.Body.Close()

		file, err := os.Create(path)
		if err != nil {
			fmt.Println("Failed to create map file:", err)
			return
		}
		defer file.Close()

		io.Copy(file, resp.Body)
		fmt.Println("Cached map image:", cleanName)
	}()

	return map[string]interface{}{"success": true}, nil
}

// joinMods joins a slice of mod paths with the platform specific separator (DayZ expects semicolon)
func joinMods(paths []string) string {
	return strings.Join(paths, ";")
}
