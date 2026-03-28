package approval

import (
	"testing"

	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestApproveAll_NoPendingDiffs(t *testing.T) {
	snapDir := t.TempDir()
	pendingDir := t.TempDir()

	store := snapshot.NewSnapshotStore(snapDir)
	mgr := NewApprovalManager(store, pendingDir)

	count, err := mgr.ApproveAll()
	if err != nil {
		t.Fatalf("ApproveAll with no pending diffs failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count 0, got %d", count)
	}

	hasPending, err := mgr.HasPendingDiffs()
	if err != nil {
		t.Fatalf("HasPendingDiffs failed: %v", err)
	}
	if hasPending {
		t.Fatal("expected no pending diffs")
	}
}

func TestHasPendingDiffs_NonExistentDir(t *testing.T) {
	snapDir := t.TempDir()
	mgr := NewApprovalManager(
		snapshot.NewSnapshotStore(snapDir),
		snapDir+"/nonexistent-pending",
	)

	hasPending, err := mgr.HasPendingDiffs()
	if err != nil {
		t.Fatalf("HasPendingDiffs failed: %v", err)
	}
	if hasPending {
		t.Fatal("expected false for non-existent pending directory")
	}
}

func TestListPending_NonExistentDir(t *testing.T) {
	snapDir := t.TempDir()
	mgr := NewApprovalManager(
		snapshot.NewSnapshotStore(snapDir),
		snapDir+"/nonexistent-pending",
	)

	pending, err := mgr.ListPending()
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(pending))
	}
}

func TestApproveAll_ClearsAllEntries(t *testing.T) {
	snapDir := t.TempDir()
	pendingDir := t.TempDir()

	store := snapshot.NewSnapshotStore(snapDir)
	mgr := NewApprovalManager(store, pendingDir)

	// Seed the store with old entries and create pending diffs.
	diffs := []PendingDiff{
		{
			RequestHash: "hash1",
			Host:        "api.example.com",
			Live: snapshot.SnapshotEntry{
				Method:     "GET",
				URL:        "https://api.example.com/users/1",
				StatusCode: 200,
				Headers:    map[string][]string{"Content-Type": {"application/json"}},
				Body:       `{"name":"Bob"}`,
			},
			Diffs: []diff.FieldDiff{{Path: "body.name", Expected: "Alice", Actual: "Bob"}},
		},
		{
			RequestHash: "hash2",
			Host:        "api.other.com",
			Live: snapshot.SnapshotEntry{
				Method:     "POST",
				URL:        "https://api.other.com/items",
				StatusCode: 201,
				Headers:    map[string][]string{"Content-Type": {"application/json"}},
				Body:       `{"id":42}`,
			},
			Diffs: []diff.FieldDiff{{Path: "body.id", Expected: "1", Actual: "42"}},
		},
		{
			RequestHash: "hash3",
			Host:        "api.example.com",
			Live: snapshot.SnapshotEntry{
				Method:     "DELETE",
				URL:        "https://api.example.com/users/2",
				StatusCode: 204,
				Headers:    map[string][]string{},
				Body:       "",
			},
			Diffs: []diff.FieldDiff{{Path: "status_code", Expected: "200", Actual: "204"}},
		},
	}

	// Seed old snapshots and save pending diffs.
	for _, pd := range diffs {
		oldEntry := snapshot.SnapshotEntry{
			Method:     pd.Live.Method,
			URL:        pd.Live.URL,
			StatusCode: 999,
			Headers:    map[string][]string{},
			Body:       "old-body",
		}
		if err := store.Record(pd.Host, pd.RequestHash, oldEntry); err != nil {
			t.Fatalf("seeding store: %v", err)
		}
		if err := mgr.SavePending(pd); err != nil {
			t.Fatalf("SavePending: %v", err)
		}
	}

	// Verify pending diffs exist before approval.
	hasPending, err := mgr.HasPendingDiffs()
	if err != nil {
		t.Fatalf("HasPendingDiffs: %v", err)
	}
	if !hasPending {
		t.Fatal("expected pending diffs before ApproveAll")
	}

	// Approve all.
	count, err := mgr.ApproveAll()
	if err != nil {
		t.Fatalf("ApproveAll: %v", err)
	}
	if count != len(diffs) {
		t.Fatalf("expected count %d, got %d", len(diffs), count)
	}

	// Verify no pending diffs remain.
	hasPending, err = mgr.HasPendingDiffs()
	if err != nil {
		t.Fatalf("HasPendingDiffs after approve: %v", err)
	}
	if hasPending {
		t.Fatal("expected no pending diffs after ApproveAll")
	}

	remaining, err := mgr.ListPending()
	if err != nil {
		t.Fatalf("ListPending after approve: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected 0 remaining, got %d", len(remaining))
	}

	// Verify each snapshot was updated to the live response.
	for _, pd := range diffs {
		got, err := store.Lookup(pd.Host, pd.RequestHash)
		if err != nil {
			t.Fatalf("Lookup %s: %v", pd.RequestHash, err)
		}
		if got == nil {
			t.Fatalf("snapshot missing for %s", pd.RequestHash)
		}
		if got.StatusCode != pd.Live.StatusCode {
			t.Errorf("hash %s: StatusCode got %d, want %d", pd.RequestHash, got.StatusCode, pd.Live.StatusCode)
		}
		if got.Body != pd.Live.Body {
			t.Errorf("hash %s: Body got %q, want %q", pd.RequestHash, got.Body, pd.Live.Body)
		}
	}
}

