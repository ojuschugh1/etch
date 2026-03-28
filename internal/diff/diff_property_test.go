package diff

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
	"pgregory.net/rapid"
)

// If two entries differ in status, headers, or body, Compare should catch it.
// We randomly pick which fields to change and verify each one shows up in the diff.
func TestDiffDetectsFieldDifferences(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		engine := NewDiffEngine()

		// Generate a base entry
		stored := genDiffSnapshotEntry(rt, "stored")

		// Decide which top-level fields to make different (at least one must differ)
		diffStatusCode := rapid.Bool().Draw(rt, "diffStatusCode")
		diffHeaders := rapid.Bool().Draw(rt, "diffHeaders")
		diffBody := rapid.Bool().Draw(rt, "diffBody")

		// Ensure at least one field differs
		if !diffStatusCode && !diffHeaders && !diffBody {
			// Force at least one difference
			switch rapid.IntRange(0, 2).Draw(rt, "forceDiff") {
			case 0:
				diffStatusCode = true
			case 1:
				diffHeaders = true
			case 2:
				diffBody = true
			}
		}

		// Build the live entry starting from a copy of stored
		live := &snapshot.SnapshotEntry{
			Method:     stored.Method,
			URL:        stored.URL,
			StatusCode: stored.StatusCode,
			Headers:    copyHeaders(stored.Headers),
			Body:       stored.Body,
		}

		// Mutate the fields that should differ
		if diffStatusCode {
			live.StatusCode = genDifferentStatusCode(rt, stored.StatusCode)
		}
		if diffHeaders {
			live.Headers = genDifferentHeaders(rt, stored.Headers)
		}
		if diffBody {
			live.Body = genDifferentBody(rt, stored.Body)
		}

		result := engine.Compare(stored, live)

		// Matched must be false since at least one field differs
		if result.Matched {
			rt.Fatalf("expected Matched=false when entries differ (diffStatus=%v, diffHeaders=%v, diffBody=%v)",
				diffStatusCode, diffHeaders, diffBody)
		}

		// Check that each differing top-level field has at least one FieldDiff
		if diffStatusCode {
			if !hasFieldWithPrefix(result.Fields, "status_code") {
				rt.Fatalf("expected FieldDiff for status_code (stored=%d, live=%d), got fields: %+v",
					stored.StatusCode, live.StatusCode, result.Fields)
			}
		}
		if diffHeaders {
			if !hasFieldWithPrefix(result.Fields, "headers.") {
				rt.Fatalf("expected FieldDiff for headers, got fields: %+v", result.Fields)
			}
		}
		if diffBody {
			if !hasFieldWithPrefix(result.Fields, "body") {
				rt.Fatalf("expected FieldDiff for body, got fields: %+v", result.Fields)
			}
		}
	})
}

// --- Generators and helpers ---

func genDiffSnapshotEntry(t *rapid.T, label string) *snapshot.SnapshotEntry {
	return &snapshot.SnapshotEntry{
		Method:     rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE"}).Draw(t, label+"Method"),
		URL:        "https://api.example.com/test",
		StatusCode: rapid.SampledFrom([]int{200, 201, 204, 301, 400, 404, 500}).Draw(t, label+"StatusCode"),
		Headers:    genDiffHeaderMap(t, label),
		Body:       genDiffBody(t, label),
	}
}

func genDiffHeaderMap(t *rapid.T, label string) map[string][]string {
	headers := make(map[string][]string)
	numHeaders := rapid.IntRange(1, 3).Draw(t, label+"NumHeaders")
	keys := []string{"Content-Type", "X-Request-Id", "Accept", "Cache-Control", "X-Custom"}
	for i := 0; i < numHeaders; i++ {
		key := rapid.SampledFrom(keys).Draw(t, label+"HeaderKey")
		val := rapid.SampledFrom([]string{
			"application/json", "text/plain", "text/html",
			"no-cache", "max-age=3600", "gzip",
		}).Draw(t, label+"HeaderVal")
		headers[key] = []string{val}
	}
	return headers
}

