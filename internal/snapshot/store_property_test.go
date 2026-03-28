package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"
)

// Save -> Load -> Save should produce byte-identical files. This is the key
// property for version-control friendliness - if the file changes on a
// no-op round trip, you get phantom diffs in PRs.
func TestSnapshotFileRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		store := NewSnapshotStore(dir)
		host := "roundtrip-host"

		sf := genSnapshotFile(rt)

		if err := store.Save(host, sf); err != nil {
			rt.Fatalf("first save: %v", err)
		}
		firstBytes, _ := os.ReadFile(filepath.Join(dir, host+".snap"))

		loaded, err := store.Load(host)
		if err != nil {
			rt.Fatalf("load: %v", err)
		}

		if err := store.Save(host, loaded); err != nil {
			rt.Fatalf("second save: %v", err)
		}
		secondBytes, _ := os.ReadFile(filepath.Join(dir, host+".snap"))

		if string(firstBytes) != string(secondBytes) {
			rt.Fatalf("file changed after round-trip:\n--- first ---\n%s\n--- second ---\n%s",
				firstBytes, secondBytes)
		}
	})
}

func genSnapshotFile(t *rapid.T) SnapshotFile {
	sf := make(SnapshotFile)
	n := rapid.IntRange(0, 5).Draw(t, "numEntries")
	for i := 0; i < n; i++ {
		hash := rapid.StringMatching(`[a-f0-9]{8,64}`).Draw(t, "hash")
		sf[hash] = genSnapshotEntry(t)
	}
	return sf
}

func genSnapshotEntry(t *rapid.T) SnapshotEntry {
	return SnapshotEntry{
		Method:     rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}).Draw(t, "method"),
		URL:        genEntryURL(t),
		StatusCode: rapid.SampledFrom([]int{200, 201, 204, 301, 400, 401, 403, 404, 500, 502, 503}).Draw(t, "statusCode"),
		Headers:    genHeaderMap(t),
		Body:       rapid.StringMatching(`[a-zA-Z0-9 {}\[\]:,"._\-]{0,200}`).Draw(t, "body"),
	}
}

func genEntryURL(t *rapid.T) string {
	scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(t, "scheme")
	host := rapid.StringMatching(`[a-z][a-z0-9]{0,12}\.[a-z]{2,4}`).Draw(t, "host")
	path := rapid.StringMatching(`(/[a-z0-9]{1,8}){0,3}`).Draw(t, "path")
	return scheme + "://" + host + path
}

func genHeaderMap(t *rapid.T) map[string][]string {
	headers := make(map[string][]string)
	n := rapid.IntRange(0, 4).Draw(t, "numHeaders")
	for i := 0; i < n; i++ {
		key := rapid.StringMatching(`[A-Z][a-z]{1,10}(-[A-Z][a-z]{1,10}){0,2}`).Draw(t, "headerKey")
		nv := rapid.IntRange(1, 3).Draw(t, "numVals")
		vals := make([]string, nv)
		for j := range vals {
			vals[j] = rapid.StringMatching(`[a-zA-Z0-9/;= ]{1,20}`).Draw(t, "headerVal")
		}
		headers[key] = vals
	}
	return headers
}

// Every persisted entry should have exactly these 5 keys - no more, no less.
// This guards against accidentally adding or dropping fields in the JSON output.
func TestSnapshotHasAllRequiredFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		store := NewSnapshotStore(dir)
		host := "fields-host"
		hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")
		entry := genSnapshotEntry(rt)

		store.Record(host, hash, entry)

		data, _ := os.ReadFile(filepath.Join(dir, host+".snap"))

		var raw map[string]map[string]interface{}
		json.Unmarshal(data, &raw)

		entryMap := raw[hash]
		want := map[string]bool{"method": true, "url": true, "status_code": true, "headers": true, "body": true}

		for key := range want {
			if _, ok := entryMap[key]; !ok {
				rt.Fatalf("missing key %q in persisted entry", key)
			}
		}
		for key := range entryMap {
			if !want[key] {
				rt.Fatalf("unexpected key %q in persisted entry", key)
			}
		}
	})
}

// N distinct hosts should produce exactly N .snap files on disk.
func TestOneFilePerHost(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		store := NewSnapshotStore(dir)

		numHosts := rapid.IntRange(1, 6).Draw(rt, "numHosts")
		hosts := make(map[string]bool)
		for len(hosts) < numHosts {
			h := rapid.StringMatching(`[a-z][a-z0-9]{0,10}\.[a-z]{2,4}`).Draw(rt, "host")
			hosts[h] = true
		}

		for host := range hosts {
			n := rapid.IntRange(1, 3).Draw(rt, "numEntries")
			for i := 0; i < n; i++ {
				hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")
				store.Record(host, hash, genSnapshotEntry(rt))
			}
		}

		entries, _ := os.ReadDir(dir)
		count := 0
		found := make(map[string]bool)
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".snap" {
				count++
				found[e.Name()[:len(e.Name())-5]] = true
			}
		}

		if count != numHosts {
			rt.Fatalf("expected %d snap files, got %d", numHosts, count)
		}
		for host := range hosts {
			if !found[host] {
				rt.Fatalf("no snap file for host %q", host)
			}
		}
	})
}

// Recording with the same hash twice should overwrite - the second entry wins.
func TestRecordOverwritesSameHash(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		store := NewSnapshotStore(dir)
		host := rapid.StringMatching(`[a-z][a-z0-9]{0,10}\.[a-z]{2,4}`).Draw(rt, "host")
		hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")

		first := genSnapshotEntry(rt)
		store.Record(host, hash, first)

		second := genSnapshotEntry(rt)
		store.Record(host, hash, second)

		got, _ := store.Lookup(host, hash)
		if got == nil {
			rt.Fatal("entry disappeared after overwrite")
		}
		if got.Method != second.Method || got.URL != second.URL ||
			got.StatusCode != second.StatusCode || got.Body != second.Body {
			rt.Fatal("entry wasn't fully overwritten by second Record call")
		}
	})
}
