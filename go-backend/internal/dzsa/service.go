package dzsa

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const serverListURL = "https://dayzsalauncher.com/api/v2/launcher/servers/dayz"

type endpoint struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type mod struct {
	Name            string          `json:"name"`
	SteamWorkshopID json.RawMessage `json:"steamWorkshopId"`
}

type rawServer struct {
	GamePort        int      `json:"gamePort"`
	Endpoint        endpoint `json:"endpoint"`
	Name            string   `json:"name"`
	Map             string   `json:"map"`
	Players         int      `json:"players"`
	MaxPlayers      int      `json:"maxPlayers"`
	Password        bool     `json:"password"`
	Version         string   `json:"version"`
	Mission         string   `json:"mission"`
	FirstPersonOnly bool     `json:"firstPersonOnly"`
	Time            string   `json:"time"`
	Mods            []mod    `json:"mods"`
	Provider        string   `json:"_hanProvider,omitempty"`
	ExternalID      int64    `json:"_hanExternalId,omitempty"`
	Country         string   `json:"_hanCountry,omitempty"`
}

type ImportedMod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ImportedServer struct {
	ExternalID      int64         `json:"externalId"`
	Name            string        `json:"name"`
	IP              string        `json:"ip"`
	GamePort        int           `json:"gamePort"`
	QueryPort       int           `json:"queryPort"`
	Map             string        `json:"map"`
	Players         int           `json:"players"`
	MaxPlayers      int           `json:"maxPlayers"`
	Password        bool          `json:"password"`
	Version         string        `json:"version"`
	Time            string        `json:"time"`
	FirstPersonOnly bool          `json:"firstPersonOnly"`
	Country         string        `json:"country"`
	Mods            []ImportedMod `json:"mods"`
}

type payload struct {
	Created interface{} `json:"created"`
	Status  interface{} `json:"status"`
	Result  []rawServer `json:"result"`
}

type Server struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Attributes ServerAttributes `json:"attributes"`
}

type ServerAttributes struct {
	Source        string        `json:"source"`
	Provider      string        `json:"provider"`
	Name          string        `json:"name"`
	IP            string        `json:"ip"`
	Port          int           `json:"port"`
	PortQuery     int           `json:"portQuery"`
	Players       int           `json:"players"`
	MaxPlayers    int           `json:"maxPlayers"`
	Rank          *int          `json:"rank"`
	Status        string        `json:"status"`
	Password      bool          `json:"password"`
	Country       string        `json:"country"`
	DayZMetricsID int64         `json:"dayzMetricsId,omitempty"`
	Details       ServerDetails `json:"details"`
}

type ServerDetails struct {
	Map         string   `json:"map"`
	Mission     string   `json:"mission"`
	Version     string   `json:"version"`
	Time        string   `json:"time"`
	Password    bool     `json:"password"`
	Official    bool     `json:"official"`
	ThirdPerson bool     `json:"third_person"`
	Modded      bool     `json:"modded"`
	ModNames    []string `json:"modNames"`
	ModIDs      []string `json:"modIds"`
}

