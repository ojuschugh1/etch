package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// Full lifecycle: record entries across multiple hosts, load them all back,
// verify the data survived, then overwrite one and confirm the update stuck.
func TestSnapshotLifecycle(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)

	// record entries to two different hosts
	store.Record("api.example.com", "hash1", SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `[{"id":1,"name":"Alice"}]`,
	})
	store.Record("api.example.com", "hash2", SnapshotEntry{
		Method: "POST", URL: "http://api.example.com/users",
		StatusCode: 201,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"id":2,"name":"Bob"}`,
	})
	store.Record("cdn.example.com", "hash3", SnapshotEntry{
		Method: "GET", URL: "http://cdn.example.com/image.png",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"image/png"}},
		Body:       "<binary>",
	})

	// should have exactly 2 snap files
	entries, _ := os.ReadDir(dir)
	snapFiles := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".snap" {
			snapFiles++
		}
	}
	if snapFiles != 2 {
		t.Fatalf("expected 2 snap files, got %d", snapFiles)
	}

	// load everything back
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(all))
	}
	if len(all["api.example.com"]) != 2 {
		t.Fatalf("expected 2 entries for api.example.com, got %d", len(all["api.example.com"]))
	}
	if len(all["cdn.example.com"]) != 1 {
		t.Fatalf("expected 1 entry for cdn.example.com, got %d", len(all["cdn.example.com"]))
	}

	// verify specific entry data
	entry, _ := store.Lookup("api.example.com", "hash1")
	if entry == nil {
		t.Fatal("hash1 not found")
	}
	if entry.Body != `[{"id":1,"name":"Alice"}]` {
		t.Errorf("body mismatch: %q", entry.Body)
	}

	// overwrite hash1 with new data
	store.Record("api.example.com", "hash1", SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `[{"id":1,"name":"Alice"},{"id":3,"name":"Charlie"}]`,
	})

	updated, _ := store.Lookup("api.example.com", "hash1")
	if updated.Body != `[{"id":1,"name":"Alice"},{"id":3,"name":"Charlie"}]` {
		t.Errorf("overwrite didn't stick: %q", updated.Body)
	}

	// hash2 should be untouched
	other, _ := store.Lookup("api.example.com", "hash2")
	if other.Body != `{"id":2,"name":"Bob"}` {
		t.Errorf("hash2 got clobbered: %q", other.Body)
	}
}

// Save a file, read the raw bytes, save again from loaded data - bytes
// should be identical. This is the key property for git-friendly snapshots.
func TestSnapshotByteStability(t *testing.T) {
	dir := t.TempDir()
	store := NewSnapshotStore(dir)

	store.Record("test.com", "aaa", SnapshotEntry{
		Method: "GET", URL: "http://test.com/a",
		StatusCode: 200,
		Headers:    map[string][]string{"X-Foo": {"bar", "baz"}},
		Body:       `{"nested":{"key":"value"},"list":[1,2,3]}`,
	})
	store.Record("test.com", "bbb", SnapshotEntry{
		Method: "POST", URL: "http://test.com/b",
		StatusCode: 201,
		Headers:    map[string][]string{},
		Body:       "plain text",
	})

	path := filepath.Join(dir, "test.com.snap")
	first, _ := os.ReadFile(path)

	loaded, _ := store.Load("test.com")
	store.Save("test.com", loaded)

	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Fatalf("file changed after load+save:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}
