package approval

import (
	"testing"

	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/snapshot"
	"pgregory.net/rapid"
)

// After ApproveAll, there should be zero pending diffs left, and every
// snapshot should match the live response that was pending.
func TestApproveAllClearsPending(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		snapDir := t.TempDir()
		pendingDir := t.TempDir()

		store := snapshot.NewSnapshotStore(snapDir)
		mgr := NewApprovalManager(store, pendingDir)

		// Generate 1-8 pending diffs, each with a unique host+hash pair.
		numPending := rapid.IntRange(1, 8).Draw(rt, "numPending")

		type pendingKey struct {
			Host string
			Hash string
		}
		seen := make(map[pendingKey]bool)
		var pendingDiffs []PendingDiff

		for len(pendingDiffs) < numPending {
			host := rapid.StringMatching(`[a-z][a-z0-9]{0,8}\.[a-z]{2,4}`).Draw(rt, "host")
			hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")
			key := pendingKey{Host: host, Hash: hash}
			if seen[key] {
				continue
			}
			seen[key] = true

			// Seed the store with an "old" snapshot entry for this hash.
			oldEntry := genSnapshotEntry(rt)
			if err := store.Record(host, hash, oldEntry); err != nil {
				rt.Fatalf("seeding snapshot store: %v", err)
			}

			// Create a "live" entry that differs from the old one.
			liveEntry := genSnapshotEntry(rt)

			pd := PendingDiff{
				RequestHash: hash,
				Host:        host,
				Live:        liveEntry,
				Diffs: []diff.FieldDiff{
					{Path: "body", Expected: oldEntry.Body, Actual: liveEntry.Body},
				},
			}
			pendingDiffs = append(pendingDiffs, pd)

			if err := mgr.SavePending(pd); err != nil {
				rt.Fatalf("SavePending failed: %v", err)
			}
		}

		// Approve all pending diffs.
		count, err := mgr.ApproveAll()
		if err != nil {
			rt.Fatalf("ApproveAll failed: %v", err)
		}
		if count != numPending {
			rt.Fatalf("ApproveAll returned count %d, want %d", count, numPending)
		}

		// Verify: zero pending diffs remain.
		hasPending, err := mgr.HasPendingDiffs()
		if err != nil {
			rt.Fatalf("HasPendingDiffs failed: %v", err)
		}
		if hasPending {
			rt.Fatal("expected zero pending diffs after ApproveAll, but HasPendingDiffs returned true")
		}

		remaining, err := mgr.ListPending()
		if err != nil {
			rt.Fatalf("ListPending failed: %v", err)
		}
		if len(remaining) != 0 {
			rt.Fatalf("expected 0 remaining pending diffs, got %d", len(remaining))
		}

		// Verify: every snapshot in the store matches the live response.
		for _, pd := range pendingDiffs {
			got, err := store.Lookup(pd.Host, pd.RequestHash)
			if err != nil {
				rt.Fatalf("Lookup failed for hash %s: %v", pd.RequestHash, err)
			}
			if got == nil {
				rt.Fatalf("snapshot missing for hash %s after ApproveAll", pd.RequestHash)
			}
			if got.Method != pd.Live.Method {
				rt.Fatalf("Method mismatch for hash %s: got %q, want %q", pd.RequestHash, got.Method, pd.Live.Method)
			}
			if got.URL != pd.Live.URL {
				rt.Fatalf("URL mismatch for hash %s: got %q, want %q", pd.RequestHash, got.URL, pd.Live.URL)
			}
			if got.StatusCode != pd.Live.StatusCode {
				rt.Fatalf("StatusCode mismatch for hash %s: got %d, want %d", pd.RequestHash, got.StatusCode, pd.Live.StatusCode)
			}
			if got.Body != pd.Live.Body {
				rt.Fatalf("Body mismatch for hash %s: got %q, want %q", pd.RequestHash, got.Body, pd.Live.Body)
			}
		}
	})
}

// --- Generators ---

func genSnapshotEntry(t *rapid.T) snapshot.SnapshotEntry {
	return snapshot.SnapshotEntry{
		Method:     rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH"}).Draw(t, "method"),
		URL:        genURL(t),
		StatusCode: rapid.SampledFrom([]int{200, 201, 204, 301, 400, 401, 404, 500}).Draw(t, "statusCode"),
		Headers:    genHeaders(t),
		Body:       rapid.StringMatching(`[a-zA-Z0-9 {}\[\]:,"._\-]{0,100}`).Draw(t, "body"),
	}
}

func genURL(t *rapid.T) string {
	scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(t, "scheme")
	host := rapid.StringMatching(`[a-z][a-z0-9]{0,8}\.[a-z]{2,4}`).Draw(t, "urlHost")
	path := rapid.StringMatching(`(/[a-z0-9]{1,6}){0,3}`).Draw(t, "path")
	return scheme + "://" + host + path
}

