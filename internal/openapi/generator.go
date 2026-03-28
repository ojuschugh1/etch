package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/snapshot"
	"gopkg.in/yaml.v3"
)

// OpenAPIGenerator builds an OpenAPI 3.0 spec from recorded snapshots.
type OpenAPIGenerator struct {
	Store *snapshot.SnapshotStore
}

// PathTemplate represents a parameterized URL path.
type PathTemplate struct {
	Template string   // e.g., "/users/{id}"
	Params   []string // e.g., ["id"]
}

// NewOpenAPIGenerator creates an OpenAPIGenerator backed by the given store.
func NewOpenAPIGenerator(store *snapshot.SnapshotStore) *OpenAPIGenerator {
	return &OpenAPIGenerator{Store: store}
}

// Generate reads all snapshots and produces an OpenAPI 3.0 spec as a nested map.
func (g *OpenAPIGenerator) Generate() (map[string]interface{}, error) {
	allSnaps, err := g.Store.LoadAll()
	if err != nil {
		return nil, err
	}

	// Collect all entries grouped by method + path.
	type endpointKey struct {
		method string
		path   string
	}

	grouped := make(map[endpointKey][]snapshot.SnapshotEntry)

	for _, sf := range allSnaps {
		for _, entry := range sf {
			parsed, err := url.Parse(entry.URL)
			if err != nil {
				continue
			}
			key := endpointKey{
				method: strings.ToUpper(entry.Method),
				path:   parsed.Path,
			}
			grouped[key] = append(grouped[key], entry)
		}
	}

	// Group paths by method + segment count for path param inference.
	type segGroupKey struct {
		method   string
		segCount int
	}
	segGroups := make(map[segGroupKey][]endpointKey)
	for key := range grouped {
		segs := splitPath(key.path)
		sgk := segGroupKey{method: key.method, segCount: len(segs)}
		segGroups[sgk] = append(segGroups[sgk], key)
	}

	// For each segment group, infer path templates and merge entries.
	type templateKey struct {
		method   string
		template string
	}
	templateEntries := make(map[templateKey][]snapshot.SnapshotEntry)

	for _, keys := range segGroups {
		// Sort keys for deterministic output.
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].path < keys[j].path
		})

		var allPaths []string
		for _, k := range keys {
			allPaths = append(allPaths, k.path)
		}

		templates := g.InferPathParams(allPaths)

		for i, k := range keys {
			tmpl := templates[i]
			tKey := templateKey{method: k.method, template: tmpl.Template}
			templateEntries[tKey] = append(templateEntries[tKey], grouped[k]...)
		}
	}

	// Build paths object.
	paths := make(map[string]interface{})
	for tKey, entries := range templateEntries {
		pathItem, ok := paths[tKey.template].(map[string]interface{})
		if !ok {
			pathItem = make(map[string]interface{})
			paths[tKey.template] = pathItem
		}
		pathItem[strings.ToLower(tKey.method)] = buildOperation(entries, tKey.template)
	}

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   "Etch Generated API",
			"version": "1.0.0",
		},
		"paths": paths,
	}

	return spec, nil
}

// WriteYAML generates the OpenAPI spec and writes it as YAML to w.
func (g *OpenAPIGenerator) WriteYAML(w io.Writer) error {
	spec, err := g.Generate()
	if err != nil {
		return err
	}

	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	if err := encoder.Encode(spec); err != nil {
		return err
	}
	return encoder.Close()
}

