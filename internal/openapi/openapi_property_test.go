package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

// Generate an OpenAPI spec from random snapshots and make sure the output
// is structurally valid: has the right version, info block, paths, etc.
func TestOpenAPIOutputIsValid(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		store := snapshot.NewSnapshotStore(dir)

		// Generate 1-4 distinct hosts, each with 1-3 snapshot entries.
		numHosts := rapid.IntRange(1, 4).Draw(rt, "numHosts")
		hosts := make([]string, 0, numHosts)
		hostSet := make(map[string]bool)
		for len(hosts) < numHosts {
			h := rapid.StringMatching(`[a-z][a-z0-9]{0,8}\.[a-z]{2,4}`).Draw(rt, "host")
			if hostSet[h] {
				continue
			}
			hostSet[h] = true
			hosts = append(hosts, h)
		}

		for _, host := range hosts {
			numEntries := rapid.IntRange(1, 3).Draw(rt, "numEntries")
			for i := 0; i < numEntries; i++ {
				hash := rapid.StringMatching(`[a-f0-9]{16,64}`).Draw(rt, "hash")
				entry := genSnapshotEntry(rt, host)
				if err := store.Record(host, hash, entry); err != nil {
					rt.Fatalf("Record failed: %v", err)
				}
			}
		}

		gen := NewOpenAPIGenerator(store)

		// Validate Generate() returns a valid OpenAPI 3.0 structure.
		spec, err := gen.Generate()
		if err != nil {
			rt.Fatalf("Generate failed: %v", err)
		}

		validateOpenAPI30Spec(rt, spec)

		// Validate WriteYAML produces parseable YAML that round-trips to a valid spec.
		var buf bytes.Buffer
		if err := gen.WriteYAML(&buf); err != nil {
			rt.Fatalf("WriteYAML failed: %v", err)
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(buf.Bytes(), &parsed); err != nil {
			rt.Fatalf("WriteYAML output is not valid YAML: %v", err)
		}

		validateOpenAPI30Spec(rt, parsed)
	})
}

// validateOpenAPI30Spec does a structural check on the spec map.
func validateOpenAPI30Spec(rt *rapid.T, spec map[string]interface{}) {
	// 1. Must have "openapi" field starting with "3.0".
	openapiVal, ok := spec["openapi"]
	if !ok {
		rt.Fatal("spec missing required 'openapi' field")
	}
	openapiStr, ok := openapiVal.(string)
	if !ok {
		rt.Fatalf("'openapi' field is not a string: %T", openapiVal)
	}
	if !strings.HasPrefix(openapiStr, "3.0") {
		rt.Fatalf("'openapi' field does not start with '3.0': %q", openapiStr)
	}

	// 2. Must have "info" object with "title" and "version".
	infoVal, ok := spec["info"]
	if !ok {
		rt.Fatal("spec missing required 'info' field")
	}
	infoMap := toStringMap(infoVal)
	if infoMap == nil {
		rt.Fatalf("'info' field is not an object: %T", infoVal)
	}
	if _, ok := infoMap["title"]; !ok {
		rt.Fatal("info object missing required 'title' field")
	}
	if _, ok := infoMap["version"]; !ok {
		rt.Fatal("info object missing required 'version' field")
	}

	// 3. Must have "paths" object.
	pathsVal, ok := spec["paths"]
	if !ok {
		rt.Fatal("spec missing required 'paths' field")
	}
	pathsMap := toStringMap(pathsVal)
	if pathsMap == nil {
		rt.Fatalf("'paths' field is not an object: %T", pathsVal)
	}

	// 4. Each path must start with "/" and contain valid HTTP method operations.
	validMethods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true,
		"patch": true, "head": true, "options": true, "trace": true,
	}
	for path, pathItemVal := range pathsMap {
		if !strings.HasPrefix(path, "/") {
			rt.Fatalf("path %q does not start with '/'", path)
		}
		pathItem := toStringMap(pathItemVal)
		if pathItem == nil {
			rt.Fatalf("path item for %q is not an object: %T", path, pathItemVal)
		}
		for method, opVal := range pathItem {
			if !validMethods[method] {
				// Could be "parameters", "summary", etc. - skip non-method keys.
				continue
			}
			opMap := toStringMap(opVal)
			if opMap == nil {
				rt.Fatalf("operation %s %s is not an object: %T", strings.ToUpper(method), path, opVal)
			}
			// If responses exist, validate structure.
			if respVal, ok := opMap["responses"]; ok {
				respMap := toStringMap(respVal)
				if respMap == nil {
					rt.Fatalf("responses for %s %s is not an object", strings.ToUpper(method), path)
				}
				for code, respObjVal := range respMap {
					// Status codes should be numeric strings or "default".
					if code != "default" && !isNumericString(code) {
						rt.Fatalf("invalid response status code %q for %s %s", code, strings.ToUpper(method), path)
					}
					respObj := toStringMap(respObjVal)
					if respObj == nil {
						rt.Fatalf("response %s for %s %s is not an object", code, strings.ToUpper(method), path)
					}
					if _, ok := respObj["description"]; !ok {
						rt.Fatalf("response %s for %s %s missing 'description'", code, strings.ToUpper(method), path)
					}
				}
			}
		}
	}
}

