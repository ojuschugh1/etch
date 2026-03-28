package mock

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Server serves recorded snapshots as HTTP responses.
type Server struct {
	store   *snapshot.SnapshotStore
	entries map[string]*snapshot.SnapshotEntry // keyed by "METHOD /path?sorted_query"
	byPath  map[string]*snapshot.SnapshotEntry // fallback keyed by "METHOD /path" (no query)
	addr    string
	Latency time.Duration // optional simulated latency
}

// NewServer loads all snapshots and builds lookup tables.
func NewServer(store *snapshot.SnapshotStore, addr string) (*Server, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	entries := make(map[string]*snapshot.SnapshotEntry)
	byPath := make(map[string]*snapshot.SnapshotEntry)
	for _, sf := range all {
		for _, entry := range sf {
			e := entry // copy
			parsed, err := url.Parse(entry.URL)
			if err != nil {
				continue
			}
			// Full key with sorted query params for exact matching
			fullKey := entry.Method + " " + parsed.Path
			if parsed.RawQuery != "" {
				fullKey += "?" + sortQuery(parsed.Query())
			}
			entries[fullKey] = &e

			// Fallback key without query params
			pathKey := entry.Method + " " + parsed.Path
			byPath[pathKey] = &e
		}
	}

	return &Server{store: store, entries: entries, byPath: byPath, addr: addr}, nil
}

// Start begins serving mock responses.
func (s *Server) Start() error {
	if len(s.entries) == 0 {
		return fmt.Errorf("no snapshots found - record some traffic first")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)

	fmt.Printf("etch mock server listening on %s (%d endpoints)\n", s.addr, len(s.entries))
	s.printEndpoints()

	return http.ListenAndServe(s.addr, mux)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// Simulate latency if configured
	if s.Latency > 0 {
		time.Sleep(s.Latency)
	}

	entry := s.findEntry(r)
	if entry == nil {
		http.Error(w, fmt.Sprintf("no snapshot for %s %s", r.Method, r.URL.Path), http.StatusNotFound)
		log.Printf("[mock] 404 %s %s", r.Method, r.URL.Path)
		return
	}

	for k, vals := range entry.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(entry.StatusCode)
	w.Write([]byte(entry.Body))

	log.Printf("[mock] %d %s %s", entry.StatusCode, r.Method, r.URL.Path)
}

// findEntry looks up a snapshot with progressive fallback:
// exact (method+path+query) -> method+path -> no trailing slash.
func (s *Server) findEntry(r *http.Request) *snapshot.SnapshotEntry {
	// Try exact match with query params
	key := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + sortQuery(r.URL.Query())
	}
	if entry, ok := s.entries[key]; ok {
		// If the snapshot has a request body, check if it matches
		if entry.RequestBody != "" && r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			if string(body) == entry.RequestBody {
				return entry
			}
		} else {
			return entry
		}
	}

	// Fallback: method + path only
	pathKey := r.Method + " " + r.URL.Path
	if entry, ok := s.byPath[pathKey]; ok {
		return entry
	}

	// Try without trailing slash
	alt := strings.TrimSuffix(pathKey, "/")
	if entry, ok := s.byPath[alt]; ok {
		return entry
	}

	return nil
}

// sortQuery produces a deterministic query string from url.Values.
func sortQuery(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := make([]string, len(params[k]))
		copy(vals, params[k])
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func (s *Server) printEndpoints() {
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := s.entries[k]
		fmt.Printf("  %s -> %d\n", k, e.StatusCode)
	}
}

// EntryCount returns the number of mock endpoints available.
func (s *Server) EntryCount() int {
	return len(s.entries)
}
