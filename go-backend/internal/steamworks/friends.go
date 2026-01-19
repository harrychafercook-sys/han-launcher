package steamworks

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	k_EFriendFlagImmediate = 0x04
)

// FriendGameInfo_t matches the C++ struct layout for FriendGameInfo_t
// Defined in ISteamFriends.h
type FriendGameInfo_t struct {
	GameID       uint64
	GameIP       uint32
	GamePort     uint16
	QueryPort    uint16
	SteamIDLobby uint64
}

// SteamFriend info struct for frontend
type SteamFriend struct {
	SteamID     uint64 `json:"steamId"`
	Name        string `json:"name"`
	IsOnline    bool   `json:"isOnline"`
	IsPlaying   bool   `json:"isPlaying"`
	GameName    string `json:"gameName"` // Only set if IsPlaying is true
	GameAddress string `json:"gameAddress"`
}

var (
	// Friends Interface Functions
	f_GetFriendCount        func(uintptr, int) int
	f_GetFriendByIndex      func(uintptr, int, int) uint64
	f_GetFriendPersonaName  func(uintptr, uint64) string
	f_GetFriendGamePlayed   func(uintptr, uint64, *FriendGameInfo_t) bool
	f_GetFriendPersonaState func(uintptr, uint64) int
)

// Helper to convert uint32 IP to string (Big Endian usually for network, but Valve sends Host order/Little Endian often?
// Actually Steam IPs are usually uint32. Let's try standard conversion.)
func int2ip(nn uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip.String()
}

// GetFriends returns a list of friends playing DayZ or just online
func GetFriends() []SteamFriend {
	if !initialized || ptrSteamFriends == 0 || f_GetFriendCount == nil {
		return []SteamFriend{}
	}

	// Check myself using GetFriendPersonaState on my own SteamID
	myID := GetSteamID()

	// If we have a valid ID, trust its state (even if 0/Offline)
	// If ID is 0, we might strictly fail or try global fallback (which is buggy, but maybe better than nothing?)
	// Given global fallback returns 1 (Online) falsely, it's better to default to 0 if ID is missing?
	// But let's stick to the plan: Trust GetFriendPersonaState(myID).

	state := 0
	if myID != 0 {
		state = GetFriendPersonaState(myID)
	} else {
		// Fallback: If we can't get ID, try global state (legacy behavior)
		state = GetPersonaState()
	}

	if state == 0 {
		return []SteamFriend{}
	}

	friendCount := f_GetFriendCount(ptrSteamFriends, k_EFriendFlagImmediate)
	friends := make([]SteamFriend, 0, friendCount)

	for i := 0; i < friendCount; i++ {
		steamID := f_GetFriendByIndex(ptrSteamFriends, i, k_EFriendFlagImmediate)
		name := f_GetFriendPersonaName(ptrSteamFriends, steamID)

		var gameInfo FriendGameInfo_t
		isPlaying := f_GetFriendGamePlayed(ptrSteamFriends, steamID, &gameInfo)

		state := 0
		if f_GetFriendPersonaState != nil {
			state = f_GetFriendPersonaState(ptrSteamFriends, steamID)
		}

		// EPersonaState 0 = Offline. Anything else is some form of online (Online, Busy, Away, Snooze, etc.)
		isOnline := state != 0

		dayzAppID := uint64(221100)
		gameName := ""
		gameAddress := ""

		if isPlaying {
			if gameInfo.GameID == dayzAppID {
				gameName = "DayZ"
				if gameInfo.GameIP != 0 {
					// Convert IP
					ip := make(net.IP, 4)
					binary.LittleEndian.PutUint32(ip, gameInfo.GameIP)
					gameAddress = fmt.Sprintf("%s:%d", ip.String(), gameInfo.GamePort)
				}
			} else {
				gameName = "Other Game"
			}
		}

		friends = append(friends, SteamFriend{
			SteamID:     steamID,
			Name:        name,
			IsOnline:    isOnline,
			IsPlaying:   isPlaying,
			GameName:    gameName,
			GameAddress: gameAddress,
		})
	}

	return friends
}
