package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"dayz-launcher-go/internal/steamworks"
)

const adminAPIBase = "https://dayz-quiz.com/han-api/v1"
const adminStatusCacheDuration = 4 * time.Second

var adminStatusCache = struct {
	sync.Mutex
	steamID uint64
	value   bool
	expires time.Time
}{}

type adminAPIError struct {
	Error string `json:"error"`
}

type adminSessionResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expiresIn"`
}

// isAdminSteamAccount is a low-friction UI capability check. Every mutation
// is independently protected by a server-issued bearer token.
func (a *App) isAdminSteamAccount(steamID uint64) bool {
	if steamID == 0 {
		return false
	}

	adminStatusCache.Lock()
	if adminStatusCache.steamID == steamID && time.Now().Before(adminStatusCache.expires) {
		value := adminStatusCache.value
		adminStatusCache.Unlock()
		return value
	}
	adminStatusCache.Unlock()

	requestURL := adminAPIBase + "/admin/status?steamId=" + url.QueryEscape(strconv.FormatUint(steamID, 10))
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return false
	}
	request.Header.Set("User-Agent", "HAN-Launcher/3")
	response, err := a.httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var payload struct {
		IsAdmin bool `json:"isAdmin"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload) != nil {
		return false
	}

	adminStatusCache.Lock()
	adminStatusCache.steamID = steamID
	adminStatusCache.value = payload.IsAdmin
	adminStatusCache.expires = time.Now().Add(adminStatusCacheDuration)
	adminStatusCache.Unlock()
	return payload.IsAdmin
}

func (a *App) AuthenticateAdmin(passphrase string) map[string]interface{} {
	steamID := steamworks.GetSteamID()
	if steamID == 0 || !steamworks.IsInitialized() {
		return map[string]interface{}{"success": false, "error": "Connect Steam before authenticating"}
	}
	payload := map[string]string{
		"steamId":  strconv.FormatUint(steamID, 10),
		"password": passphrase,
	}
	var session adminSessionResponse
	if err := a.adminRequest(http.MethodPost, "/admin/session", payload, "", &session); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	if session.Token == "" {
		return map[string]interface{}{"success": false, "error": "Server returned an invalid session"}
	}
	if err := storeAdminToken(session.Token); err != nil {
		return map[string]interface{}{"success": false, "error": "Could not securely save the admin session"}
	}
	return map[string]interface{}{"success": true, "expiresIn": session.ExpiresIn}
}

func (a *App) CreatePlaytest(payload map[string]interface{}) map[string]interface{} {
	return a.adminMutation(http.MethodPost, "/playtests", payload)
}

func (a *App) RemovePlaytest(id string) map[string]interface{} {
	if len(id) != 36 {
		return map[string]interface{}{"success": false, "error": "This server has no removable playtest ID"}
	}
	return a.adminMutation(http.MethodDelete, "/playtests/"+url.PathEscape(id), nil)
}

func (a *App) adminMutation(method, path string, payload interface{}) map[string]interface{} {
	token, err := loadAdminToken()
	if err != nil || token == "" {
		return map[string]interface{}{"success": false, "needsAuthentication": true, "error": "Admin passphrase required"}
	}
	result := make(map[string]interface{})
	if err := a.adminRequest(method, path, payload, token, &result); err != nil {
		if strings.Contains(err.Error(), "authentication required") || strings.Contains(err.Error(), "expired or invalid") {
			_ = deleteAdminToken()
			return map[string]interface{}{"success": false, "needsAuthentication": true, "error": "Admin session expired"}
		}
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	result["success"] = true
	return result
}

func (a *App) adminRequest(method, path string, payload interface{}, token string, target interface{}) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, adminAPIBase+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HAN-Launcher/3")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("admin service unavailable")
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 64*1024)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError adminAPIError
		if json.NewDecoder(limited).Decode(&apiError) == nil && apiError.Error != "" {
			return fmt.Errorf("%s", strings.ToLower(apiError.Error))
		}
		return fmt.Errorf("admin service returned HTTP %d", response.StatusCode)
	}
	if target != nil {
		if err := json.NewDecoder(limited).Decode(target); err != nil {
			return fmt.Errorf("invalid response from admin service")
		}
	}
	return nil
}