type QueryResult struct {
	Success bool     `json:"success"`
	Servers []Server `json:"servers"`
	Total   int      `json:"total"`
	Offset  int      `json:"offset"`
	Limit   int      `json:"limit"`
	HasMore bool     `json:"hasMore"`
	Source  string   `json:"source"`
	Created string   `json:"created,omitempty"`
	Warning string   `json:"warning,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type Service struct {
	mu                 sync.Mutex
	client             *http.Client
	cachePath          string
	discoveryCachePath string
	metadataCachePath  string
	servers            []rawServer
	discoveries        []rawServer
	discoveriesLoaded  bool
	metadata           map[string]persistedServerMetadata
	metadataLoaded     bool
	created            string
	source             string
}

type persistedServerMetadata struct {
	IP              string `json:"ip"`
	GamePort        int    `json:"gamePort"`
	QueryPort       int    `json:"queryPort"`
	ExternalID      int64  `json:"dayzMetricsId,omitempty"`
	Country         string `json:"country,omitempty"`
	FirstPersonOnly *bool  `json:"firstPersonOnly,omitempty"`
	UpdatedAtUnix   int64  `json:"updatedAtUnix"`
}

func NewService(client *http.Client) *Service {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		configDir = os.TempDir()
	}

	return &Service{
		client:             client,
		cachePath:          filepath.Join(configDir, "HAN Launcher", "cache", "dzsa_servers.json"),
		discoveryCachePath: filepath.Join(configDir, "HAN Launcher", "cache", "dayzmetrics_discoveries.json"),
		metadataCachePath:  filepath.Join(configDir, "HAN Launcher", "cache", "dayzmetrics_server_metadata.json"),
	}
}

func (s *Service) Query(query string, filters map[string]interface{}, offset, limit int, refresh bool) QueryResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadDiscoveriesOnce()
	s.loadMetadataOnce()

	warning := ""
	if refresh || len(s.servers) == 0 {
		if err := s.loadFresh(); err != nil {
			warning = fmt.Sprintf("DZSA download failed: %v", err)
			if len(s.servers) == 0 {
				if cacheErr := s.loadCache(); cacheErr != nil {
					return QueryResult{
						Success: false,
						Servers: []Server{},
						Error:   fmt.Sprintf("%s; cached list unavailable: %v", warning, cacheErr),
					}
				}
			}
		}
	}

	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	pool := s.servers
	if normalizeName(query) != "" {
		pool = mergedSearchPool(s.servers, s.discoveries)
	}
	matches := make([]rawServer, 0, len(pool))
	for _, server := range pool {
		if matchesQuery(server, query) && matchesFilters(server, filters) {
			matches = append(matches, server)
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Players != matches[j].Players {
			return matches[i].Players > matches[j].Players
		}
		return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
	})

	total := len(matches)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	servers := make([]Server, 0, end-offset)
	for _, server := range matches[offset:end] {
		servers = append(servers, normalize(server))
	}

	return QueryResult{
		Success: true,
		Servers: servers,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: end < total,
		Source:  s.source,
		Created: s.created,
		Warning: warning,
	}
}

// FindKnownByName checks DZSA first, then the persisted discovery overlay.
// It deliberately does not expose the overlay to the normal empty-query list.
func (s *Service) FindKnownByName(name string) (Server, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadDiscoveriesOnce()
	s.loadMetadataOnce()

	needle := normalizeName(name)
	if needle == "" {
		return Server{}, false
	}
	for _, server := range s.servers {
		if normalizeName(server.Name) == needle {
			return normalize(server), true
		}
	}
	for _, server := range s.discoveries {
		if normalizeName(server.Name) == needle {
			return normalize(server), true
		}
	}
	return Server{}, false
}

// MergeDayZMetricsServers stores genuinely new DayZMetrics discoveries while
// always preferring DZSA when either the normalized name or endpoint matches.
func (s *Service) MergeDayZMetricsServers(imported []ImportedServer) []Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadDiscoveriesOnce()
	s.loadMetadataOnce()

	result := make([]Server, 0, len(imported))
	changed := false
	for _, candidate := range imported {
		if candidate.IP == "" || candidate.GamePort <= 0 || candidate.QueryPort <= 0 || normalizeName(candidate.Name) == "" {
			continue
		}

		if existing, ok := findByNameOrEndpoint(s.servers, candidate.Name, candidate.IP, candidate.GamePort, candidate.QueryPort); ok {
			result = append(result, normalize(existing))
			continue
		}
		if existing, ok := findByNameOrEndpoint(s.discoveries, candidate.Name, candidate.IP, candidate.GamePort, candidate.QueryPort); ok {
			result = append(result, normalize(existing))
			continue
		}

		mods := make([]mod, 0, len(candidate.Mods))
		for _, item := range candidate.Mods {
			id, _ := json.Marshal(item.ID)
			mods = append(mods, mod{Name: item.Name, SteamWorkshopID: id})
		}
		server := rawServer{
			GamePort:        candidate.GamePort,
			Endpoint:        endpoint{IP: candidate.IP, Port: candidate.QueryPort},
			Name:            candidate.Name,
			Map:             candidate.Map,
			Players:         candidate.Players,
			MaxPlayers:      candidate.MaxPlayers,
			Password:        candidate.Password,
			Version:         candidate.Version,
			FirstPersonOnly: candidate.FirstPersonOnly,
			Time:            candidate.Time,
			Mods:            mods,
			Provider:        "dayzmetrics",
			ExternalID:      candidate.ExternalID,
			Country:         candidate.Country,
		}
		s.discoveries = append(s.discoveries, server)
		result = append(result, normalize(server))
		changed = true
	}

	if changed {
		if err := s.writeDiscoveryCache(); err != nil {
			fmt.Printf("[DZSA] Could not update DayZMetrics discovery cache: %v\n", err)
		}
	}
	return result
}

func (s *Service) loadFresh() error {
	req, err := http.NewRequest(http.MethodGet, serverListURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HAN-Launcher/2.2 (+https://hanlauncher.com)")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}

	parsed, err := parsePayload(body)
	if err != nil {
		return err
	}

	s.servers = parsed.Result
	s.applyMetadataToAllServers()
	s.created = stringifyCreated(parsed.Created)
	s.source = "network"
	if err := s.writeCurrentServerCache(parsed.Status); err != nil {
		// A valid network list is still usable even if the cache cannot be updated.
		fmt.Printf("[DZSA] Could not update cache: %v\n", err)
	}
	return nil
}

func (s *Service) loadCache() error {
	body, err := os.ReadFile(s.cachePath)
	if err != nil {
		return err
	}
	parsed, err := parsePayload(body)
	if err != nil {
		return err
	}
	s.servers = parsed.Result
	s.loadMetadataOnce()
	s.applyMetadataToAllServers()
	s.created = stringifyCreated(parsed.Created)
	s.source = "cache"
	return nil
}

func parsePayload(body []byte) (payload, error) {
	var parsed payload
	if err := json.Unmarshal(body, &parsed); err != nil {
		return payload{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(parsed.Result) == 0 {
		return payload{}, errors.New("server list was empty")
	}
	return parsed, nil
}

func (s *Service) writeCache(body []byte) error {
	return writeAtomic(s.cachePath, "dzsa_servers_*.tmp", body)
}

func (s *Service) writeCurrentServerCache(status interface{}) error {
	if status == nil {
		status = "cached"
	}
	body, err := json.Marshal(payload{
		Created: s.created,
		Status:  status,
		Result:  s.servers,
	})
	if err != nil {
		return err
	}
	return s.writeCache(body)
}

func metadataKey(ip string, gamePort, queryPort int) string {
	ip = strings.ToLower(strings.TrimSpace(ip))
	if ip == "" {
		return ""
	}
	ports := make([]int, 0, 2)
	if gamePort > 0 {
		ports = append(ports, gamePort)
	}
	if queryPort > 0 && queryPort != gamePort {
		ports = append(ports, queryPort)
	}
	if len(ports) == 0 {
		return ""
	}
	sort.Ints(ports)
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, strconv.Itoa(port))
	}
	return ip + "|" + strings.Join(parts, ",")
}

func normalizeCountry(value string) string {
	country := strings.ToUpper(strings.TrimSpace(value))
	if len(country) != 2 {
		return ""
	}
	for _, char := range country {
		if char < 'A' || char > 'Z' {
			return ""
		}
	}
	return country
}

func (s *Service) loadMetadataOnce() {
	if s.metadataLoaded {
		return
	}
	s.metadataLoaded = true
	s.metadata = make(map[string]persistedServerMetadata)

	body, err := os.ReadFile(s.metadataCachePath)
	if err == nil {
		var stored map[string]persistedServerMetadata
		if json.Unmarshal(body, &stored) == nil && stored != nil {
			s.metadata = stored
		}
	}
	s.applyMetadataToAllServers()
}

func (s *Service) metadataFor(server rawServer) (persistedServerMetadata, bool) {
	if len(s.metadata) == 0 {
		return persistedServerMetadata{}, false
	}
	if exact, ok := s.metadata[metadataKey(server.Endpoint.IP, server.GamePort, server.Endpoint.Port)]; ok {
		return exact, true
	}
	for _, candidate := range s.metadata {
		if !strings.EqualFold(strings.TrimSpace(candidate.IP), strings.TrimSpace(server.Endpoint.IP)) {
			continue
		}
		if (candidate.GamePort > 0 && candidate.GamePort == server.GamePort) ||
			(candidate.QueryPort > 0 && candidate.QueryPort == server.Endpoint.Port) {
			return candidate, true
		}
	}
	return persistedServerMetadata{}, false
}

func (s *Service) applyMetadata(server *rawServer) bool {
	if server == nil {
		return false
	}
	metadata, ok := s.metadataFor(*server)
	if !ok {
		return false
	}
	changed := false
	if country := normalizeCountry(metadata.Country); country != "" && server.Country != country {
		server.Country = country
		changed = true
	}
	if metadata.ExternalID > 0 && server.ExternalID != metadata.ExternalID {
		server.ExternalID = metadata.ExternalID
		changed = true
	}
	if metadata.FirstPersonOnly != nil && server.FirstPersonOnly != *metadata.FirstPersonOnly {
		server.FirstPersonOnly = *metadata.FirstPersonOnly
		changed = true
	}
	return changed
}

func (s *Service) applyMetadataToAllServers() {
	for index := range s.servers {
		s.applyMetadata(&s.servers[index])
	}
	for index := range s.discoveries {
		s.applyMetadata(&s.discoveries[index])
	}
}

func (s *Service) writeMetadataCache() error {
	body, err := json.MarshalIndent(s.metadata, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.metadataCachePath, "dayzmetrics_server_metadata_*.tmp", body)
}

// SaveDayZMetricsMetadata permanently enriches the local DZSA list by
// endpoint. The separate overlay survives future fresh DZSA downloads, while
// the current DZSA cache is also rewritten with the merged country and ID.
func (s *Service) SaveDayZMetricsMetadata(ip string, gamePort, queryPort int, externalID int64, country string, perspective int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadDiscoveriesOnce()
	s.loadMetadataOnce()

	key := metadataKey(ip, gamePort, queryPort)
	if key == "" {
		return errors.New("invalid server endpoint")
	}
	record := s.metadata[key]
	record.IP = strings.TrimSpace(ip)
	if gamePort > 0 {
		record.GamePort = gamePort
	}
	if queryPort > 0 {
		record.QueryPort = queryPort
	}
	if externalID > 0 {
		record.ExternalID = externalID
	}
	if normalized := normalizeCountry(country); normalized != "" {
		record.Country = normalized
	}
	if perspective == 0 || perspective == 1 {
		firstPersonOnly := perspective == 1
		record.FirstPersonOnly = &firstPersonOnly
	}
	if record.ExternalID <= 0 && record.Country == "" && record.FirstPersonOnly == nil {
		return errors.New("no DayZMetrics metadata supplied")
	}
	record.UpdatedAtUnix = time.Now().Unix()
	s.metadata[key] = record
	s.applyMetadataToAllServers()

	if err := s.writeMetadataCache(); err != nil {
		return err
	}
	if len(s.servers) > 0 {
		if err := s.writeCurrentServerCache(nil); err != nil {
			return err
		}
	}
	if len(s.discoveries) > 0 {
		if err := s.writeDiscoveryCache(); err != nil {
			return err
		}
	}
	return nil
}

func endpointSharesPort(ip string, gamePort, queryPort int, candidateIP string, candidateGamePort, candidateQueryPort int) bool {
	if !strings.EqualFold(strings.TrimSpace(ip), strings.TrimSpace(candidateIP)) {
		return false
	}
	expectedPorts := []int{gamePort, queryPort}
	candidatePorts := []int{candidateGamePort, candidateQueryPort}
	for _, expectedPort := range expectedPorts {
		if expectedPort <= 0 {
			continue
		}
		for _, candidatePort := range candidatePorts {
			if candidatePort > 0 && expectedPort == candidatePort {
				return true
			}
		}
	}
	return false
}

// RemoveDayZMetricsID invalidates a confirmed stale endpoint mapping without
// discarding independently useful country or perspective metadata. Both the
// metadata overlay and enriched server caches are rewritten before returning.
func (s *Service) RemoveDayZMetricsID(ip string, gamePort, queryPort int, externalID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadDiscoveriesOnce()
	s.loadMetadataOnce()

	metadataChanged := false
	for key, record := range s.metadata {
		if !endpointSharesPort(ip, gamePort, queryPort, record.IP, record.GamePort, record.QueryPort) {
			continue
		}
		if externalID > 0 && record.ExternalID != externalID {
			continue
		}

		record.ExternalID = 0
		record.UpdatedAtUnix = time.Now().Unix()
		if record.Country == "" && record.FirstPersonOnly == nil {
			delete(s.metadata, key)
		} else {
			s.metadata[key] = record
		}
		metadataChanged = true
	}

	clearServerIDs := func(servers []rawServer) bool {
		changed := false
		for index := range servers {
			server := &servers[index]
			if !endpointSharesPort(ip, gamePort, queryPort, server.Endpoint.IP, server.GamePort, server.Endpoint.Port) {
				continue
			}
			if externalID > 0 && server.ExternalID != externalID {
				continue
			}
			if server.ExternalID > 0 {
				server.ExternalID = 0
				changed = true
			}
		}
		return changed
	}

	serverCacheChanged := clearServerIDs(s.servers)
	discoveryCacheChanged := clearServerIDs(s.discoveries)
	if metadataChanged {
		if err := s.writeMetadataCache(); err != nil {
			return err
		}
	}
	if serverCacheChanged && len(s.servers) > 0 {
		if err := s.writeCurrentServerCache(nil); err != nil {
			return err
		}
	}
	if discoveryCacheChanged && len(s.discoveries) > 0 {
		if err := s.writeDiscoveryCache(); err != nil {
			return err
		}
	}
	return nil
}

// GetDayZMetricsMetadata returns the persisted endpoint enrichment used for
// manually configured servers which are not part of the downloaded DZSA list.
func (s *Service) GetDayZMetricsMetadata(ip string, gamePort, queryPort int) (persistedServerMetadata, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadMetadataOnce()
	return s.metadataFor(rawServer{
		GamePort: gamePort,
		Endpoint: endpoint{IP: ip, Port: queryPort},
	})
}

func (s *Service) loadDiscoveriesOnce() {
	if s.discoveriesLoaded {
		return
	}
	s.discoveriesLoaded = true
	body, err := os.ReadFile(s.discoveryCachePath)
	if err != nil {
		return
	}
	var discoveries []rawServer
	if json.Unmarshal(body, &discoveries) != nil {
		return
	}
	for index := range discoveries {
		discoveries[index].Provider = "dayzmetrics"
	}
	s.discoveries = discoveries
}

func (s *Service) writeDiscoveryCache() error {
	body, err := json.MarshalIndent(s.discoveries, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.discoveryCachePath, "dayzmetrics_discoveries_*.tmp", body)
}

func writeAtomic(path, pattern string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err = tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	if err = os.Rename(tmpPath, path); err == nil {
		return nil
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return err
	}
	return os.Rename(tmpPath, path)
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func sameEndpoint(server rawServer, ip string, gamePort, queryPort int) bool {
	if !strings.EqualFold(strings.TrimSpace(server.Endpoint.IP), strings.TrimSpace(ip)) {
		return false
	}
	return server.GamePort == gamePort || server.Endpoint.Port == queryPort
}

func findByNameOrEndpoint(servers []rawServer, name, ip string, gamePort, queryPort int) (rawServer, bool) {
	nameKey := normalizeName(name)
	for _, server := range servers {
		if normalizeName(server.Name) == nameKey || sameEndpoint(server, ip, gamePort, queryPort) {
			return server, true
		}
	}
	return rawServer{}, false
}

func mergedSearchPool(base, discoveries []rawServer) []rawServer {
	result := make([]rawServer, 0, len(base)+len(discoveries))
	result = append(result, base...)
	for _, discovery := range discoveries {
		if _, duplicate := findByNameOrEndpoint(base, discovery.Name, discovery.Endpoint.IP, discovery.GamePort, discovery.Endpoint.Port); duplicate {
			continue
		}
		result = append(result, discovery)
	}
	return result
}

func matchesQuery(server rawServer, query string) bool {
	needle := strings.ToLower(strings.Trim(strings.TrimSpace(query), `"`))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		server.Name,
		server.Map,
		server.Mission,
		server.Endpoint.IP,
		fmt.Sprintf("%s:%d", server.Endpoint.IP, server.GamePort),
		fmt.Sprintf("%s:%d", server.Endpoint.IP, server.Endpoint.Port),
	}, " "))
	return strings.Contains(haystack, needle)
}