func genDiffBody(t *rapid.T, label string) string {
	// Generate simple JSON or plain text bodies
	return rapid.SampledFrom([]string{
		`{"name":"Alice","age":30}`,
		`{"name":"Bob","age":25}`,
		`{"items":[1,2,3]}`,
		`{"active":true}`,
		`plain text body`,
		`another plain body`,
		`{"nested":{"key":"value"}}`,
	}).Draw(t, label+"Body")
}

func genDifferentStatusCode(t *rapid.T, current int) int {
	for {
		code := rapid.SampledFrom([]int{200, 201, 204, 301, 400, 401, 403, 404, 500, 502, 503}).Draw(t, "newStatusCode")
		if code != current {
			return code
		}
	}
}

func genDifferentHeaders(t *rapid.T, current map[string][]string) map[string][]string {
	// Strategy: generate a completely new header map that is guaranteed to differ.
	// We add a unique header key that cannot exist in the original.
	newHeaders := make(map[string][]string)
	newHeaders["X-Diff-Marker"] = []string{rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "diffMarkerVal")}
	return newHeaders
}

func genDifferentBody(t *rapid.T, current string) string {
	for {
		body := rapid.SampledFrom([]string{
			`{"name":"Charlie","age":99}`,
			`{"name":"Diana","age":42}`,
			`{"items":[4,5,6]}`,
			`{"active":false}`,
			`different plain text`,
			`yet another body`,
			`{"nested":{"key":"changed"}}`,
			`{"completely":"different"}`,
		}).Draw(t, "newBody")
		if body != current {
			return body
		}
	}
}

func copyHeaders(h map[string][]string) map[string][]string {
	cp := make(map[string][]string, len(h))
	for k, v := range h {
		vals := make([]string, len(v))
		copy(vals, v)
		cp[k] = vals
	}
	return cp
}

func hasFieldWithPrefix(fields []FieldDiff, prefix string) bool {
	for _, f := range fields {
		if len(f.Path) >= len(prefix) && f.Path[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// When two JSON bodies differ, the diff should report specific field paths
// like "body.user.name" instead of just saying "body changed".
func TestDiffReportsFieldLevelPaths(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		engine := NewDiffEngine()

		// Generate two distinct nested JSON objects that share the same structure
		// but differ in at least one leaf value.
		baseObj := genNestedJSONObject(rt, "base", 0)
		mutatedObj := mutateJSONObject(rt, baseObj)

		storedBody := mustMarshal(baseObj)
		liveBody := mustMarshal(mutatedObj)

		// Sanity: the bodies must actually differ for the property to be meaningful
		if storedBody == liveBody {
			return // skip degenerate case where mutation produced identical JSON
		}

		stored := &snapshot.SnapshotEntry{
			Method:     "GET",
			URL:        "https://api.example.com/test",
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"application/json"}},
			Body:       storedBody,
		}
		live := &snapshot.SnapshotEntry{
			Method:     "GET",
			URL:        "https://api.example.com/test",
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"application/json"}},
			Body:       liveBody,
		}

		result := engine.Compare(stored, live)

		// Since bodies differ and everything else is identical, we must have diffs
		if result.Matched {
			rt.Fatalf("expected Matched=false for differing JSON bodies:\nstored=%s\nlive=%s", storedBody, liveBody)
		}

		if len(result.Fields) == 0 {
			rt.Fatalf("expected at least one FieldDiff for differing JSON bodies:\nstored=%s\nlive=%s", storedBody, liveBody)
		}

		// Property: every FieldDiff path must start with "body." and must be more
		// specific than just "body" - i.e., it must identify a field within the body.
		for _, fd := range result.Fields {
			if fd.Path == "body" {
				rt.Fatalf("FieldDiff path is 'body' (whole-body), expected field-level path like 'body.x':\nstored=%s\nlive=%s\nfields=%+v",
					storedBody, liveBody, result.Fields)
			}
			if !hasPrefix(fd.Path, "body.") && !hasPrefix(fd.Path, "body[") {
				rt.Fatalf("FieldDiff path %q does not start with 'body.' or 'body[', expected a body sub-path:\nstored=%s\nlive=%s",
					fd.Path, storedBody, liveBody)
			}
		}

		// Property: each reported path must correspond to an actual difference
		// between the two JSON trees. We verify by checking that Expected != Actual
		// for every FieldDiff (the engine should not report spurious diffs).
		for _, fd := range result.Fields {
			if fd.Expected == fd.Actual {
				rt.Fatalf("FieldDiff at path %q has Expected == Actual == %q, should only report actual differences",
					fd.Path, fd.Expected)
			}
		}
	})
}

