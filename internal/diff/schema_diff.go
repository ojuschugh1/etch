package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// InferSchema extracts a type-only schema from a JSON body.
// Values are replaced with their types: "string", "number", "boolean",
// "null", "array", "object". Nested structures are preserved.
func InferSchema(body string) map[string]interface{} {
	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil
	}
	return inferType(parsed).(map[string]interface{})
}

func inferType(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, child := range val {
			result[k] = inferType(child)
		}
		return result
	case []interface{}:
		if len(val) == 0 {
			return "array:empty"
		}
		return map[string]interface{}{
			"_type":  "array",
			"_items": inferType(val[0]),
		}
	case string:
		return "string"
	case float64:
		// check if it's actually an integer
		if val == float64(int64(val)) {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// CompareSchemas compares two inferred schemas and returns field diffs
// for structural/type changes only. Value changes are completely ignored.
func CompareSchemas(path string, expected, actual interface{}) []FieldDiff {
	// both nil
	if expected == nil && actual == nil {
		return nil
	}

	// one nil
	if expected == nil {
		return []FieldDiff{{Path: path, Expected: "", Actual: schemaString(actual)}}
	}
	if actual == nil {
		return []FieldDiff{{Path: path, Expected: schemaString(expected), Actual: ""}}
	}

	// both are type strings (leaf nodes)
	eStr, eIsStr := expected.(string)
	aStr, aIsStr := actual.(string)
	if eIsStr && aIsStr {
		if eStr != aStr {
			return []FieldDiff{{
				Path:     path,
				Expected: eStr,
				Actual:   aStr,
			}}
		}
		return nil
	}

	// both are objects
	eMap, eIsMap := expected.(map[string]interface{})
	aMap, aIsMap := actual.(map[string]interface{})

	if eIsMap && aIsMap {
		// check if both are array descriptors
		if eMap["_type"] == "array" && aMap["_type"] == "array" {
			return CompareSchemas(path+"[]", eMap["_items"], aMap["_items"])
		}

		var diffs []FieldDiff
		allKeys := make(map[string]bool)
		for k := range eMap {
			allKeys[k] = true
		}
		for k := range aMap {
			allKeys[k] = true
		}

		sorted := make([]string, 0, len(allKeys))
		for k := range allKeys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)

		for _, key := range sorted {
			if strings.HasPrefix(key, "_") {
				continue // skip internal markers
			}
			childPath := path + "." + key
			eVal, eOK := eMap[key]
			aVal, aOK := aMap[key]

			if !eOK {
				diffs = append(diffs, FieldDiff{
					Path:     childPath,
					Expected: "",
					Actual:   schemaString(aVal),
				})
			} else if !aOK {
				diffs = append(diffs, FieldDiff{
					Path:     childPath,
					Expected: schemaString(eVal),
					Actual:   "",
				})
			} else {
				diffs = append(diffs, CompareSchemas(childPath, eVal, aVal)...)
			}
		}
		return diffs
	}

	// type mismatch (one is object, other is string type)
	if eIsStr != aIsStr || eIsMap != aIsMap {
		return []FieldDiff{{
			Path:     path,
			Expected: schemaString(expected),
			Actual:   schemaString(actual),
		}}
	}

	return nil
}

func schemaString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]interface{}:
		if val["_type"] == "array" {
			return "array<" + schemaString(val["_items"]) + ">"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			if !strings.HasPrefix(k, "_") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + schemaString(val[k])
		}
		return "object{" + strings.Join(parts, ",") + "}"
	default:
		return fmt.Sprintf("%v", v)
	}
}
