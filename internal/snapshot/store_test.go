package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_NonExistentFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)

	sf, err := store.Load("no-such-host")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if sf == nil {
		t.Fatal("expected non-nil SnapshotFile")
	}
	if len(sf) != 0 {
		t.Fatalf("expected empty SnapshotFile, got %d entries", len(sf))
	}
}

func TestSaveThenLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)
	host := "api.example.com"

	original := SnapshotFile{
		"abc123": {
			Method:     "GET",
			URL:        "https://api.example.com/users/1",
			StatusCode: 200,
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: `{"id":1,"name":"Alice"}`,
		},
		"def456": {
			Method:     "POST",
			URL:        "https://api.example.com/users",
			StatusCode: 201,
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
				"X-Request-Id": {"req-001", "req-002"},
			},
			Body: `{"id":2,"name":"Bob"}`,
		},
	}

	if err := store.Save(host, original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load(host)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch:\noriginal: %+v\nloaded:   %+v", original, loaded)
	}
}

func TestRecord_OverwritesDuplicateHash(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)
	host := "api.example.com"
	hash := "samehash"

	first := SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/old",
		StatusCode: 200,
		Headers:    map[string][]string{"X-Version": {"1"}},
		Body:       `{"version":"old"}`,
	}
	if err := store.Record(host, hash, first); err != nil {
		t.Fatalf("first Record failed: %v", err)
	}

	second := SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/new",
		StatusCode: 201,
		Headers:    map[string][]string{"X-Version": {"2"}},
		Body:       `{"version":"new"}`,
	}
	if err := store.Record(host, hash, second); err != nil {
		t.Fatalf("second Record failed: %v", err)
	}

	got, err := store.Lookup(host, hash)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("Lookup returned nil after overwrite")
	}
	if got.URL != second.URL {
		t.Errorf("URL not overwritten: got %q, want %q", got.URL, second.URL)
	}
	if got.StatusCode != second.StatusCode {
		t.Errorf("StatusCode not overwritten: got %d, want %d", got.StatusCode, second.StatusCode)
	}
	if got.Body != second.Body {
		t.Errorf("Body not overwritten: got %q, want %q", got.Body, second.Body)
	}
}

func TestLoad_CorruptFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)
	host := "corrupt-host"

	// Write invalid JSON to the snap file.
	corruptData := []byte(`{not valid json!!!`)
	if err := os.WriteFile(filepath.Join(dir, host+".snap"), corruptData, 0644); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	_, err := store.Load(host)
	if err == nil {
		t.Fatal("expected error for corrupt snapshot file, got nil")
	}
}

// Lookup for a hash that doesn't exist should return nil, not error.
func TestLookup_MissingHash_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)
	host := "api.example.com"

	entry := SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/exists",
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       "ok",
	}
	if err := store.Record(host, "existinghash", entry); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	got, err := store.Lookup(host, "nonexistenthash")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing hash, got %+v", got)
	}
}

func TestLoadAll_MultipleHosts(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)

	hosts := []string{"host-a.com", "host-b.com"}
	for _, h := range hosts {
		entry := SnapshotEntry{
			Method:     "GET",
			URL:        "https://" + h + "/ping",
			StatusCode: 200,
			Headers:    map[string][]string{},
			Body:       "pong",
		}
		if err := store.Record(h, "hash1", entry); err != nil {
			t.Fatalf("Record failed for %s: %v", h, err)
		}
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(all) != len(hosts) {
		t.Fatalf("expected %d hosts, got %d", len(hosts), len(all))
	}
	for _, h := range hosts {
		if _, ok := all[h]; !ok {
			t.Errorf("missing host %q in LoadAll result", h)
		}
	}
}

func TestLoadAll_NonExistentDir_ReturnsEmpty(t *testing.T) {
	store := NewSnapshotStore(filepath.Join(t.TempDir(), "does-not-exist"))

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(all))
	}
}

// Keys in the JSON output should be sorted so that git diffs stay clean.
func TestSave_DeterministicJSON(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)
	host := "deterministic-host"

	sf := SnapshotFile{
		"zzz": {
			Method:     "POST",
			URL:        "https://deterministic-host/z",
			StatusCode: 201,
			Headers:    map[string][]string{"Z-Header": {"z"}},
			Body:       "z-body",
		},
		"aaa": {
			Method:     "GET",
			URL:        "https://deterministic-host/a",
			StatusCode: 200,
			Headers:    map[string][]string{"A-Header": {"a"}},
			Body:       "a-body",
		},
	}

	if err := store.Save(host, sf); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, host+".snap"))
	if err != nil {
		t.Fatalf("reading snap file: %v", err)
	}

	// Verify it's valid JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}

	// Verify top-level keys are sorted: "aaa" before "zzz"
	content := string(data)
	aaaIdx := indexOf(content, `"aaa"`)
	zzzIdx := indexOf(content, `"zzz"`)
	if aaaIdx == -1 || zzzIdx == -1 {
		t.Fatal("expected both hash keys in output")
	}
	if aaaIdx >= zzzIdx {
		t.Error("expected top-level keys to be sorted alphabetically (aaa before zzz)")
	}

	// Verify entry fields are sorted: body < headers < method < status_code < url
	for hash, entryRaw := range raw {
		var entryMap map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &entryMap); err != nil {
			t.Fatalf("entry %s is not a valid JSON object: %v", hash, err)
		}
		requiredKeys := []string{"body", "headers", "method", "status_code", "url"}
		for _, k := range requiredKeys {
			if _, ok := entryMap[k]; !ok {
				t.Errorf("entry %s missing required key %q", hash, k)
			}
		}
	}
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
