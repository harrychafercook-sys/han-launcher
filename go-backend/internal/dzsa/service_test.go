package dzsa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDayZMetricsDiscoveriesAreSearchOnlyAndPersisted(t *testing.T) {
	cacheDir := t.TempDir()
	service := &Service{
		discoveryCachePath: filepath.Join(cacheDir, "discoveries.json"),
		servers: []rawServer{{
			Name:     "Existing DZSA Server",
			GamePort: 2302,
			Endpoint: endpoint{IP: "10.0.0.1", Port: 27016},
		}},
		source: "test",
	}

	duplicateName := service.MergeDayZMetricsServers([]ImportedServer{{
		ExternalID: 1, Name: "  existing   dzsa SERVER ", IP: "10.0.0.2", GamePort: 2302, QueryPort: 27016,
	}})
	if len(duplicateName) != 1 || duplicateName[0].Attributes.Provider != "dzsa" || len(service.discoveries) != 0 {
		t.Fatalf("expected normalized-name collision to prefer DZSA: %#v", duplicateName)
	}

	duplicateEndpoint := service.MergeDayZMetricsServers([]ImportedServer{{
		ExternalID: 2, Name: "Different advertised name", IP: "10.0.0.1", GamePort: 2302, QueryPort: 27016,
	}})
	if len(duplicateEndpoint) != 1 || duplicateEndpoint[0].Attributes.Provider != "dzsa" || len(service.discoveries) != 0 {
		t.Fatalf("expected endpoint collision to prefer DZSA: %#v", duplicateEndpoint)
	}

	unique := service.MergeDayZMetricsServers([]ImportedServer{{
		ExternalID: 3, Name: "Unique Metrics Server", IP: "10.0.0.3", GamePort: 2302, QueryPort: 27016,
	}})
	if len(unique) != 1 || unique[0].Attributes.Provider != "dayzmetrics" || len(service.discoveries) != 1 {
		t.Fatalf("expected unique discovery to be saved: %#v", unique)
	}

	mainList := service.Query("", nil, 0, 100, false)
	if len(mainList.Servers) != 1 || mainList.Servers[0].Attributes.Provider != "dzsa" {
		t.Fatalf("main list must remain DZSA-only: %#v", mainList.Servers)
	}

	search := service.Query("Unique Metrics", nil, 0, 100, false)
	if len(search.Servers) != 1 || search.Servers[0].Attributes.Provider != "dayzmetrics" {
		t.Fatalf("explicit search should include discovery: %#v", search.Servers)
	}

	reloaded := &Service{
		discoveryCachePath: service.discoveryCachePath,
		servers:            append([]rawServer(nil), service.servers...),
		source:             "test",
	}
	reloadedSearch := reloaded.Query("Unique Metrics", nil, 0, 100, false)
	if len(reloadedSearch.Servers) != 1 || reloadedSearch.Servers[0].Attributes.Provider != "dayzmetrics" {
		t.Fatalf("expected persisted discovery after reload: %#v", reloadedSearch.Servers)
	}
	if reloadedMain := reloaded.Query("", nil, 0, 100, false); len(reloadedMain.Servers) != 1 {
		t.Fatalf("persisted discoveries leaked into main list: %#v", reloadedMain.Servers)
	}
}