// toStringMap converts an interface{} to map[string]interface{}, handling
// both native Go maps and YAML-decoded maps.
func toStringMap(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(m))
		for k, val := range m {
			result[fmt.Sprintf("%v", k)] = val
		}
		return result
	default:
		return nil
	}
}

// isNumericString returns true if s consists entirely of digits.
func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// genSnapshotEntry generates a random SnapshotEntry for the given host.
func genSnapshotEntry(t *rapid.T, host string) snapshot.SnapshotEntry {
	method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH"}).Draw(t, "method")
	path := rapid.StringMatching(`(/[a-z]{1,8}){1,3}`).Draw(t, "path")
	scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(t, "scheme")
	url := fmt.Sprintf("%s://%s%s", scheme, host, path)
	statusCode := rapid.SampledFrom([]int{200, 201, 204, 400, 404, 500}).Draw(t, "statusCode")

	// Generate a valid JSON body or empty string.
	bodyType := rapid.IntRange(0, 2).Draw(t, "bodyType")
	var body string
	switch bodyType {
	case 0:
		body = ""
	case 1:
		body = fmt.Sprintf(`{"id":%d,"name":"%s"}`,
			rapid.IntRange(1, 9999).Draw(t, "id"),
			rapid.StringMatching(`[a-zA-Z]{2,10}`).Draw(t, "name"))
	case 2:
		body = fmt.Sprintf(`[{"id":%d}]`, rapid.IntRange(1, 9999).Draw(t, "arrId"))
	}

	headers := make(map[string][]string)
	if body != "" {
		headers["Content-Type"] = []string{"application/json"}
	}

	return snapshot.SnapshotEntry{
		Method:     method,
		URL:        url,
		StatusCode: statusCode,
		Headers:    headers,
		Body:       body,
	}
}

// /users/123 and /users/456 should collapse into /users/{id}.
// We generate random URL sets with one varying segment and check that
// InferPathParams parameterizes the right position.
func TestPathParameterInference(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		gen := &OpenAPIGenerator{}

		// Generate a base path with 1-4 segments of lowercase alpha strings.
		numSegments := rapid.IntRange(1, 4).Draw(rt, "numSegments")
		baseSegments := make([]string, numSegments)
		for i := 0; i < numSegments; i++ {
			baseSegments[i] = rapid.StringMatching(`[a-z]{2,8}`).Draw(rt, fmt.Sprintf("seg%d", i))
		}

		// Pick one segment index to vary across URLs.
		varyIdx := rapid.IntRange(0, numSegments-1).Draw(rt, "varyIdx")

		// Generate 2-5 distinct values for the varying segment.
		numVariants := rapid.IntRange(2, 5).Draw(rt, "numVariants")
		variantSet := make(map[string]bool)
		var variants []string
		for len(variants) < numVariants {
			v := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(rt, "variant")
			// Ensure variant differs from the base segment and is unique.
			if variantSet[v] || v == baseSegments[varyIdx] {
				continue
			}
			variantSet[v] = true
			variants = append(variants, v)
		}

		// Build the URL list by substituting the varying segment.
		urls := make([]string, len(variants))
		for i, v := range variants {
			segs := make([]string, numSegments)
			copy(segs, baseSegments)
			segs[varyIdx] = v
			urls[i] = "/" + strings.Join(segs, "/")
		}

		// Run InferPathParams.
		templates := gen.InferPathParams(urls)

		// All templates must have the same Template string.
		if len(templates) != len(urls) {
			rt.Fatalf("expected %d templates, got %d", len(urls), len(templates))
		}

		firstTemplate := templates[0].Template
		for i, tmpl := range templates {
			if tmpl.Template != firstTemplate {
				rt.Fatalf("template[%d] = %q, expected %q (all should match)", i, tmpl.Template, firstTemplate)
			}
		}

		// The template must contain at least one path parameter placeholder.
		if !strings.Contains(firstTemplate, "{") || !strings.Contains(firstTemplate, "}") {
			rt.Fatalf("template %q does not contain a path parameter placeholder; urls=%v", firstTemplate, urls)
		}

		// The varying segment position must be parameterized.
		templateSegs := splitPath(firstTemplate)
		if len(templateSegs) != numSegments {
			rt.Fatalf("template segment count %d != expected %d", len(templateSegs), numSegments)
		}
		varySeg := templateSegs[varyIdx]
		if !strings.HasPrefix(varySeg, "{") || !strings.HasSuffix(varySeg, "}") {
			rt.Fatalf("segment at vary index %d is %q, expected a parameter placeholder", varyIdx, varySeg)
		}

		// Non-varying segments must remain literal (unchanged from base).
		for i, seg := range templateSegs {
			if i == varyIdx {
				continue
			}
			if seg != baseSegments[i] {
				rt.Fatalf("segment[%d] = %q, expected literal %q", i, seg, baseSegments[i])
			}
		}

		// Each template must list at least one param.
		for i, tmpl := range templates {
			if len(tmpl.Params) == 0 {
				rt.Fatalf("template[%d] has no Params, expected at least 1", i)
			}
		}
	})
}

