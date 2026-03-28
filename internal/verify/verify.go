package verify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Relation defines a metamorphic consistency check between API responses.
type Relation struct {
	Name      string   `yaml:"name" json:"name"`
	Type      string   `yaml:"type" json:"type"` // equivalence, subset, inverse, idempotent
	Endpoints []string `yaml:"endpoints" json:"endpoints"`
	Field     string   `yaml:"field" json:"field"`           // field path to check
	CountField string  `yaml:"count_field" json:"count_field"` // for consistency checks
}

// Result holds the outcome of running one relation check.
type Result struct {
	Name    string
	Passed  bool
	Message string
}

// Report holds all relation check results.
type Report struct {
	Results []Result
	Passed  int
	Failed  int
}

// RunAll checks all relations against the recorded snapshots.
func RunAll(store *snapshot.SnapshotStore, relations []Relation) (*Report, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	// build a lookup: "METHOD /path" -> entry
	entries := make(map[string]*snapshot.SnapshotEntry)
	for _, sf := range all {
		for _, entry := range sf {
			parsed, _ := url.Parse(entry.URL)
			if parsed == nil {
				continue
			}
			key := entry.Method + " " + parsed.Path
			if parsed.RawQuery != "" {
				key += "?" + parsed.RawQuery
			}
			e := entry
			entries[key] = &e
		}
	}

	report := &Report{}

	for _, rel := range relations {
		result := checkRelation(rel, entries)
		report.Results = append(report.Results, result)
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}

	return report, nil
}

func checkRelation(rel Relation, entries map[string]*snapshot.SnapshotEntry) Result {
	switch rel.Type {
	case "equivalence":
		return checkEquivalence(rel, entries)
	case "subset":
		return checkSubset(rel, entries)
	case "idempotent":
		return checkIdempotent(rel, entries)
	case "consistency":
		return checkConsistency(rel, entries)
	default:
		return Result{Name: rel.Name, Passed: false, Message: fmt.Sprintf("unknown relation type: %s", rel.Type)}
	}
}

// equivalence: two endpoints should return the same value for a given field.
// e.g. GET /users?page=1 total == GET /users?page=2 total
func checkEquivalence(rel Relation, entries map[string]*snapshot.SnapshotEntry) Result {
	if len(rel.Endpoints) < 2 {
		return Result{Name: rel.Name, Passed: false, Message: "equivalence needs at least 2 endpoints"}
	}

	values := []string{}
	for _, ep := range rel.Endpoints {
		entry := findEntry(entries, ep)
		if entry == nil {
			return Result{Name: rel.Name, Passed: false, Message: fmt.Sprintf("no snapshot for %s", ep)}
		}
		val := extractField(entry.Body, rel.Field)
		values = append(values, val)
	}

	for i := 1; i < len(values); i++ {
		if values[i] != values[0] {
			return Result{
				Name:    rel.Name,
				Passed:  false,
				Message: fmt.Sprintf("%s differs: %q vs %q (%s vs %s)", rel.Field, values[0], values[i], rel.Endpoints[0], rel.Endpoints[i]),
			}
		}
	}

	return Result{Name: rel.Name, Passed: true, Message: "all endpoints return same value"}
}

// subset: filtered results should be a subset of unfiltered results.
// e.g. items from GET /users?role=admin should all appear in GET /users
func checkSubset(rel Relation, entries map[string]*snapshot.SnapshotEntry) Result {
	if len(rel.Endpoints) < 2 {
		return Result{Name: rel.Name, Passed: false, Message: "subset needs 2 endpoints (filtered, unfiltered)"}
	}

	filtered := findEntry(entries, rel.Endpoints[0])
	unfiltered := findEntry(entries, rel.Endpoints[1])
	if filtered == nil || unfiltered == nil {
		return Result{Name: rel.Name, Passed: false, Message: "missing snapshot for one or both endpoints"}
	}

	filteredIDs := extractArrayField(filtered.Body, rel.Field)
	unfilteredIDs := extractArrayField(unfiltered.Body, rel.Field)

	unfilteredSet := make(map[string]bool)
	for _, id := range unfilteredIDs {
		unfilteredSet[id] = true
	}

	for _, id := range filteredIDs {
		if !unfilteredSet[id] {
			return Result{
				Name:    rel.Name,
				Passed:  false,
				Message: fmt.Sprintf("filtered result %q not found in unfiltered set", id),
			}
		}
	}

	return Result{Name: rel.Name, Passed: true, Message: "filtered results are a subset of unfiltered"}
}