func TestDayZMetricsMetadataPersistsAndEnrichesDZSAList(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "dzsa_servers.json")
	metadataPath := filepath.Join(cacheDir, "dayzmetrics_server_metadata.json")
	baseServer := rawServer{
		Name:     "DZSA Server",
		GamePort: 2302,
		Endpoint: endpoint{IP: "10.0.0.5", Port: 27016},
	}
	service := &Service{
		cachePath:         cachePath,
		metadataCachePath: metadataPath,
		servers:           []rawServer{baseServer},
		source:            "test",
	}

	if err := service.SaveDayZMetricsMetadata("10.0.0.5", 2302, 27016, 160006, "de", 1); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	result := service.Query("", nil, 0, 100, false)
	if len(result.Servers) != 1 || result.Servers[0].Attributes.Country != "DE" || result.Servers[0].Attributes.DayZMetricsID != 160006 || result.Servers[0].Attributes.Details.ThirdPerson {
		t.Fatalf("expected enriched in-memory DZSA server: %#v", result.Servers)
	}

	body, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read enriched DZSA cache: %v", err)
	}
	parsed, err := parsePayload(body)
	if err != nil || len(parsed.Result) != 1 || parsed.Result[0].Country != "DE" || parsed.Result[0].ExternalID != 160006 || !parsed.Result[0].FirstPersonOnly {
		t.Fatalf("expected metadata merged into DZSA cache file: parsed=%#v err=%v", parsed, err)
	}

	reloaded := &Service{
		cachePath:         cachePath,
		metadataCachePath: metadataPath,
		servers:           []rawServer{baseServer},
		source:            "test",
	}
	reloadedResult := reloaded.Query("", nil, 0, 100, false)
	if len(reloadedResult.Servers) != 1 || reloadedResult.Servers[0].Attributes.Country != "DE" || reloadedResult.Servers[0].Attributes.DayZMetricsID != 160006 || reloadedResult.Servers[0].Attributes.Details.ThirdPerson {
		t.Fatalf("expected metadata overlay after reload: %#v", reloadedResult.Servers)
	}
	metadata, ok := reloaded.GetDayZMetricsMetadata("10.0.0.5", 2302, 27016)
	if !ok || metadata.Country != "DE" || metadata.ExternalID != 160006 || metadata.FirstPersonOnly == nil || !*metadata.FirstPersonOnly {
		t.Fatalf("expected manual-server metadata lookup after reload: %#v, ok=%v", metadata, ok)
	}
	if err := reloaded.RemoveDayZMetricsID("10.0.0.5", 9998, 9999, 160006); err != nil {
		t.Fatalf("ignore unrelated endpoint during ID removal: %v", err)
	}
	stillMapped, ok := reloaded.GetDayZMetricsMetadata("10.0.0.5", 2302, 27016)
	if !ok || stillMapped.ExternalID != 160006 {
		t.Fatalf("mapping removed without a matching port: %#v, ok=%v", stillMapped, ok)
	}

	if err := reloaded.RemoveDayZMetricsID("10.0.0.5", 2302, 9999, 160006); err != nil {
		t.Fatalf("remove stale DayZMetrics ID by matching game port: %v", err)
	}
	clearedResult := reloaded.Query("", nil, 0, 100, false)
	if len(clearedResult.Servers) != 1 || clearedResult.Servers[0].Attributes.DayZMetricsID != 0 {
		t.Fatalf("expected stale ID removed from in-memory DZSA server: %#v", clearedResult.Servers)
	}
	if clearedResult.Servers[0].Attributes.Country != "DE" || clearedResult.Servers[0].Attributes.Details.ThirdPerson {
		t.Fatalf("expected country and perspective preserved after ID removal: %#v", clearedResult.Servers[0])
	}
	clearedMetadata, ok := reloaded.GetDayZMetricsMetadata("10.0.0.5", 2302, 27016)
	if !ok || clearedMetadata.ExternalID != 0 || clearedMetadata.Country != "DE" || clearedMetadata.FirstPersonOnly == nil || !*clearedMetadata.FirstPersonOnly {
		t.Fatalf("expected only file-backed ID removed: %#v, ok=%v", clearedMetadata, ok)
	}
	clearedBody, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache after ID removal: %v", err)
	}
	clearedPayload, err := parsePayload(clearedBody)
	if err != nil || len(clearedPayload.Result) != 1 || clearedPayload.Result[0].ExternalID != 0 {
		t.Fatalf("expected rewritten DZSA cache without stale ID: parsed=%#v err=%v", clearedPayload, err)
	}
	metadataBody, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata after ID removal: %v", err)
	}
	var metadataFile map[string]persistedServerMetadata
	if err := json.Unmarshal(metadataBody, &metadataFile); err != nil {
		t.Fatalf("parse metadata after ID removal: %v", err)
	}
	for _, record := range metadataFile {
		if record.ExternalID != 0 {
			t.Fatalf("expected file-backed metadata without stale ID: %#v", metadataFile)
		}
	}
}