func matchesFilters(server rawServer, filters map[string]interface{}) bool {
	if filters == nil {
		return true
	}
	if min, ok := intFilter(filters["minPlayers"]); ok && server.Players < min {
		return false
	}
	if max, ok := intFilter(filters["maxPlayers"]); ok && server.Players > max {
		return false
	}
	if !boolChoice(filters["modded"], len(server.Mods) > 0) {
		return false
	}
	if !boolChoice(filters["thirdPerson"], !server.FirstPersonOnly) {
		return false
	}
	if !boolChoice(filters["password"], server.Password) {
		return false
	}

	mapFilter := strings.ToLower(strings.TrimSpace(stringFilter(filters["map"])))
	if mapFilter != "" {
		aliases := map[string][]string{
			"chernarus": {"chernarus", "chernarusplus"},
			"livonia":   {"livonia", "enoch"},
			"sakhal":    {"sakhal"},
		}
		serverMap := strings.ToLower(server.Map)
		allowed := aliases[mapFilter]
		if len(allowed) == 0 {
			allowed = []string{mapFilter}
		}
		matched := false
		for _, candidate := range allowed {
			if strings.Contains(serverMap, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func boolChoice(value interface{}, actual bool) bool {
	choice := strings.ToLower(stringFilter(value))
	switch choice {
	case "true":
		return actual
	case "false":
		return !actual
	default:
		return true
	}
}

func intFilter(value interface{}) (int, bool) {
	text := strings.TrimSpace(stringFilter(value))
	if text == "" {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	return n, err == nil
}

func stringFilter(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

func normalize(server rawServer) Server {
	modNames := make([]string, 0, len(server.Mods))
	modIDs := make([]string, 0, len(server.Mods))
	for _, item := range server.Mods {
		modNames = append(modNames, item.Name)
		if id := rawID(item.SteamWorkshopID); id != "" {
			modIDs = append(modIDs, id)
		}
	}

	provider := server.Provider
	if provider == "" {
		provider = "dzsa"
	}
	id := fmt.Sprintf("dzsa-%s-%d-%d", strings.ReplaceAll(server.Endpoint.IP, ".", "-"), server.GamePort, server.Endpoint.Port)
	if provider == "dayzmetrics" && server.ExternalID > 0 {
		id = fmt.Sprintf("dayzmetrics-%d", server.ExternalID)
	}
	return Server{
		ID:   id,
		Type: "server",
		Attributes: ServerAttributes{
			Source:        provider,
			Provider:      provider,
			Name:          server.Name,
			IP:            server.Endpoint.IP,
			Port:          server.GamePort,
			PortQuery:     server.Endpoint.Port,
			Players:       server.Players,
			MaxPlayers:    server.MaxPlayers,
			Rank:          nil,
			Status:        "online",
			Password:      server.Password,
			Country:       server.Country,
			DayZMetricsID: server.ExternalID,
			Details: ServerDetails{
				Map:         server.Map,
				Mission:     server.Mission,
				Version:     server.Version,
				Time:        server.Time,
				Password:    server.Password,
				Official:    false,
				ThirdPerson: !server.FirstPersonOnly,
				Modded:      len(server.Mods) > 0,
				ModNames:    modNames,
				ModIDs:      modIDs,
			},
		},
	}
}

func rawID(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String()
	}
	return ""
}

func stringifyCreated(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	}
}