func TestApproveOne_LeavesOtherEntriesIntact(t *testing.T) {
	snapDir := t.TempDir()
	pendingDir := t.TempDir()

	store := snapshot.NewSnapshotStore(snapDir)
	mgr := NewApprovalManager(store, pendingDir)

	// Create three pending diffs.
	diffs := []PendingDiff{
		{
			RequestHash: "aaa111",
			Host:        "api.example.com",
			Live: snapshot.SnapshotEntry{
				Method:     "GET",
				URL:        "https://api.example.com/a",
				StatusCode: 200,
				Headers:    map[string][]string{},
				Body:       `{"a":"new"}`,
			},
			Diffs: []diff.FieldDiff{{Path: "body.a", Expected: "old", Actual: "new"}},
		},
		{
			RequestHash: "bbb222",
			Host:        "api.example.com",
			Live: snapshot.SnapshotEntry{
				Method:     "GET",
				URL:        "https://api.example.com/b",
				StatusCode: 200,
				Headers:    map[string][]string{},
				Body:       `{"b":"new"}`,
			},
			Diffs: []diff.FieldDiff{{Path: "body.b", Expected: "old", Actual: "new"}},
		},
		{
			RequestHash: "ccc333",
			Host:        "api.other.com",
			Live: snapshot.SnapshotEntry{
				Method:     "PUT",
				URL:        "https://api.other.com/c",
				StatusCode: 200,
				Headers:    map[string][]string{},
				Body:       `{"c":"new"}`,
			},
			Diffs: []diff.FieldDiff{{Path: "body.c", Expected: "old", Actual: "new"}},
		},
	}

	for _, pd := range diffs {
		oldEntry := snapshot.SnapshotEntry{
			Method:     pd.Live.Method,
			URL:        pd.Live.URL,
			StatusCode: 999,
			Headers:    map[string][]string{},
			Body:       "old-body",
		}
		if err := store.Record(pd.Host, pd.RequestHash, oldEntry); err != nil {
			t.Fatalf("seeding store: %v", err)
		}
		if err := mgr.SavePending(pd); err != nil {
			t.Fatalf("SavePending: %v", err)
		}
	}

	// Approve only the second entry.
	if err := mgr.ApproveOne("bbb222"); err != nil {
		t.Fatalf("ApproveOne: %v", err)
	}

	// Verify the approved snapshot was updated.
	got, err := store.Lookup("api.example.com", "bbb222")
	if err != nil {
		t.Fatalf("Lookup bbb222: %v", err)
	}
	if got == nil {
		t.Fatal("snapshot missing for bbb222")
	}
	if got.Body != `{"b":"new"}` {
		t.Errorf("approved Body: got %q, want %q", got.Body, `{"b":"new"}`)
	}

	// Verify the other two entries are still pending.
	remaining, err := mgr.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining pending, got %d", len(remaining))
	}

	remainingHashes := make(map[string]bool)
	for _, r := range remaining {
		remainingHashes[r.RequestHash] = true
	}
	if !remainingHashes["aaa111"] {
		t.Error("expected aaa111 to still be pending")
	}
	if !remainingHashes["ccc333"] {
		t.Error("expected ccc333 to still be pending")
	}
	if remainingHashes["bbb222"] {
		t.Error("bbb222 should not be pending after ApproveOne")
	}

	// Verify the unapproved snapshots were NOT updated.
	for _, hash := range []string{"aaa111", "ccc333"} {
		host := "api.example.com"
		if hash == "ccc333" {
			host = "api.other.com"
		}
		entry, err := store.Lookup(host, hash)
		if err != nil {
			t.Fatalf("Lookup %s: %v", hash, err)
		}
		if entry == nil {
			t.Fatalf("snapshot missing for %s", hash)
		}
		if entry.Body != "old-body" {
			t.Errorf("hash %s: Body should still be old, got %q", hash, entry.Body)
		}
		if entry.StatusCode != 999 {
			t.Errorf("hash %s: StatusCode should still be 999, got %d", hash, entry.StatusCode)
		}
	}
}

func TestApproveOne_NonExistentHash_ReturnsError(t *testing.T) {
	snapDir := t.TempDir()
	pendingDir := t.TempDir()

	store := snapshot.NewSnapshotStore(snapDir)
	mgr := NewApprovalManager(store, pendingDir)

	err := mgr.ApproveOne("nonexistent-hash")
	if err == nil {
		t.Fatal("expected error for non-existent hash, got nil")
	}
}
