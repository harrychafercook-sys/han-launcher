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
)

// IsConnected returns the current connection status
func IsConnected() bool {
	return isConnected
}

// Init connects to Discord RPC
func Init(clientId string) error {
	ConfiguredClientId = clientId
	err := client.Login(clientId)
	if err != nil {
		fmt.Printf("[Discord] Login failed: %v\n", err)
		return err
	}
	startTime = time.Now()
	isConnected = true
	fmt.Println("[Discord] Connected!")
	return nil
}

// UpdatePresence updates the user's status
func UpdatePresence(details, state, largeImage, largeText string) error {
	if !isConnected {
		return fmt.Errorf("discord not connected")
	}

	err := client.SetActivity(client.Activity{
		State:      state,
		Details:    details,
		LargeImage: largeImage,
		LargeText:  largeText,
		Timestamps: &client.Timestamps{
			Start: &startTime,
		},
	})

	if err != nil {
		fmt.Printf("[Discord] Update failed: %v\n", err)
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
