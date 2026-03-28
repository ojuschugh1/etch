package approval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// PendingDiff represents a live response that differs from the stored snapshot.
type PendingDiff struct {
	RequestHash string                 `json:"request_hash"`
	Host        string                 `json:"host"`
	Live        snapshot.SnapshotEntry `json:"live"`
	Diffs       []diff.FieldDiff       `json:"diffs"`
}

// ApprovalManager manages the approval workflow for updating snapshots.
type ApprovalManager struct {
	Store      *snapshot.SnapshotStore
	PendingDir string
}

// NewApprovalManager creates an ApprovalManager backed by the given store
// with pending diffs stored in pendingDir.
func NewApprovalManager(store *snapshot.SnapshotStore, pendingDir string) *ApprovalManager {
	return &ApprovalManager{
		Store:      store,
		PendingDir: pendingDir,
	}
}

// SavePending writes a PendingDiff to disk as {pendingDir}/{requestHash}.json.
func (a *ApprovalManager) SavePending(p PendingDiff) error {
	if err := os.MkdirAll(a.PendingDir, 0755); err != nil {
		return fmt.Errorf("creating pending directory: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pending diff: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(a.PendingDir, p.RequestHash+".json")
	return os.WriteFile(path, data, 0644)
}

// ApproveAll iterates all pending diffs, updates the snapshot store with
// each live entry, and removes the pending files. Returns the count of
// approved diffs.
func (a *ApprovalManager) ApproveAll() (int, error) {
	pending, err := a.ListPending()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, p := range pending {
		if err := a.Store.Record(p.Host, p.RequestHash, p.Live); err != nil {
			return count, fmt.Errorf("updating snapshot for hash %s: %w", p.RequestHash, err)
		}
		path := filepath.Join(a.PendingDir, p.RequestHash+".json")
		if err := os.Remove(path); err != nil {
			return count, fmt.Errorf("removing pending file for hash %s: %w", p.RequestHash, err)
		}
		count++
	}
	return count, nil
}

// ApproveOne updates the snapshot for a single request hash and removes
// its pending file. Returns an error if the hash is not found.
func (a *ApprovalManager) ApproveOne(requestHash string) error {
	path := filepath.Join(a.PendingDir, requestHash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no pending diff found for hash %s", requestHash)
		}
		return fmt.Errorf("reading pending file for hash %s: %w", requestHash, err)
	}

	var p PendingDiff
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("parsing pending file for hash %s: %w", requestHash, err)
	}

	if err := a.Store.Record(p.Host, p.RequestHash, p.Live); err != nil {
		return fmt.Errorf("updating snapshot for hash %s: %w", requestHash, err)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing pending file for hash %s: %w", requestHash, err)
	}
	return nil
}

// HasPendingDiffs returns true if there are any pending diff files.
func (a *ApprovalManager) HasPendingDiffs() (bool, error) {
	entries, err := os.ReadDir(a.PendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading pending directory: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			return true, nil
		}
	}
	return false, nil
}

// ListPending returns all pending PendingDiff entries from the pending directory.
func (a *ApprovalManager) ListPending() ([]PendingDiff, error) {
	entries, err := os.ReadDir(a.PendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pending directory: %w", err)
	}

	var result []PendingDiff
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.PendingDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading pending file %s: %w", e.Name(), err)
		}
		var p PendingDiff
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing pending file %s: %w", e.Name(), err)
		}
		result = append(result, p)
	}
	return result, nil
}

// ApproveByPattern approves all pending diffs where at least one field diff
// path contains the given pattern string. Returns the count of approved diffs.
// This is useful for bulk-approving known field renames like "user_id -> userId".
func (a *ApprovalManager) ApproveByPattern(pattern string) (int, error) {
	pending, err := a.ListPending()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, p := range pending {
		matches := false
		for _, d := range p.Diffs {
			if strings.Contains(d.Path, pattern) {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}

		if err := a.Store.Record(p.Host, p.RequestHash, p.Live); err != nil {
			return count, fmt.Errorf("updating snapshot for hash %s: %w", p.RequestHash, err)
		}
		path := filepath.Join(a.PendingDir, p.RequestHash+".json")
		if err := os.Remove(path); err != nil {
			return count, fmt.Errorf("removing pending file for hash %s: %w", p.RequestHash, err)
		}
		count++
	}
	return count, nil
}
