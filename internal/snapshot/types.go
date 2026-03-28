package snapshot

// SnapshotEntry represents a single recorded HTTP response.
type SnapshotEntry struct {
	Method      string              `json:"method"`
	URL         string              `json:"url"`
	StatusCode  int                 `json:"status_code"`
	Headers     map[string][]string `json:"headers"`
	Body        string              `json:"body"`
	RequestBody string              `json:"request_body,omitempty"`
}

// SnapshotFile is a map of RequestHash to SnapshotEntry.
type SnapshotFile map[string]SnapshotEntry

// TestSummary holds the results of a test run.
// Diffs is []interface{} to avoid an import cycle with the diff package; type-assert to diff.DiffResult.
type TestSummary struct {
	TotalRequests int
	Matches       int
	Mismatches    int
	Unrecorded    int
	Diffs         []interface{}
}