// The generated schema should match the structure of the recorded body.
// We generate random JSON, infer a schema, and walk both to make sure
// types and properties line up.
func TestSchemaMatchesRecordedBody(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random JSON value and marshal it to a string.
		jsonVal := genJSONValue(rt, 0)
		bodyBytes, err := json.Marshal(jsonVal)
		if err != nil {
			rt.Fatalf("json.Marshal failed: %v", err)
		}
		body := string(bodyBytes)

		// Infer the schema from the body using the generator's function.
		schema := inferJSONSchema(body)
		if schema == nil {
			rt.Fatalf("inferJSONSchema returned nil for valid JSON body: %s", body)
		}

		// Validate that the schema is consistent with the original value.
		validateSchemaMatchesValue(rt, schema, jsonVal, "$")
	})
}

// genJSONValue generates a random JSON-compatible value (object, array, string,
// number, bool, or null) with bounded depth.
func genJSONValue(rt *rapid.T, depth int) interface{} {
	if depth >= 3 {
		// At max depth, only generate leaf values.
		return genJSONLeaf(rt)
	}

	kind := rapid.IntRange(0, 4).Draw(rt, "jsonKind")
	switch kind {
	case 0: // object
		numKeys := rapid.IntRange(0, 4).Draw(rt, "numKeys")
		obj := make(map[string]interface{}, numKeys)
		for i := 0; i < numKeys; i++ {
			key := rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, fmt.Sprintf("key%d", i))
			obj[key] = genJSONValue(rt, depth+1)
		}
		return obj
	case 1: // array
		numItems := rapid.IntRange(0, 3).Draw(rt, "numItems")
		arr := make([]interface{}, numItems)
		for i := 0; i < numItems; i++ {
			arr[i] = genJSONValue(rt, depth+1)
		}
		return arr
	default:
		return genJSONLeaf(rt)
	}
}

// genJSONLeaf generates a random JSON leaf value (string, number, bool, or null).
func genJSONLeaf(rt *rapid.T) interface{} {
	leafKind := rapid.IntRange(0, 3).Draw(rt, "leafKind")
	switch leafKind {
	case 0:
		return rapid.StringMatching(`[a-zA-Z0-9 ]{0,20}`).Draw(rt, "strVal")
	case 1:
		return rapid.Float64Range(-1e6, 1e6).Draw(rt, "numVal")
	case 2:
		return rapid.Bool().Draw(rt, "boolVal")
	default:
		return nil
	}
}

// validateSchemaMatchesValue recursively checks that a JSON Schema map is
// consistent with the given JSON value.
func validateSchemaMatchesValue(rt *rapid.T, schema map[string]interface{}, value interface{}, path string) {
	schemaType, _ := schema["type"].(string)

	switch val := value.(type) {
	case map[string]interface{}:
		if schemaType != "object" {
			rt.Fatalf("at %s: expected schema type 'object' for map value, got %q", path, schemaType)
		}
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil && len(val) > 0 {
			rt.Fatalf("at %s: schema has no 'properties' but value has %d keys", path, len(val))
		}
		for key, childVal := range val {
			childSchema, ok := props[key]
			if !ok {
				rt.Fatalf("at %s: schema missing property %q", path, key)
			}
			childSchemaMap, ok := childSchema.(map[string]interface{})
			if !ok {
				rt.Fatalf("at %s.%s: property schema is not a map", path, key)
			}
			validateSchemaMatchesValue(rt, childSchemaMap, childVal, path+"."+key)
		}

	case []interface{}:
		if schemaType != "array" {
			rt.Fatalf("at %s: expected schema type 'array' for array value, got %q", path, schemaType)
		}
		if len(val) > 0 {
			items, ok := schema["items"].(map[string]interface{})
			if !ok {
				rt.Fatalf("at %s: array schema missing 'items' but value has %d elements", path, len(val))
			}
			// Validate the items schema against the first element (matches generator behavior).
			validateSchemaMatchesValue(rt, items, val[0], path+"[0]")
		}

	case float64:
		if schemaType != "number" {
			rt.Fatalf("at %s: expected schema type 'number' for float64 value, got %q", path, schemaType)
		}

	case bool:
		if schemaType != "boolean" {
			rt.Fatalf("at %s: expected schema type 'boolean' for bool value, got %q", path, schemaType)
		}

	case string:
		if schemaType != "string" {
			rt.Fatalf("at %s: expected schema type 'string' for string value, got %q", path, schemaType)
		}

	case nil:
		// Null values are mapped to {"type": "string", "nullable": true}.
		if schemaType != "string" {
			rt.Fatalf("at %s: expected schema type 'string' for null value, got %q", path, schemaType)
		}
		nullable, _ := schema["nullable"].(bool)
		if !nullable {
			rt.Fatalf("at %s: expected 'nullable: true' for null value", path)
		}

	default:
		rt.Fatalf("at %s: unexpected value type %T", path, value)
	}
}
