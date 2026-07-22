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
	SteamID     uint64 `json:"steamId,string"`
	Name        string `json:"name"`
	IsOnline    bool   `json:"isOnline"`
	IsPlaying   bool   `json:"isPlaying"`
	GameName    string `json:"gameName"` // Only set if IsPlaying is true
	GameAddress string `json:"gameAddress"`
	GamePort    uint16 `json:"gamePort"`
	QueryPort   uint16 `json:"queryPort"`
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

// GetFriendsConnectionState mirrors the state check used before exposing the
// Friends list. Checking the local Steam ID first has proven more reliable
// than treating successful Steamworks initialization as Friends connectivity.
func GetFriendsConnectionState() int {
	if !initialized || ptrSteamFriends == 0 || f_GetFriendCount == nil {
		return 0
	}

	myID := GetSteamID()
	if myID != 0 {
		return GetFriendPersonaState(myID)
	}
	return GetPersonaState()
}

// GetFriends returns a list of friends playing DayZ or just online
func GetFriends() []SteamFriend {
	if !initialized || ptrSteamFriends == 0 || f_GetFriendCount == nil {
		return []SteamFriend{}
	}

	if GetFriendsConnectionState() == 0 {
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
		var gamePort, queryPort uint16

		if isPlaying {
			if gameInfo.GameID == dayzAppID {
				gameName = "DayZ"
				if gameInfo.GameIP != 0 {
					// Convert IP
					ip := make(net.IP, 4)
					binary.BigEndian.PutUint32(ip, gameInfo.GameIP)
					gameAddress = fmt.Sprintf("%s:%d", ip.String(), gameInfo.GamePort)
					gamePort = gameInfo.GamePort
					queryPort = gameInfo.QueryPort
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
			GamePort:    gamePort,
			QueryPort:   queryPort,
		})
	}

	return friends
}

// OpenChat opens the Steam overlay chat window for a specific user
func OpenChat(steamID uint64) {
	if !initialized {
		fmt.Println("[Steamworks] OpenChat: Not initialized")
		return
	}
	if ptrSteamFriends == 0 {
		fmt.Println("[Steamworks] OpenChat: ptrSteamFriends is null")
		return
	}
	if f_ActivateGameOverlayToUser == nil {
		fmt.Println("[Steamworks] OpenChat: f_ActivateGameOverlayToUser is nil")
		return
	}
	fmt.Printf("[Steamworks] OpenChat: Activating 'chat' overlay for %d\n", steamID)
	f_ActivateGameOverlayToUser(ptrSteamFriends, "chat", steamID)
}

// GetFriendPersonaName returns the name for a given SteamID string
// wrapper for f_GetFriendPersonaName
func GetFriendPersonaName(steamIDString string) string {
	if !initialized || ptrSteamFriends == 0 || f_GetFriendPersonaName == nil {
		return ""
	}
	var steamID uint64
	// Parse string to uint64
	fmt.Sscanf(steamIDString, "%d", &steamID)

	if steamID == 0 {
		return ""
	}

	return f_GetFriendPersonaName(ptrSteamFriends, steamID)
}