// InferPathParams takes a list of URL paths with the same segment count and
// detects segments that vary across the set, replacing them with parameter
// placeholders.
func (g *OpenAPIGenerator) InferPathParams(urls []string) []PathTemplate {
	if len(urls) == 0 {
		return nil
	}
	if len(urls) == 1 {
		return []PathTemplate{{Template: urls[0]}}
	}

	// Split all paths into segments.
	allSegments := make([][]string, len(urls))
	segCount := -1
	for i, u := range urls {
		segs := splitPath(u)
		if segCount == -1 {
			segCount = len(segs)
		}
		if len(segs) != segCount {
			// Different segment counts - return each path as-is.
			result := make([]PathTemplate, len(urls))
			for j, u2 := range urls {
				result[j] = PathTemplate{Template: u2}
			}
			return result
		}
		allSegments[i] = segs
	}

	if segCount == 0 {
		result := make([]PathTemplate, len(urls))
		for i := range urls {
			result[i] = PathTemplate{Template: "/"}
		}
		return result
	}

	// Determine which segments vary.
	varying := make([]bool, segCount)
	for s := 0; s < segCount; s++ {
		first := allSegments[0][s]
		for i := 1; i < len(allSegments); i++ {
			if allSegments[i][s] != first {
				varying[s] = true
				break
			}
		}
	}

	// Build the template.
	paramIdx := 0
	templateSegs := make([]string, segCount)
	var params []string
	for s := 0; s < segCount; s++ {
		if varying[s] {
			paramName := inferParamName(allSegments[0], s, paramIdx)
			templateSegs[s] = "{" + paramName + "}"
			params = append(params, paramName)
			paramIdx++
		} else {
			templateSegs[s] = allSegments[0][s]
		}
	}

	template := "/" + strings.Join(templateSegs, "/")

	result := make([]PathTemplate, len(urls))
	for i := range urls {
		result[i] = PathTemplate{
			Template: template,
			Params:   params,
		}
	}
	return result
}

// splitPath splits a URL path into non-empty segments.
func splitPath(p string) []string {
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// inferParamName generates a parameter name from context. If the preceding
// segment looks like a plural resource name (e.g., "users"), it derives a
// singular form + "Id". Otherwise falls back to a generic name.
func inferParamName(segments []string, idx int, paramIdx int) string {
	if idx > 0 {
		prev := segments[idx-1]
		if strings.HasSuffix(prev, "s") && len(prev) > 1 {
			return strings.TrimSuffix(prev, "s") + "Id"
		}
		return prev + "Id"
	}
	return "id"
}

// buildOperation creates an OpenAPI operation object from snapshot entries.
func buildOperation(entries []snapshot.SnapshotEntry, template string) map[string]interface{} {
	operation := map[string]interface{}{}

	// Extract path parameters from template.
	var parameters []interface{}
	for _, seg := range splitPath(template) {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			paramName := seg[1 : len(seg)-1]
			parameters = append(parameters, map[string]interface{}{
				"name":     paramName,
				"in":       "path",
				"required": true,
				"schema":   map[string]interface{}{"type": "string"},
			})
		}
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}

	// Group entries by status code.
	responsesByStatus := make(map[int][]snapshot.SnapshotEntry)
	for _, e := range entries {
		responsesByStatus[e.StatusCode] = append(responsesByStatus[e.StatusCode], e)
	}

	responses := make(map[string]interface{})
	var statusCodes []int
	for code := range responsesByStatus {
		statusCodes = append(statusCodes, code)
	}
	sort.Ints(statusCodes)

	for _, code := range statusCodes {
		statusEntries := responsesByStatus[code]
		resp := map[string]interface{}{
			"description": "Recorded response",
		}

		// Infer schema from the first entry with a parseable JSON body.
		for _, e := range statusEntries {
			if e.Body == "" {
				continue
			}
			schema := inferJSONSchema(e.Body)
			if schema != nil {
				resp["content"] = map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": schema,
					},
				}
				break
			}
		}

		responses[fmt.Sprintf("%d", code)] = resp
	}

	if len(responses) > 0 {
		operation["responses"] = responses
	}

	return operation
}

// inferJSONSchema parses a JSON string and returns a basic JSON Schema map.
// Returns nil if the body is not valid JSON.
func inferJSONSchema(body string) map[string]interface{} {
	var raw interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil
	}
	return schemaFromValue(raw)
}

// schemaFromValue recursively builds a JSON Schema from a parsed JSON value.
func schemaFromValue(v interface{}) map[string]interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		properties := make(map[string]interface{})
		var keys []string
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			properties[k] = schemaFromValue(val[k])
		}
		return map[string]interface{}{
			"type":       "object",
			"properties": properties,
		}
	case []interface{}:
		schema := map[string]interface{}{
			"type": "array",
		}
		if len(val) > 0 {
			schema["items"] = schemaFromValue(val[0])
		}
		return schema
	case float64:
		return map[string]interface{}{"type": "number"}
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case string:
		return map[string]interface{}{"type": "string"}
	case nil:
		return map[string]interface{}{"type": "string", "nullable": true}
	default:
		return map[string]interface{}{"type": "string"}
	}
}