// --- Generators for Property 10 ---

// genNestedJSONObject generates a random nested JSON-like map with string keys
// and values that are strings, numbers, booleans, nested objects, or arrays.
func genNestedJSONObject(t *rapid.T, label string, depth int) map[string]interface{} {
	maxDepth := 3
	numKeys := rapid.IntRange(1, 4).Draw(t, label+"NumKeys")
	obj := make(map[string]interface{}, numKeys)

	keyPool := []string{"name", "age", "active", "email", "score", "role", "city", "items", "meta", "data"}

	for i := 0; i < numKeys; i++ {
		key := rapid.SampledFrom(keyPool).Draw(t, fmt.Sprintf("%sKey%d", label, i))
		if depth < maxDepth {
			kind := rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("%sKind%d", label, i))
			switch kind {
			case 0: // string
				obj[key] = rapid.SampledFrom([]string{"Alice", "Bob", "Charlie", "Diana", "Eve"}).Draw(t, fmt.Sprintf("%sStr%d", label, i))
			case 1: // number
				obj[key] = float64(rapid.IntRange(0, 1000).Draw(t, fmt.Sprintf("%sNum%d", label, i)))
			case 2: // bool
				obj[key] = rapid.Bool().Draw(t, fmt.Sprintf("%sBool%d", label, i))
			case 3: // nested object
				obj[key] = genNestedJSONObject(t, fmt.Sprintf("%sNested%d", label, i), depth+1)
			case 4: // array of scalars
				arrLen := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("%sArrLen%d", label, i))
				arr := make([]interface{}, arrLen)
				for j := 0; j < arrLen; j++ {
					arr[j] = float64(rapid.IntRange(0, 100).Draw(t, fmt.Sprintf("%sArrElem%d_%d", label, i, j)))
				}
				obj[key] = arr
			}
		} else {
			// At max depth, only produce scalars
			obj[key] = rapid.SampledFrom([]string{"leaf1", "leaf2", "leaf3"}).Draw(t, fmt.Sprintf("%sLeaf%d", label, i))
		}
	}
	return obj
}

// mutateJSONObject takes a JSON object and changes at least one leaf value,
// preserving the overall structure so the diff engine can produce field-level paths.
func mutateJSONObject(t *rapid.T, original map[string]interface{}) map[string]interface{} {
	// Deep copy
	cp := deepCopyMap(original)

	// Collect all leaf paths
	paths := collectLeafPaths(cp, "")
	if len(paths) == 0 {
		// Degenerate: add a new key
		cp["_mutated"] = "yes"
		return cp
	}

	// Pick a random leaf to mutate
	idx := rapid.IntRange(0, len(paths)-1).Draw(t, "mutateIdx")
	path := paths[idx]
	setLeafValue(cp, path, "_CHANGED_")

	return cp
}

// collectLeafPaths returns dot-separated paths to all leaf (non-map, non-slice) values.
func collectLeafPaths(obj map[string]interface{}, prefix string) []string {
	var paths []string
	for k, v := range obj {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			paths = append(paths, collectLeafPaths(val, p)...)
		case []interface{}:
			for i, elem := range val {
				elemPath := fmt.Sprintf("%s[%d]", p, i)
				if sub, ok := elem.(map[string]interface{}); ok {
					paths = append(paths, collectLeafPaths(sub, elemPath)...)
				} else {
					paths = append(paths, elemPath)
				}
			}
		default:
			paths = append(paths, p)
		}
	}
	return paths
}

