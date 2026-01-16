package discord

import (
	"fmt"
	"time"

	"github.com/hugolgst/rich-go/client"
)

var (
	ConfiguredClientId string
	startTime          time.Time
	isConnected        bool

	// Cache for heartbeat
	lastDetails    string
	lastState      string
	lastLargeImage string
	lastLargeText  string
)

// IsConnected returns the current connection status
func IsConnected() bool {
	return isConnected
}

// Init connects to Discord RPC
func Init(clientId string) error {
	ConfiguredClientId = clientId
	// Try to logout first to ensure clean state if previously failed
	if !isConnected {
		client.Logout()
	}

	err := client.Login(clientId)
	if err != nil {
		fmt.Printf("[Discord] Login failed: %v\n", err)
		// Ensure we are marked as disconnected
		isConnected = false
		return err
	}
	startTime = time.Now()
	isConnected = true
	fmt.Println("[Discord] Connected!")
	return nil
}

// UpdatePresence updates the user's status
func UpdatePresence(details, state, largeImage, largeText string) error {
	// Cache the values for heartbeat
	lastDetails = details
	lastState = state
	lastLargeImage = largeImage
	lastLargeText = largeText

	if !isConnected {
		return fmt.Errorf("discord not connected")
	}

	return sendActivity()
}

// Refresh sends the last cached activity to verify connection (Heartbeat)
func Refresh() error {
	if !isConnected {
		return fmt.Errorf("discord not connected")
	}
	// If we haven't set an activity yet, do nothing or send default?
	if lastDetails == "" {
		return nil
	}
	return sendActivity()
}

func sendActivity() error {
	err := client.SetActivity(client.Activity{
		State:      lastState,
		Details:    lastDetails,
		LargeImage: lastLargeImage,
		LargeText:  lastLargeText,
		Timestamps: &client.Timestamps{
			Start: &startTime,
		},
		Buttons: []*client.Button{
			{
				Label: "Get Han Launcher",
				Url:   "https://github.com/harrychafercook-sys/han-launcher",
			},
		},
	})

	if err != nil {
		fmt.Printf("[Discord] Update/Heartbeat failed: %v\n", err)
		// Mark as disconnected so the app loop knows to reconnect
		isConnected = false
		return err
	}
	return nil
}

// Close closes the Discord connection
func Close() {
	if isConnected {
		client.Logout()
		isConnected = false
	}
}

// DisconnectForced sets isConnected to false without calling Logout (useful if process died)
func DisconnectForced() {
	isConnected = false
}
