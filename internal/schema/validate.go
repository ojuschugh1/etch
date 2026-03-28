package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Violation represents a single schema validation failure.
type Violation struct {
	Path     string
	Message  string
	Severity string // "error" or "warning"
}

// Result holds all violations from validating a response against a spec.
type Result struct {
	Endpoint   string
	StatusCode int
	Violations []Violation
}

// Spec holds a parsed OpenAPI 3.0 specification.
type Spec struct {
	raw map[string]interface{}
}

// LoadSpec reads an OpenAPI spec from a YAML or JSON file.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	var raw map[string]interface{}

	// try YAML first, then JSON
	if err := yaml.Unmarshal(data, &raw); err != nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing spec (tried YAML and JSON): %w", err)
		}
	}

	return &Spec{raw: raw}, nil
}

// resolveRef follows a local $ref like "#/components/schemas/User". Returns nil if unresolvable.
func (s *Spec) resolveRef(ref string) map[string]interface{} {
	if !strings.HasPrefix(ref, "#/") {
		return nil // only local refs supported
	}
	parts := strings.Split(ref[2:], "/")
	var current interface{} = s.raw
	for _, part := range parts {
		m, ok := toMap(current)
		if !ok {
			return nil
		}
		current = m[part]
	}
	if m, ok := toMap(current); ok {
		return m
	}
	return nil
}

// resolveSchema resolves $ref if present, with up to 10 levels to prevent loops.
func (s *Spec) resolveSchema(schema map[string]interface{}) map[string]interface{} {
	for i := 0; i < 10; i++ {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}
		resolved := s.resolveRef(ref)
		if resolved == nil {
			return schema
		}
		schema = resolved
	}
	return schema
}

// ValidateResponse checks a response body against the schema defined for
// the given method, path, and status code in the spec.
func (s *Spec) ValidateResponse(method, path string, statusCode int, body string) *Result {
	result := &Result{
		Endpoint:   method + " " + path,
		StatusCode: statusCode,
	}

	// find the path in the spec
	paths, ok := toMap(s.raw["paths"])
	if !ok {
		return result
	}

	pathItem, ok := toMap(paths[path])
	if !ok {
		// try to match parameterized paths
		pathItem, path, ok = s.matchParameterizedPath(paths, path)
		if !ok {
			result.Violations = append(result.Violations, Violation{
				Path:     path,
				Message:  "endpoint not defined in spec",
				Severity: "warning",
			})
			return result
		}
	}

	operation, ok := toMap(pathItem[strings.ToLower(method)])
	if !ok {
		result.Violations = append(result.Violations, Violation{
			Path:     method + " " + path,
			Message:  "method not defined in spec",
			Severity: "warning",
		})
		return result
	}

	responses, ok := toMap(operation["responses"])
	if !ok {
		return result
	}

	statusStr := fmt.Sprintf("%d", statusCode)
	respDef, ok := toMap(responses[statusStr])
	if !ok {
		// try "default"
		respDef, ok = toMap(responses["default"])
		if !ok {
			result.Violations = append(result.Violations, Violation{
				Path:     statusStr,
				Message:  fmt.Sprintf("status code %d not defined in spec", statusCode),
				Severity: "warning",
			})
			return result
		}
	}

	// get the schema from content -> application/json -> schema
	content, ok := toMap(respDef["content"])
	if !ok {
		return result
	}
	jsonContent, ok := toMap(content["application/json"])
	if !ok {
		return result
	}
	schemaDef, ok := toMap(jsonContent["schema"])
	if !ok {
		return result
	}

	// resolve $ref at the top level
	schemaDef = s.resolveSchema(schemaDef)

	// parse the response body and validate against schema
	if body == "" {
		return result
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		result.Violations = append(result.Violations, Violation{
			Path:     "body",
			Message:  "response body is not valid JSON",
			Severity: "error",
		})
		return result
	}

	violations := s.validateValue("body", parsed, schemaDef)
	result.Violations = violations
	return result
}