func genHeaders(t *rapid.T) map[string][]string {
	headers := make(map[string][]string)
	n := rapid.IntRange(0, 3).Draw(t, "numHeaders")
	for i := 0; i < n; i++ {
		key := rapid.StringMatching(`[A-Z][a-z]{1,8}(-[A-Z][a-z]{1,8}){0,2}`).Draw(t, "hdrKey")
		val := rapid.StringMatching(`[a-zA-Z0-9/;= ]{1,15}`).Draw(t, "hdrVal")
		headers[key] = []string{val}
	}
	return headers
}

// ApproveOne should only touch the targeted hash. Everything else stays pending.
func TestApproveOneIsSelective(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		snapDir := t.TempDir()
		pendingDir := t.TempDir()

		store := snapshot.NewSnapshotStore(snapDir)
		mgr := NewApprovalManager(store, pendingDir)

		// Generate 2-8 pending diffs, each with a unique host+hash pair.
		numPending := rapid.IntRange(2, 8).Draw(rt, "numPending")

		type pendingKey struct {
			Host string
			Hash string
		}
		seen := make(map[pendingKey]bool)
		var pendingDiffs []PendingDiff

		for len(pendingDiffs) < numPending {
			host := rapid.StringMatching(`[a-z][a-z0-9]{0,8}\.[a-z]{2,4}`).Draw(rt, "host")
			hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")
			key := pendingKey{Host: host, Hash: hash}
			if seen[key] {
				continue
			}
			seen[key] = true

			// Seed the store with an "old" snapshot entry for this hash.
			oldEntry := genSnapshotEntry(rt)
			if err := store.Record(host, hash, oldEntry); err != nil {
				rt.Fatalf("seeding snapshot store: %v", err)
			}

			// Create a "live" entry that differs from the old one.
			liveEntry := genSnapshotEntry(rt)

			pd := PendingDiff{
				RequestHash: hash,
				Host:        host,
				Live:        liveEntry,
				Diffs: []diff.FieldDiff{
					{Path: "body", Expected: oldEntry.Body, Actual: liveEntry.Body},
				},
			}
			pendingDiffs = append(pendingDiffs, pd)

			if err := mgr.SavePending(pd); err != nil {
				rt.Fatalf("SavePending failed: %v", err)
			}
		}

		// Pick one random pending diff to approve.
		targetIdx := rapid.IntRange(0, len(pendingDiffs)-1).Draw(rt, "targetIdx")
		targetDiff := pendingDiffs[targetIdx]

		// Approve only the targeted hash.
		if err := mgr.ApproveOne(targetDiff.RequestHash); err != nil {
			rt.Fatalf("ApproveOne failed: %v", err)
		}

		// Verify: the targeted snapshot was updated to match the live response.
		got, err := store.Lookup(targetDiff.Host, targetDiff.RequestHash)
		if err != nil {
			rt.Fatalf("Lookup failed for approved hash %s: %v", targetDiff.RequestHash, err)
		}
		if got == nil {
			rt.Fatalf("snapshot missing for approved hash %s", targetDiff.RequestHash)
		}
		if got.Method != targetDiff.Live.Method {
			rt.Fatalf("approved Method mismatch: got %q, want %q", got.Method, targetDiff.Live.Method)
		}
		if got.URL != targetDiff.Live.URL {
			rt.Fatalf("approved URL mismatch: got %q, want %q", got.URL, targetDiff.Live.URL)
		}
		if got.StatusCode != targetDiff.Live.StatusCode {
			rt.Fatalf("approved StatusCode mismatch: got %d, want %d", got.StatusCode, targetDiff.Live.StatusCode)
		}
		if got.Body != targetDiff.Live.Body {
			rt.Fatalf("approved Body mismatch: got %q, want %q", got.Body, targetDiff.Live.Body)
		}

		// Verify: the targeted pending file was removed.
		remaining, err := mgr.ListPending()
		if err != nil {
			rt.Fatalf("ListPending failed: %v", err)
		}
		if len(remaining) != numPending-1 {
			rt.Fatalf("expected %d remaining pending diffs, got %d", numPending-1, len(remaining))
		}

		// Verify: all other pending diffs are still present and unchanged.
		remainingByHash := make(map[string]PendingDiff)
		for _, r := range remaining {
			remainingByHash[r.RequestHash] = r
		}

		for i, pd := range pendingDiffs {
			if i == targetIdx {
				// The approved diff should NOT be in the remaining set.
				if _, found := remainingByHash[pd.RequestHash]; found {
					rt.Fatalf("approved hash %s should not be in remaining pending diffs", pd.RequestHash)
				}
				continue
			}

			// Every other diff should still be pending.
			r, found := remainingByHash[pd.RequestHash]
			if !found {
				rt.Fatalf("pending diff for hash %s missing after ApproveOne", pd.RequestHash)
			}
			if r.Host != pd.Host {
				rt.Fatalf("pending Host changed for hash %s: got %q, want %q", pd.RequestHash, r.Host, pd.Host)
			}
			if r.Live.Body != pd.Live.Body {
				rt.Fatalf("pending Live.Body changed for hash %s", pd.RequestHash)
			}
			if r.Live.StatusCode != pd.Live.StatusCode {
				rt.Fatalf("pending Live.StatusCode changed for hash %s", pd.RequestHash)
			}
		}
	})
}
