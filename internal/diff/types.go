package diff

// DiffResult represents the comparison result for a single request.
type DiffResult struct {
	RequestHash string
	URL         string
	Matched     bool
	Fields      []FieldDiff
}

// FieldDiff represents a single field-level difference.
type FieldDiff struct {
	Path     string
	Expected string
	Actual   string
}
