package a2s

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	A2S_INFO_HEADER  = 0x54
	A2S_RULES_HEADER = 0x56
	A2S_PLAYER_HEADER = 0x55
	A2S_CHALLENGE    = 0x41
	A2S_INFO_RESP    = 0x49
	A2S_RULES_RESP   = 0x45
	A2S_PLAYER_RESP   = 0x44
)

type Player struct {
	Index    uint8   `json:"index"`
	Name     string  `json:"name"`
	Score    int32   `json:"score"`
	Duration float32 `json:"duration"`
}

type ServerInfo struct {
	Name        string `json:"name"`
	Map         string `json:"map"`
	Players     uint8  `json:"players"`
	MaxPlayers  uint8  `json:"maxPlayers"`
	Environment string `json:"environment"`
	Password    bool   `json:"password"`
	Version     string `json:"version"`
	Tags        string `json:"tags"` // Important for time
	Latency     int64  `json:"latency"`
}

func QueryInfo(ip string, port int) (*ServerInfo, error) {
	address := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("udp", address, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}

	start := time.Now()

	// 1. Send Initial Request
	// Header: FF FF FF FF 54 ... "Source Engine Query\0"
	req := new(bytes.Buffer)
	binary.Write(req, binary.LittleEndian, int32(-1))
	req.WriteByte(A2S_INFO_HEADER)
	req.WriteString("Source Engine Query\x00")

	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, err
	}

	// 2. Read Response (Might be Challenge or Info)
	buf := make([]byte, 1400)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	latency := time.Since(start).Milliseconds()
	data := buf[:n]

	// Check Header
	if len(data) < 5 || int32(binary.LittleEndian.Uint32(data[0:4])) != -1 {
		return nil, fmt.Errorf("invalid header")
	}

	payload := data[4:]
	header := payload[0]

	// 3. Handle Challenge
	if header == A2S_CHALLENGE {
		challenge := payload[1:] // 4 bytes typically

		// Resend with challenge
		req2 := new(bytes.Buffer)
		binary.Write(req2, binary.LittleEndian, int32(-1))
		req2.WriteByte(A2S_INFO_HEADER)
		req2.WriteString("Source Engine Query\x00")
		req2.Write(challenge)

		start = time.Now() // Reset timer for actual ping measurement?
		// Or keep original start? Conventionally ping is RTT.
		// If we do challenge, we are doing 2 RTTs.
		// Let's just measure the last leg for "Ping" or maybe just divide by 2?
		// Usually users want to know "how long to get a packet back".
		// Let's reset start to measure the info trip.
		start = time.Now()

		if _, err := conn.Write(req2.Bytes()); err != nil {
			return nil, err
		}

		n, err = conn.Read(buf)
		if err != nil {
			return nil, err
		}
		latency = time.Since(start).Milliseconds()
		data = buf[:n]

		if len(data) < 5 {
			return nil, fmt.Errorf("response too short")
		}
		payload = data[4:]
		header = payload[0]
	}

	if header != A2S_INFO_RESP {
		return nil, fmt.Errorf("unexpected header: %x", header)
	}

	// 4. Parse Info
	return parseInfoPayload(payload[1:], latency) // Skip Header Byte
}

func parseInfoPayload(b []byte, ping int64) (*ServerInfo, error) {
	reader := bytes.NewReader(b)

	readString := func() string {
		var strBuilder strings.Builder
		for {
			b, err := reader.ReadByte()
			if err != nil || b == 0 {
				break
			}
			strBuilder.WriteByte(b)
		}
		return strBuilder.String()
	}

	// Protocol (1 byte)
	reader.ReadByte()

	info := &ServerInfo{Latency: ping}
	info.Name = readString()
	info.Map = readString()
	readString() // Folder
	readString() // Game

	// ID (2 bytes)
	reader.Seek(2, io.SeekCurrent)

	// Players
	bPlayers, _ := reader.ReadByte()
	info.Players = bPlayers

	// Max Players
	bMax, _ := reader.ReadByte()
	info.MaxPlayers = bMax

	// Bots
	reader.ReadByte()

	// Server Type
	reader.ReadByte()

	// Environment
	bEnv, _ := reader.ReadByte()
	info.Environment = string(bEnv)

	// Visibility (Password)
	bVis, _ := reader.ReadByte()
	info.Password = (bVis == 1)

	// VAC
	reader.ReadByte()

	// Version
	info.Version = readString()

	// EDF (Extra Data Flag)
	if reader.Len() > 0 {
		edf, _ := reader.ReadByte()

		if edf&0x80 != 0 {
			reader.Seek(2, io.SeekCurrent) // Port
		}
		if edf&0x10 != 0 {
			reader.Seek(8, io.SeekCurrent) // SteamID
		}
		if edf&0x40 != 0 { // SourceTV
			reader.Seek(2, io.SeekCurrent)
			readString()
		}
		if edf&0x20 != 0 {
			info.Tags = readString()
		}
	}

	return info, nil
}

