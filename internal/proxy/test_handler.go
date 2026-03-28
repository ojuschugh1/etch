package proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/ojuschugh1/etch/internal/approval"
	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/envvar"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// TestHandler handles intercepted request/response pairs in test mode by
// comparing live responses against stored snapshots and tracking results.
type TestHandler struct {
	HashComputer    *hash.HashComputer
	SnapshotStore   *snapshot.SnapshotStore
	DiffEngine      *diff.DiffEngine
	ApprovalManager *approval.ApprovalManager
	EnvExpander     *envvar.Expander // optional, collapses URLs before hashing
	Summary         *snapshot.TestSummary
	HitHashes       []string // hashes that were matched during this test run
	mu              sync.Mutex
}

// NewTestHandler creates a TestHandler wired to the given components.
func NewTestHandler(hc *hash.HashComputer, ss *snapshot.SnapshotStore, de *diff.DiffEngine, am *approval.ApprovalManager) *TestHandler {
	return &TestHandler{
		HashComputer:    hc,
		SnapshotStore:   ss,
		DiffEngine:      de,
		ApprovalManager: am,
		Summary:         &snapshot.TestSummary{},
	}
}

// HandleRequest computes the request hash, looks up the stored snapshot,
// and compares the live response against it. Counters are updated
// thread-safely.
func (h *TestHandler) HandleRequest(req *http.Request, resp *http.Response) error {
	// Collapse the URL to match how it was stored during recording.
	// If env vars are configured, https://api.staging.com/users becomes
	// {{BASE_URL}}/users - matching the snapshot's collapsed URL.
	requestURL := req.URL.String()
	if h.EnvExpander != nil {
		requestURL = h.EnvExpander.Collapse(requestURL)
	}

	// Read the request body if present (for POST/PUT/PATCH).
	var reqBodyStr string
	if req.Body != nil {
		reqBodyBytes, err := io.ReadAll(req.Body)
		if err == nil && len(reqBodyBytes) > 0 {
			reqBodyStr = string(reqBodyBytes)
		}
	}

	// Compute request hash from the (possibly collapsed) URL and request body.
	reqHash, err := h.HashComputer.ComputeHash(req.Method, requestURL, req.Header, reqBodyStr)
	if err != nil {
		return fmt.Errorf("test: compute hash: %w", err)
	}

	host := req.URL.Host

	// Read the live response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("test: read response body: %w", err)
	}

	// Lookup stored snapshot.
	stored, err := h.SnapshotStore.Lookup(host, reqHash)
	if err != nil {
		return fmt.Errorf("test: lookup snapshot: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.Summary.TotalRequests++

	if stored == nil {
		// No snapshot found - unrecorded request.
		h.Summary.Unrecorded++
		log.Printf("[test] unrecorded request: %s %s (hash: %s)\n", req.Method, req.URL.String(), reqHash)
		return nil
	}

	// Track this hash as "hit" for pruning support.
	h.HitHashes = append(h.HitHashes, reqHash)

	// Build a live snapshot entry for comparison.
	live := &snapshot.SnapshotEntry{
		Method:     req.Method,
		URL:        req.URL.String(),
		StatusCode: resp.StatusCode,
		Headers:    cloneHeaders(resp.Header),
		Body:       string(body),
	}

	// Compare stored vs live.
	result := h.DiffEngine.Compare(stored, live)
	result.RequestHash = reqHash
	result.URL = req.URL.String()

	if result.Matched {
		h.Summary.Matches++
	} else {
		h.Summary.Mismatches++
		h.Summary.Diffs = append(h.Summary.Diffs, result)

		// Save pending diff for the approval workflow.
		pending := approval.PendingDiff{
			RequestHash: reqHash,
			Host:        host,
			Live:        *live,
			Diffs:       result.Fields,
		}
		if err := h.ApprovalManager.SavePending(pending); err != nil {
			log.Printf("[test] failed to save pending diff for %s: %v", reqHash, err)
		}

		// Log the diff to stderr.
		formatted := h.DiffEngine.FormatDiff(result, false)
		log.Printf("[test] mismatch detected:\n%s", formatted)
	}

	return nil
}

// GetSummary returns a copy of the current test summary.
func (h *TestHandler) GetSummary() snapshot.TestSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	return *h.Summary
}

// GetHitHashes returns the list of snapshot hashes that were matched.
func (h *TestHandler) GetHitHashes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]string, len(h.HitHashes))
	copy(cp, h.HitHashes)
	return cp
}
