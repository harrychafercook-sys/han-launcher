package dayz

import (
	"fmt"
	"regexp"
	"time"

	"strings"

	"github.com/woozymasta/a2s/pkg/a2s"
	"github.com/woozymasta/a2s/pkg/a3sb"
)

// VerificationResult represents the result of a server scan
type VerificationResult struct {
	Success     bool   `json:"success"`
	Name        string `json:"name,omitempty"`
	IP          string `json:"ip,omitempty"`
	GamePort    int    `json:"gamePort,omitempty"`
	QueryPort   int    `json:"queryPort,omitempty"`
	Mods        []Mod  `json:"mods,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Discord     string `json:"discord,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Mod represents a single mod
type Mod struct {
	Name       string `json:"name"`
	WorkshopID string `json:"workshopId"`
}

// VerifyMods queries a DayZ server for its detailed information including mods and game port
func VerifyMods(ip string, port int, timeoutSeconds int) *VerificationResult {
	address := fmt.Sprintf("%s:%d", ip, port)

	// Create A2S Client
	client, err := a2s.NewWithString(address)
	if err != nil {
		return &VerificationResult{Success: false, Error: fmt.Sprintf("Error creating client: %v", err)}
	}
	defer client.Close()

	// Set timeout from parameter
	client.Timeout = time.Duration(timeoutSeconds) * time.Second

	// 1. Get Server Info
	info, err := client.GetInfo()
	if err != nil {
		return &VerificationResult{Success: false, Error: fmt.Sprintf("Error querying info: %v", err)}
	}

	// 2. Query Rules using A3SB (Wait for A2S to complete first to avoid conflict on single socket if shared, though new client manages it)
	a3sbClient := &a3sb.Client{Client: client}
	rules, err := a3sbClient.GetRules(221100)
	if err != nil {
		return &VerificationResult{Success: false, Error: fmt.Sprintf("Error querying rules: %v", err)}
	}

	// 3. Process Mods
	var outputMods []Mod
	if rules != nil && len(rules.Mods) > 0 {
		for _, m := range rules.Mods {
			outputMods = append(outputMods, Mod{
				Name:       m.Name,
				WorkshopID: fmt.Sprintf("%d", m.ID),
			})
		}
	}
	if outputMods == nil {
		outputMods = []Mod{}
	}

	// 4. Extract Description & Discord
	var description, discordLink string
	if rules != nil {
		description = rules.Description

		// Regex for Discord
		re := regexp.MustCompile(`(?:https?://)?(?:www\.)?(?:discord\.gg|discord\.com/invite)/[a-zA-Z0-9-]+`)
		if match := re.FindString(description); match != "" {
			if !strings.HasPrefix(match, "http") {
				match = "https://" + match
			}
			discordLink = match
		}
	}

	// 5. Construct Response
	// info.Port is expected to be the GamePort in standard A2S_INFO responses (unless it's a proxy)
	return &VerificationResult{
		Success:     true,
		Name:        info.Name,
		IP:          ip,
		GamePort:    int(info.Port),
		QueryPort:   port,
		Mods:        outputMods,
		Version:     info.Version,
		Description: description,
		Discord:     discordLink,
	}
}
