package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SnapshotStore reads and writes snapshot files organized by host.
type SnapshotStore struct {
	Dir string
	mu  sync.Mutex // protects concurrent read-modify-write in Record
}

// NewSnapshotStore creates a SnapshotStore rooted at dir.
func NewSnapshotStore(dir string) *SnapshotStore {
	return &SnapshotStore{Dir: dir}
}

// filePath returns the on-disk path for a given host's snapshot file.
func (s *SnapshotStore) filePath(host string) string {
	return filepath.Join(s.Dir, host+".snap")
}

// Load reads the snapshot file for host. Returns empty file if not found.
func (s *SnapshotStore) Load(host string) (SnapshotFile, error) {
	data, err := os.ReadFile(s.filePath(host))
	if err != nil {
		if os.IsNotExist(err) {
			return make(SnapshotFile), nil
		}
		return nil, fmt.Errorf("reading snapshot file for %s: %w", host, err)
	}

	var sf SnapshotFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parsing snapshot file for %s: %w", host, err)
	}
	return sf, nil
}

// Save writes the snapshot file atomically (temp file + rename).
func (s *SnapshotStore) Save(host string, file SnapshotFile) error {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	data, err := marshalSnapshotFile(file)
	if err != nil {
		return fmt.Errorf("marshaling snapshot file for %s: %w", host, err)
	}

	target := s.filePath(host)

	// write to a temp file then rename to avoid half-written state
	tmp, err := os.CreateTemp(s.Dir, ".snap-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", host, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp snapshot for %s: %w", host, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp snapshot for %s: %w", host, err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp snapshot for %s: %w", host, err)
	}

	return nil
}

// Record upserts an entry in the snapshot file for host. Thread-safe.
func (s *SnapshotStore) Record(host, hash string, entry SnapshotEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sf, err := s.Load(host)
	if err != nil {
		return err
	}
	sf[hash] = entry
	return s.Save(host, sf)
}

// Lookup returns the snapshot entry for hash, or nil if not found.
func (s *SnapshotStore) Lookup(host, hash string) (*SnapshotEntry, error) {
	sf, err := s.Load(host)
	if err != nil {
		return nil, err
	}
	entry, ok := sf[hash]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

// LoadAll loads every .snap file in the directory. Returns empty map if dir doesn't exist.
func (s *SnapshotStore) LoadAll() (map[string]SnapshotFile, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]SnapshotFile), nil
		}
		return nil, fmt.Errorf("reading snapshot directory: %w", err)
	}

	result := make(map[string]SnapshotFile)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".snap") {
			continue
		}
		host := strings.TrimSuffix(e.Name(), ".snap")
		sf, err := s.Load(host)
		if err != nil {
			return nil, err
		}
		result[host] = sf
	}
	return result, nil
}

// PruneResult holds the outcome of a prune operation.
type PruneResult struct {
	RemovedHashes []string // hashes that were removed
	RemovedHosts  []string // hosts whose entire snap file was removed
	TotalBefore   int
	TotalAfter    int
}

// Prune removes entries not in hitSet. If dryRun, nothing is deleted.
func (s *SnapshotStore) Prune(hitSet map[string]bool, dryRun bool) (*PruneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.LoadAll()
	if err != nil {
		return nil, err
	}

	result := &PruneResult{}

	for host, sf := range all {
		before := len(sf)
		result.TotalBefore += before

		pruned := make(SnapshotFile)
		for hash, entry := range sf {
			if hitSet[hash] {
				pruned[hash] = entry
			} else {
				result.RemovedHashes = append(result.RemovedHashes, hash)
			}
		}

		result.TotalAfter += len(pruned)

		if !dryRun {
			if len(pruned) == 0 {
				// Remove the entire file
				os.Remove(s.filePath(host))
				result.RemovedHosts = append(result.RemovedHosts, host)
			} else if len(pruned) < before {
				if err := s.Save(host, pruned); err != nil {
					return nil, fmt.Errorf("saving pruned snapshot for %s: %w", host, err)
				}
			}
		}
	}

	return result, nil
}

// marshalSnapshotFile produces deterministic JSON with sorted keys.
func marshalSnapshotFile(sf SnapshotFile) ([]byte, error) {
	// Convert to an ordered structure: map[string] -> ordered entry map.
	// Go's json.Marshal already sorts map[string] keys, but we need
	// SnapshotEntry fields in alphabetical JSON-tag order too.
	ordered := make(map[string]map[string]interface{}, len(sf))
	for hash, entry := range sf {
		ordered[hash] = entryToSortedMap(entry)
	}

	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return nil, err
	}
	// Trailing newline for POSIX-friendly files.
	data = append(data, '\n')
	return data, nil
}

// entryToSortedMap converts a SnapshotEntry to a map for sorted JSON output.
func entryToSortedMap(e SnapshotEntry) map[string]interface{} {
	// Sort header keys and their values for deterministic output.
	sortedHeaders := make(map[string][]string, len(e.Headers))
	for k, vals := range e.Headers {
		sorted := make([]string, len(vals))
		copy(sorted, vals)
		sort.Strings(sorted)
		sortedHeaders[k] = sorted
	}

	m := map[string]interface{}{
		"body":        e.Body,
		"headers":     sortedHeaders,
		"method":      e.Method,
		"status_code": e.StatusCode,
		"url":         e.URL,
	}
	if e.RequestBody != "" {
		m["request_body"] = e.RequestBody
	}
	return m
}