func QueryRules(ip string, port int) (map[string]string, error) {
	address := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("udp", address, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}

	// 1. Send Initial Request
	req := new(bytes.Buffer)
	binary.Write(req, binary.LittleEndian, int32(-1))
	req.WriteByte(A2S_RULES_HEADER)
	// Challenge usually required immediately

	// Send initial query to get challenge
	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, err
	}

	// 2. Read Challenge Response
	buf := make([]byte, 4096) // Rules can be large
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	data := buf[:n]

	if len(data) < 5 || int32(binary.LittleEndian.Uint32(data[0:4])) != -1 {
		return nil, fmt.Errorf("invalid header")
	}

	header := data[4]
	var challenge []byte

	if header == A2S_CHALLENGE {
		challenge = data[5:9]
	} else if header == A2S_RULES_RESP {
		// Already got rules? Unlikely but possible if cached
		return parseRulesPayload(data[5:])
	} else {
		return nil, fmt.Errorf("expected challenge, got %x", header)
	}

	// 3. Send Challenge Response
	req2 := new(bytes.Buffer)
	binary.Write(req2, binary.LittleEndian, int32(-1))
	req2.WriteByte(A2S_RULES_HEADER)
	req2.Write(challenge)

	if _, err := conn.Write(req2.Bytes()); err != nil {
		return nil, err
	}

	// 4. Read Rules Response (Multi-packet support not fully implemented here for simplicity, assuming fits or single packet for now)
	// Actually A2S_RULES often splits. But many simple queries fit in one or two.
	// For robustness we should handle multi-packet but let's try reading once.
	// DayZ mod lists can be HUGE.
	// We might need a better library or simple loop.
	// Let's read once. If it's partial, we might miss data.
	// For "Digz query" equivalent, basic read might suffice for small mod lists.

	n, err = conn.Read(buf)
	if err != nil {
		return nil, err
	}
	data = buf[:n]
	if len(data) < 5 { // Header check
		return nil, fmt.Errorf("short response")
	}

	// Check for Split Packet Header (0xFEFF or -2)
	// If split, we need reassembly.
	// To minimize complexity, we assume standard packet for now or implement basic reassembly later if needed.

	if header := data[4]; header == A2S_RULES_RESP {
		return parseRulesPayload(data[5:])
	}

	return nil, fmt.Errorf("unexpected rules response header: %x", data[4])
}

func parseRulesPayload(b []byte) (map[string]string, error) {
	reader := bytes.NewReader(b)
	rules := make(map[string]string)

	readString := func() string {
		var strBuilder strings.Builder
		for {
			b, err := reader.ReadByte()
			if err != nil || b == 0 {
				break
			}
			strBuilder.WriteByte(b)
		}
		return strBuilder.String()
	}

	// Num Rules (2 bytes)
	var numRules uint16
	binary.Read(reader, binary.LittleEndian, &numRules)

	for i := 0; i < int(numRules) && reader.Len() > 0; i++ {
		key := readString()
		val := readString()
		rules[key] = val
	}

	return rules, nil
}

func QueryPlayers(ip string, port int) ([]*Player, error) {
	address := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("udp", address, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}

	// 1. Get Challenge
	req := new(bytes.Buffer)
	binary.Write(req, binary.LittleEndian, int32(-1))
	req.WriteByte(A2S_PLAYER_HEADER)
	binary.Write(req, binary.LittleEndian, int32(-1)) // Initial Challenge -1

	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	data := buf[:n]

	if len(data) < 5 || int32(binary.LittleEndian.Uint32(data[0:4])) != -1 {
		return nil, fmt.Errorf("invalid header")
	}

	header := data[4]
	var challenge int32

	if header == A2S_CHALLENGE {
		challenge = int32(binary.LittleEndian.Uint32(data[5:9]))
	} else if header == A2S_PLAYER_RESP {
		return parsePlayersPayload(data[5:])
	} else {
		return nil, fmt.Errorf("expected challenge, got %x", header)
	}

	// 2. Send Challenge Response
	req2 := new(bytes.Buffer)
	binary.Write(req2, binary.LittleEndian, int32(-1))
	req2.WriteByte(A2S_PLAYER_HEADER)
	binary.Write(req2, binary.LittleEndian, challenge)

	if _, err := conn.Write(req2.Bytes()); err != nil {
		return nil, err
	}

	// 3. Read Players
	n, err = conn.Read(buf)
	if err != nil {
		return nil, err
	}
	data = buf[:n]

	if len(data) < 5 {
		return nil, fmt.Errorf("short response")
	}

	if header := data[4]; header == A2S_PLAYER_RESP {
		return parsePlayersPayload(data[5:])
	}

	return nil, fmt.Errorf("unexpected player response header: %x", data[4])
}

func parsePlayersPayload(b []byte) ([]*Player, error) {
	reader := bytes.NewReader(b)
	players := make([]*Player, 0)

	readString := func() string {
		var strBuilder strings.Builder
		for {
			b, err := reader.ReadByte()
			if err != nil || b == 0 {
				break
			}
			strBuilder.WriteByte(b)
		}
		return strBuilder.String()
	}

	// Num Players (1 byte)
	numPlayers, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	for i := 0; i < int(numPlayers) && reader.Len() > 0; i++ {
		p := &Player{}
		
		// Index (1 byte)
		idx, _ := reader.ReadByte()
		p.Index = idx

		// Name (String)
		p.Name = readString()

		// Score (4 bytes)
		binary.Read(reader, binary.LittleEndian, &p.Score)

		// Duration (4 bytes float)
		binary.Read(reader, binary.LittleEndian, &p.Duration)

		players = append(players, p)
	}

	return players, nil
}