// setLeafValue navigates the map using a dot/bracket path and sets the leaf to newVal.
func setLeafValue(obj map[string]interface{}, path string, newVal interface{}) {
	parts := splitPath(path)
	current := interface{}(obj)
	for i, part := range parts {
		if i == len(parts)-1 {
			// Set the value
			switch c := current.(type) {
			case map[string]interface{}:
				c[part.key] = newVal
			case []interface{}:
				if part.index >= 0 && part.index < len(c) {
					c[part.index] = newVal
				}
			}
			return
		}
		// Navigate deeper
		switch c := current.(type) {
		case map[string]interface{}:
			current = c[part.key]
		case []interface{}:
			if part.index >= 0 && part.index < len(c) {
				current = c[part.index]
			} else {
				return
			}
		default:
			return
		}
	}
}

type pathPart struct {
	key   string
	index int // -1 if not an array index
}

// splitPath splits "a.b[0].c" into pathParts.
func splitPath(path string) []pathPart {
	var parts []pathPart
	i := 0
	for i < len(path) {
		if path[i] == '[' {
			// Parse array index
			j := i + 1
			for j < len(path) && path[j] != ']' {
				j++
			}
			idx := 0
			for _, ch := range path[i+1 : j] {
				idx = idx*10 + int(ch-'0')
			}
			parts = append(parts, pathPart{index: idx})
			i = j + 1
			if i < len(path) && path[i] == '.' {
				i++ // skip dot after ]
			}
		} else {
			// Parse key
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			parts = append(parts, pathPart{key: path[i:j], index: -1})
			i = j
			if i < len(path) && path[i] == '.' {
				i++ // skip dot
			}
		}
	}
	return parts
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			cp[k] = deepCopyMap(val)
		case []interface{}:
			arr := make([]interface{}, len(val))
			for i, elem := range val {
				if sub, ok := elem.(map[string]interface{}); ok {
					arr[i] = deepCopyMap(sub)
				} else {
					arr[i] = elem
				}
			}
			cp[k] = arr
		default:
			cp[k] = v
		}
	}
	return cp
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// ANSI color codes should only appear when color=true. When color=false
// (piped output, CI, etc.) the output should be plain text.
func TestColorOutputRespectsFlag(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		engine := NewDiffEngine()

		// Generate a non-matching DiffResult with at least one FieldDiff
		numFields := rapid.IntRange(1, 5).Draw(rt, "numFields")
		fields := make([]FieldDiff, numFields)
		for i := 0; i < numFields; i++ {
			fields[i] = FieldDiff{
				Path: rapid.SampledFrom([]string{
					"status_code",
					"headers.Content-Type",
					"body.name",
					"body.items[0]",
					"body.nested.key",
				}).Draw(rt, fmt.Sprintf("fieldPath%d", i)),
				Expected: rapid.SampledFrom([]string{
					"200", "application/json", "Alice", "1", "old_value",
				}).Draw(rt, fmt.Sprintf("fieldExpected%d", i)),
				Actual: rapid.SampledFrom([]string{
					"500", "text/plain", "Bob", "2", "new_value",
				}).Draw(rt, fmt.Sprintf("fieldActual%d", i)),
			}
		}

		result := DiffResult{
			RequestHash: "abc123",
			URL:         rapid.SampledFrom([]string{
				"https://api.example.com/users",
				"https://api.example.com/items/1",
				"https://api.example.com/health",
			}).Draw(rt, "url"),
			Matched: false,
			Fields:  fields,
		}

		colorEnabled := rapid.Bool().Draw(rt, "colorEnabled")
		output := engine.FormatDiff(result, colorEnabled)

		// ANSI escape sequences start with ESC[ (\x1b[)
		containsANSI := strings.Contains(output, "\033[")

		if colorEnabled {
			// When color is enabled, output MUST contain ANSI codes
			if !containsANSI {
				rt.Fatalf("color=true but output contains no ANSI escape sequences.\nOutput: %q", output)
			}
		} else {
			// When color is disabled, output MUST NOT contain any ANSI codes
			if containsANSI {
				rt.Fatalf("color=false but output contains ANSI escape sequences.\nOutput: %q", output)
			}
		}
	})
}