func (s *Spec) validateValue(path string, value interface{}, schema map[string]interface{}) []Violation {
	// Resolve $ref if present
	schema = s.resolveSchema(schema)

	// Handle allOf: value must match ALL schemas
	if allOf, ok := schema["allOf"]; ok {
		return s.validateAllOf(path, value, allOf)
	}

	// Handle oneOf: value must match exactly ONE schema
	if oneOf, ok := schema["oneOf"]; ok {
		return s.validateOneOf(path, value, oneOf)
	}

	// Handle anyOf: value must match at least ONE schema
	if anyOf, ok := schema["anyOf"]; ok {
		return s.validateAnyOf(path, value, anyOf)
	}

	expectedType, _ := schema["type"].(string)
	var violations []Violation

	switch expectedType {
	case "object":
		obj, ok := value.(map[string]interface{})
		if !ok {
			return []Violation{{
				Path:     path,
				Message:  fmt.Sprintf("expected object, got %s", typeName(value)),
				Severity: "error",
			}}
		}

		props, _ := toMap(schema["properties"])

		// Check required fields
		requiredFields := toStringSlice(schema["required"])
		for _, reqField := range requiredFields {
			if _, exists := obj[reqField]; !exists {
				violations = append(violations, Violation{
					Path:     path + "." + reqField,
					Message:  "required field missing from response",
					Severity: "error",
				})
			}
		}

		if props != nil {
			// check for missing fields defined in properties (non-required are warnings)
			requiredSet := make(map[string]bool, len(requiredFields))
			for _, r := range requiredFields {
				requiredSet[r] = true
			}

			for key, propSchema := range props {
				if _, exists := obj[key]; !exists {
					if !requiredSet[key] {
						// Already reported required fields above as errors
						violations = append(violations, Violation{
							Path:     path + "." + key,
							Message:  "field missing from response",
							Severity: "error",
						})
					}
				} else {
					propMap, _ := toMap(propSchema)
					if propMap != nil {
						violations = append(violations, s.validateValue(path+"."+key, obj[key], propMap)...)
					}
				}
			}

			// check for undocumented fields
			for key := range obj {
				if _, defined := props[key]; !defined {
					violations = append(violations, Violation{
						Path:     path + "." + key,
						Message:  "field not defined in spec (undocumented)",
						Severity: "warning",
					})
				}
			}
		}

	case "array":
		arr, ok := value.([]interface{})
		if !ok {
			return []Violation{{
				Path:     path,
				Message:  fmt.Sprintf("expected array, got %s", typeName(value)),
				Severity: "error",
			}}
		}
		items, _ := toMap(schema["items"])
		if items != nil {
			// validate ALL array elements, not just the first
			for i, elem := range arr {
				violations = append(violations, s.validateValue(fmt.Sprintf("%s[%d]", path, i), elem, items)...)
			}
		}

	case "string":
		if _, ok := value.(string); !ok && value != nil {
			violations = append(violations, Violation{
				Path:     path,
				Message:  fmt.Sprintf("expected string, got %s", typeName(value)),
				Severity: "error",
			})
		}

	case "integer":
		if num, ok := value.(float64); ok {
			if num != float64(int64(num)) {
				violations = append(violations, Violation{
					Path:     path,
					Message:  fmt.Sprintf("expected integer, got float (%v)", num),
					Severity: "error",
				})
			}
		} else if value != nil {
			violations = append(violations, Violation{
				Path:     path,
				Message:  fmt.Sprintf("expected integer, got %s", typeName(value)),
				Severity: "error",
			})
		}

	case "number":
		if _, ok := value.(float64); !ok && value != nil {
			violations = append(violations, Violation{
				Path:     path,
				Message:  fmt.Sprintf("expected number, got %s", typeName(value)),
				Severity: "error",
			})
		}

	case "boolean":
		if _, ok := value.(bool); !ok && value != nil {
			violations = append(violations, Violation{
				Path:     path,
				Message:  fmt.Sprintf("expected boolean, got %s", typeName(value)),
				Severity: "error",
			})
		}
	}

	return violations
}

