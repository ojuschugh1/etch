package proxy

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ojuschugh1/etch/internal/envvar"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/redact"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// RecordHandler handles intercepted request/response pairs in record mode
// by computing a request hash and persisting the response as a snapshot.
type RecordHandler struct {
	HashComputer  *hash.HashComputer
	SnapshotStore *snapshot.SnapshotStore
	EnvExpander   *envvar.Expander // optional, collapses URLs to {{VAR}} placeholders
	Redactor      *redact.Redactor // optional, scrubs PII/secrets before saving
}

// NewRecordHandler creates a RecordHandler with the given hash computer and
// snapshot store.
func NewRecordHandler(hc *hash.HashComputer, ss *snapshot.SnapshotStore) *RecordHandler {
	return &RecordHandler{
		HashComputer:  hc,
		SnapshotStore: ss,
	}
}

// HandleRequest computes a deterministic hash from the request, reads the
// response body, and persists the snapshot entry keyed by host and hash.
func (h *RecordHandler) HandleRequest(req *http.Request, resp *http.Response) error {
	// Read the request body if present (for POST/PUT/PATCH).
	var reqBodyStr string
	if req.Body != nil {
		reqBodyBytes, err := io.ReadAll(req.Body)
		if err == nil && len(reqBodyBytes) > 0 {
			reqBodyStr = string(reqBodyBytes)
		}
	}

	// Compute request hash from method, URL, headers, and request body.
	reqHash, err := h.HashComputer.ComputeHash(req.Method, req.URL.String(), req.Header, reqBodyStr)
	if err != nil {
		return fmt.Errorf("record: compute hash: %w", err)
	}

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("record: read response body: %w", err)
	}

	// Extract host from the request URL.
	host := req.URL.Host

	// Build the snapshot entry.
	// If env expander is configured, collapse the URL to use {{VAR}} placeholders
	// so the same snapshot works across environments.
	entryURL := req.URL.String()
	if h.EnvExpander != nil {
		entryURL = h.EnvExpander.Collapse(entryURL)
	}

	entry := snapshot.SnapshotEntry{
		Method:      req.Method,
		URL:         entryURL,
		StatusCode:  resp.StatusCode,
		Headers:     cloneHeaders(resp.Header),
		Body:        string(body),
		RequestBody: reqBodyStr,
	}

	// scrub PII and secrets if redactor is configured
	if h.Redactor != nil {
		entry.Body = h.Redactor.RedactBody(entry.Body)
		entry.Headers = h.Redactor.RedactHeaders(entry.Headers)
		if entry.RequestBody != "" {
			entry.RequestBody = h.Redactor.RedactBody(entry.RequestBody)
		}
	}

	// Warn if sensitive content detected and redaction is not enabled
	if h.Redactor == nil {
		scanner := redact.New()
		if scanner.HasSensitiveContent(entry.Body) || scanner.HasSensitiveContent(entry.RequestBody) {
			fmt.Fprintf(os.Stderr, "⚠ Sensitive content detected in %s %s. Consider using --redact flag.\n", req.Method, req.URL.Path)
		}
	}

	// Persist the snapshot.
	if err := h.SnapshotStore.Record(host, reqHash, entry); err != nil {
		return fmt.Errorf("record: persist snapshot: %w", err)
	}

	return nil
}

// cloneHeaders returns a deep copy of an http.Header as map[string][]string.
func cloneHeaders(h http.Header) map[string][]string {
	clone := make(map[string][]string, len(h))
	for k, vv := range h {
		vals := make([]string, len(vv))
		copy(vals, vv)
		clone[k] = vals
	}
	return clone
}