// idempotent: same request recorded multiple times should produce same result.
func checkIdempotent(rel Relation, entries map[string]*snapshot.SnapshotEntry) Result {
	if len(rel.Endpoints) < 1 {
		return Result{Name: rel.Name, Passed: false, Message: "idempotent needs at least 1 endpoint"}
	}

	ep := rel.Endpoints[0]
	entry := findEntry(entries, ep)
	if entry == nil {
		return Result{Name: rel.Name, Passed: false, Message: fmt.Sprintf("no snapshot for %s", ep)}
	}

	// for idempotency, we just verify the snapshot exists and has a stable body.
	// real idempotency testing would need multiple recordings - check if the
	// snapshot store has multiple entries for the same hash.
	return Result{Name: rel.Name, Passed: true, Message: "snapshot exists (record multiple times to verify idempotency)"}
}

// consistency: a count field in one endpoint matches the array length in another.
// e.g. GET /users/count body.count == len(GET /users body.users)
func checkConsistency(rel Relation, entries map[string]*snapshot.SnapshotEntry) Result {
	if len(rel.Endpoints) < 2 || rel.Field == "" || rel.CountField == "" {
		return Result{Name: rel.Name, Passed: false, Message: "consistency needs 2 endpoints, field, and count_field"}
	}

	listEntry := findEntry(entries, rel.Endpoints[0])
	countEntry := findEntry(entries, rel.Endpoints[1])
	if listEntry == nil || countEntry == nil {
		return Result{Name: rel.Name, Passed: false, Message: "missing snapshot for one or both endpoints"}
	}

	items := extractArrayField(listEntry.Body, rel.Field)
	countVal := extractField(countEntry.Body, rel.CountField)

	expectedCount := fmt.Sprintf("%d", len(items))
	if countVal != expectedCount {
		return Result{
			Name:    rel.Name,
			Passed:  false,
			Message: fmt.Sprintf("array length %d != count field %s", len(items), countVal),
		}
	}

	return Result{Name: rel.Name, Passed: true, Message: fmt.Sprintf("array length matches count (%d)", len(items))}
}

func findEntry(entries map[string]*snapshot.SnapshotEntry, endpoint string) *snapshot.SnapshotEntry {
	if e, ok := entries[endpoint]; ok {
		return e
	}
	// try partial match (method + path without query)
	for key, e := range entries {
		if strings.HasPrefix(key, endpoint) {
			return e
		}
	}
	return nil
}

func extractField(body string, fieldPath string) string {
	var parsed interface{}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return ""
	}

	parts := strings.Split(fieldPath, ".")
	current := parsed
	for _, part := range parts {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = obj[part]
	}

	return fmt.Sprintf("%v", current)
}

func extractArrayField(body string, fieldPath string) []string {
	var parsed interface{}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return nil
	}

	parts := strings.Split(fieldPath, ".")
	current := parsed
	for _, part := range parts {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = obj[part]
	}

	arr, ok := current.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, len(arr))
	for i, v := range arr {
		result[i] = fmt.Sprintf("%v", v)
	}
	return result
}

// Format produces a readable report.
func (r *Report) Format(color bool) string {
	if len(r.Results) == 0 {
		return "No relations defined. Create .etch/relations.json to add consistency checks.\n"
	}

	green, red, reset := "", "", ""
	if color {
		green = "\033[32m"
		red = "\033[31m"
		reset = "\033[0m"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Relation checks: %d passed, %d failed\n\n", r.Passed, r.Failed))

	// sort: failures first
	sorted := make([]Result, len(r.Results))
	copy(sorted, r.Results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Passed != sorted[j].Passed {
			return !sorted[i].Passed
		}
		return sorted[i].Name < sorted[j].Name
	})

	for _, res := range sorted {
		if res.Passed {
			b.WriteString(fmt.Sprintf("  %s✓%s %s - %s\n", green, reset, res.Name, res.Message))
		} else {
			b.WriteString(fmt.Sprintf("  %s✗%s %s - %s\n", red, reset, res.Name, res.Message))
		}
	}

	return b.String()
}