func (s *Spec) validateAllOf(path string, value interface{}, allOf interface{}) []Violation {
	schemas, ok := allOf.([]interface{})
	if !ok {
		return nil
	}
	var violations []Violation
	for _, sub := range schemas {
		subMap, ok := toMap(sub)
		if !ok {
			continue
		}
		violations = append(violations, s.validateValue(path, value, subMap)...)
	}
	return violations
}

func (s *Spec) validateOneOf(path string, value interface{}, oneOf interface{}) []Violation {
	schemas, ok := oneOf.([]interface{})
	if !ok {
		return nil
	}
	matchCount := 0
	for _, sub := range schemas {
		subMap, ok := toMap(sub)
		if !ok {
			continue
		}
		v := s.validateValue(path, value, subMap)
		if len(v) == 0 {
			matchCount++
		}
	}
	if matchCount == 0 {
		return []Violation{{
			Path:     path,
			Message:  "value does not match any oneOf schema",
			Severity: "error",
		}}
	}
	if matchCount > 1 {
		return []Violation{{
			Path:     path,
			Message:  fmt.Sprintf("value matches %d oneOf schemas (should match exactly 1)", matchCount),
			Severity: "warning",
		}}
	}
	return nil
}

func (s *Spec) validateAnyOf(path string, value interface{}, anyOf interface{}) []Violation {
	schemas, ok := anyOf.([]interface{})
	if !ok {
		return nil
	}
	for _, sub := range schemas {
		subMap, ok := toMap(sub)
		if !ok {
			continue
		}
		v := s.validateValue(path, value, subMap)
		if len(v) == 0 {
			return nil // matched at least one
		}
	}
	return []Violation{{
		Path:     path,
		Message:  "value does not match any anyOf schema",
		Severity: "error",
	}}
}

// toStringSlice converts []interface{} to []string (from JSON/YAML unmarshaling).
func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func (s *Spec) matchParameterizedPath(paths map[string]interface{}, actualPath string) (map[string]interface{}, string, bool) {
	actualSegs := strings.Split(strings.Trim(actualPath, "/"), "/")

	for specPath, pathItem := range paths {
		specSegs := strings.Split(strings.Trim(specPath, "/"), "/")
		if len(specSegs) != len(actualSegs) {
			continue
		}

		match := true
		for i, seg := range specSegs {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				continue // parameter segment, matches anything
			}
			if seg != actualSegs[i] {
				match = false
				break
			}
		}

		if match {
			if m, ok := toMap(pathItem); ok {
				return m, specPath, true
			}
		}
	}

	return nil, actualPath, false
}

func toMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(m))
		for k, val := range m {
			result[fmt.Sprintf("%v", k)] = val
		}
		return result, true
	}
	return nil, false
}

func typeName(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// Format produces a readable validation report.
func (r *Result) Format(color bool) string {
	if len(r.Violations) == 0 {
		return ""
	}

	red, yellow, reset := "", "", ""
	if color {
		red = "\033[31m"
		yellow = "\033[33m"
		reset = "\033[0m"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s (status %d)\n", r.Endpoint, r.StatusCode))

	// sort violations by severity (errors first)
	sort.Slice(r.Violations, func(i, j int) bool {
		if r.Violations[i].Severity != r.Violations[j].Severity {
			return r.Violations[i].Severity == "error"
		}
		return r.Violations[i].Path < r.Violations[j].Path
	})

	for _, v := range r.Violations {
		sym := yellow + "!" + reset
		if v.Severity == "error" {
			sym = red + "✗" + reset
		}
		b.WriteString(fmt.Sprintf("    %s %s: %s\n", sym, v.Path, v.Message))
	}

	return b.String()
}

// HasErrors returns true if any violation is an error (not just warning).
func (r *Result) HasErrors() bool {
	for _, v := range r.Violations {
		if v.Severity == "error" {
			return true
		}
	}
	return false
}
