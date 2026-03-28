package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// DiffEngine compares stored and live snapshot entries.
type DiffEngine struct {
	IgnoreRules IgnoreChecker
	Normalizer  ValueNormalizer
	SchemaMode  bool // when true, compare types/structure only, ignore values
}

// IgnoreChecker decides whether a field path should be skipped during comparison.
type IgnoreChecker interface {
	ShouldIgnore(path string) bool
}

// ValueNormalizer replaces dynamic values with stable placeholders before comparison.
type ValueNormalizer interface {
	NormalizeBody(body string) string
	NormalizeHeaders(headers map[string][]string) map[string][]string
}

// NewDiffEngine creates a new DiffEngine with no ignore rules.
func NewDiffEngine() *DiffEngine {
	return &DiffEngine{}
}

// NewDiffEngineWithIgnore creates a DiffEngine that skips fields matching the given rules.
func NewDiffEngineWithIgnore(rules IgnoreChecker) *DiffEngine {
	return &DiffEngine{IgnoreRules: rules}
}

// NewDiffEngineFull creates a DiffEngine with both ignore rules and normalization.
func NewDiffEngineFull(rules IgnoreChecker, normalizer ValueNormalizer) *DiffEngine {
	return &DiffEngine{IgnoreRules: rules, Normalizer: normalizer}
}

// Compare compares stored vs live snapshot, filtering ignored fields.
func (d *DiffEngine) Compare(stored, live *snapshot.SnapshotEntry) DiffResult {
	var fields []FieldDiff

	// normalize before comparing if configured
	storedCmp, liveCmp := stored, live
	if d.Normalizer != nil {
		sc := *stored
		sc.Body = d.Normalizer.NormalizeBody(stored.Body)
		sc.Headers = d.Normalizer.NormalizeHeaders(stored.Headers)
		storedCmp = &sc

		lc := *live
		lc.Body = d.Normalizer.NormalizeBody(live.Body)
		lc.Headers = d.Normalizer.NormalizeHeaders(live.Headers)
		liveCmp = &lc
	}

	// status code
	if storedCmp.StatusCode != liveCmp.StatusCode {
		fields = append(fields, FieldDiff{
			Path:     "status_code",
			Expected: strconv.Itoa(storedCmp.StatusCode),
			Actual:   strconv.Itoa(liveCmp.StatusCode),
		})
	}

	// headers
	fields = append(fields, compareHeaders(storedCmp.Headers, liveCmp.Headers)...)

	// body
	if d.SchemaMode {
		// schema mode: compare types and structure only, ignore values
		storedSchema := InferSchema(storedCmp.Body)
		liveSchema := InferSchema(liveCmp.Body)
		if storedSchema != nil && liveSchema != nil {
			fields = append(fields, CompareSchemas("body", storedSchema, liveSchema)...)
		} else if storedCmp.Body != liveCmp.Body {
			// non-JSON bodies fall back to exact comparison
			fields = append(fields, compareBodies(storedCmp.Body, liveCmp.Body)...)
		}
	} else {
		fields = append(fields, compareBodies(storedCmp.Body, liveCmp.Body)...)
	}

	// Filter out ignored fields
	if d.IgnoreRules != nil {
		filtered := fields[:0]
		for _, f := range fields {
			if !d.IgnoreRules.ShouldIgnore(f.Path) {
				filtered = append(filtered, f)
			}
		}
		fields = filtered
	}

	return DiffResult{
		Matched: len(fields) == 0,
		Fields:  fields,
	}
}

// compareHeaders produces FieldDiffs for header differences between stored and live.
func compareHeaders(stored, live map[string][]string) []FieldDiff {
	var fields []FieldDiff

	// Collect all header keys from both maps
	allKeys := make(map[string]bool)
	for k := range stored {
		allKeys[k] = true
	}
	for k := range live {
		allKeys[k] = true
	}

	// Sort keys for deterministic output
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		storedVals, storedOK := stored[key]
		liveVals, liveOK := live[key]

		if !storedOK {
			// Header only in live
			fields = append(fields, FieldDiff{
				Path:     fmt.Sprintf("headers.%s", key),
				Expected: "",
				Actual:   formatHeaderValues(liveVals),
			})
		} else if !liveOK {
			// Header only in stored
			fields = append(fields, FieldDiff{
				Path:     fmt.Sprintf("headers.%s", key),
				Expected: formatHeaderValues(storedVals),
				Actual:   "",
			})
		} else if !headerValuesEqual(storedVals, liveVals) {
			fields = append(fields, FieldDiff{
				Path:     fmt.Sprintf("headers.%s", key),
				Expected: formatHeaderValues(storedVals),
				Actual:   formatHeaderValues(liveVals),
			})
		}
	}

	return fields
}

func headerValuesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func formatHeaderValues(vals []string) string {
	if len(vals) == 1 {
		return vals[0]
	}
	b, _ := json.Marshal(vals)
	return string(b)
}

// compareBodies diffs two bodies. Field-level if both are JSON, whole-body otherwise.
func compareBodies(stored, live string) []FieldDiff {
	var storedJSON, liveJSON interface{}
	storedErr := json.Unmarshal([]byte(stored), &storedJSON)
	liveErr := json.Unmarshal([]byte(live), &liveJSON)

	if storedErr == nil && liveErr == nil {
		// Both are valid JSON - do field-level diff
		return diffJSON("body", storedJSON, liveJSON)
	}

	// Non-JSON or mixed - whole-body comparison
	if stored != live {
		return []FieldDiff{{
			Path:     "body",
			Expected: stored,
			Actual:   live,
		}}
	}
	return nil
}

// diffJSON recursively compares two JSON values and returns field-level diffs.
func diffJSON(path string, expected, actual interface{}) []FieldDiff {
	// If both are nil/null
	if expected == nil && actual == nil {
		return nil
	}

	// Handle type mismatches or one being nil
	if expected == nil || actual == nil {
		return []FieldDiff{{
			Path:     path,
			Expected: jsonString(expected),
			Actual:   jsonString(actual),
		}}
	}

	switch e := expected.(type) {
	case map[string]interface{}:
		a, ok := actual.(map[string]interface{})
		if !ok {
			return []FieldDiff{{
				Path:     path,
				Expected: jsonString(expected),
				Actual:   jsonString(actual),
			}}
		}
		return diffJSONObjects(path, e, a)

	case []interface{}:
		a, ok := actual.([]interface{})
		if !ok {
			return []FieldDiff{{
				Path:     path,
				Expected: jsonString(expected),
				Actual:   jsonString(actual),
			}}
		}
		return diffJSONArrays(path, e, a)

	default:
		// Scalar comparison (string, float64, bool)
		if fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual) {
			return []FieldDiff{{
				Path:     path,
				Expected: jsonString(expected),
				Actual:   jsonString(actual),
			}}
		}
		return nil
	}
}

func diffJSONObjects(path string, expected, actual map[string]interface{}) []FieldDiff {
	var fields []FieldDiff

	// Collect all keys
	allKeys := make(map[string]bool)
	for k := range expected {
		allKeys[k] = true
	}
	for k := range actual {
		allKeys[k] = true
	}

	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		childPath := fmt.Sprintf("%s.%s", path, key)
		eVal, eOK := expected[key]
		aVal, aOK := actual[key]

		if !eOK {
			// Key only in actual
			fields = append(fields, FieldDiff{
				Path:     childPath,
				Expected: "",
				Actual:   jsonString(aVal),
			})
		} else if !aOK {
			// Key only in expected
			fields = append(fields, FieldDiff{
				Path:     childPath,
				Expected: jsonString(eVal),
				Actual:   "",
			})
		} else {
			fields = append(fields, diffJSON(childPath, eVal, aVal)...)
		}
	}

	return fields
}

func diffJSONArrays(path string, expected, actual []interface{}) []FieldDiff {
	// if same length, try order-insensitive matching first.
	// serialize each element and check if both arrays contain the same set.
	if len(expected) == len(actual) && len(expected) > 0 {
		if arraysEqualUnordered(expected, actual) {
			return nil // same elements, just reordered
		}
	}

	// different lengths or different content - fall back to positional diff
	var fields []FieldDiff
	maxLen := len(expected)
	if len(actual) > maxLen {
		maxLen = len(actual)
	}

	for i := 0; i < maxLen; i++ {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		if i >= len(expected) {
			fields = append(fields, FieldDiff{
				Path:     childPath,
				Expected: "",
				Actual:   jsonString(actual[i]),
			})
		} else if i >= len(actual) {
			fields = append(fields, FieldDiff{
				Path:     childPath,
				Expected: jsonString(expected[i]),
				Actual:   "",
			})
		} else {
			fields = append(fields, diffJSON(childPath, expected[i], actual[i])...)
		}
	}

	return fields
}

// arraysEqualUnordered checks if two arrays have the same elements regardless of order.
func arraysEqualUnordered(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}

	// serialize each element and sort
	aStrs := make([]string, len(a))
	bStrs := make([]string, len(b))
	for i := range a {
		aStrs[i] = jsonString(a[i])
		bStrs[i] = jsonString(b[i])
	}
	sort.Strings(aStrs)
	sort.Strings(bStrs)

	for i := range aStrs {
		if aStrs[i] != bStrs[i] {
			return false
		}
	}
	return true
}

// jsonString converts a JSON value to its string representation.
func jsonString(v interface{}) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
