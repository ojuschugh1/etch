package hash

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func TestComputeHash_KnownValue(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "application/json")

	hash, err := hc.ComputeHash("GET", "https://api.example.com/users?limit=10&offset=0", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Canonical form:
	// GET\nhttps://api.example.com/users?limit=10&offset=0\nAccept:application/json\nContent-Type:application/json
	expected := sha256Hex("GET\nhttps://api.example.com/users?limit=10&offset=0\nAccept:application/json\nContent-Type:application/json")
	if hash != expected {
		t.Errorf("hash mismatch:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_MethodNormalizedToUppercase(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	hash1, err := hc.ComputeHash("get", "https://example.com/path", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := hc.ComputeHash("GET", "https://example.com/path", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("lowercase method produced different hash: %s vs %s", hash1, hash2)
	}
}

func TestComputeHash_EmptyHeaders(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	hash, err := hc.ComputeHash("POST", "https://api.example.com/data", http.Header{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no headers, canonical form is just METHOD\nURL
	expected := sha256Hex("POST\nhttps://api.example.com/data")
	if hash != expected {
		t.Errorf("hash mismatch for empty headers:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_NilHeaders(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	hash, err := hc.ComputeHash("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := sha256Hex("GET\nhttps://example.com/")
	if hash != expected {
		t.Errorf("hash mismatch for nil headers:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_EmptyQueryParams(t *testing.T) {
	hc := NewHashComputer(nil, nil)

	headers := http.Header{}
	headers.Set("X-Custom", "value")

	hash, err := hc.ComputeHash("GET", "https://api.example.com/items", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := sha256Hex("GET\nhttps://api.example.com/items\nX-Custom:value")
	if hash != expected {
		t.Errorf("hash mismatch for no query params:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_QueryParamsSorted(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	hash1, err := hc.ComputeHash("GET", "https://example.com/search?z=1&a=2", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := hc.ComputeHash("GET", "https://example.com/search?a=2&z=1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("different query param order produced different hashes: %s vs %s", hash1, hash2)
	}

	// Verify the actual canonical form uses sorted params
	expected := sha256Hex("GET\nhttps://example.com/search?a=2&z=1")
	if hash1 != expected {
		t.Errorf("hash doesn't match expected canonical form:\n  got:  %s\n  want: %s", hash1, expected)
	}
}

func TestComputeHash_UnicodeURL(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	hash, err := hc.ComputeHash("GET", "https://example.com/café/日本語", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash for unicode URL")
	}

	// Verify determinism with unicode
	hash2, err := hc.ComputeHash("GET", "https://example.com/café/日本語", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != hash2 {
		t.Errorf("unicode URL hash not deterministic: %s vs %s", hash, hash2)
	}
}

func TestComputeHash_ExcludedHeadersIgnored(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	base := http.Header{}
	base.Set("Accept", "text/html")

	withExcluded := base.Clone()
	withExcluded.Set("Date", "Mon, 01 Jan 2024 00:00:00 GMT")
	withExcluded.Set("Authorization", "Bearer token123")
	withExcluded.Set("X-Request-Id", "abc-123")

	hash1, err := hc.ComputeHash("GET", "https://example.com/page", base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := hc.ComputeHash("GET", "https://example.com/page", withExcluded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("excluded headers affected hash: %s vs %s", hash1, hash2)
	}
}

func TestComputeHash_IncludedHeadersMode(t *testing.T) {
	// When IncludedHeaders is set, only those headers are used
	hc := NewHashComputer(nil, []string{"X-Api-Key"})

	headers := http.Header{}
	headers.Set("X-Api-Key", "secret")
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "text/plain")

	hash, err := hc.ComputeHash("POST", "https://example.com/api", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only X-Api-Key should be in the canonical form
	expected := sha256Hex("POST\nhttps://example.com/api\nX-Api-Key:secret")
	if hash != expected {
		t.Errorf("included headers mode hash mismatch:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_IncludedHeadersIgnoresOthers(t *testing.T) {
	hc := NewHashComputer(nil, []string{"X-Api-Key"})

	h1 := http.Header{}
	h1.Set("X-Api-Key", "key1")
	h1.Set("Accept", "text/html")

	h2 := http.Header{}
	h2.Set("X-Api-Key", "key1")
	h2.Set("Accept", "application/json")
	h2.Set("X-Extra", "something")

	hash1, err := hc.ComputeHash("GET", "https://example.com/", h1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := hc.ComputeHash("GET", "https://example.com/", h2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("non-included headers affected hash in included mode: %s vs %s", hash1, hash2)
	}
}

func TestComputeHash_ExcludedHeadersMode_NonExcludedIncluded(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	h1 := http.Header{}
	h1.Set("Accept", "application/json")

	h2 := http.Header{}
	h2.Set("Accept", "text/xml")

	hash1, err := hc.ComputeHash("GET", "https://example.com/", h1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := hc.ComputeHash("GET", "https://example.com/", h2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different non-excluded header values should produce different hashes")
	}
}

func TestComputeHash_HeadersCaseInsensitive(t *testing.T) {
	hc := NewHashComputer(nil, []string{"content-type"})

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	hash, err := hc.ComputeHash("GET", "https://example.com/", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The included header "content-type" should match "Content-Type" via canonical key
	expected := sha256Hex("GET\nhttps://example.com/\nContent-Type:application/json")
	if hash != expected {
		t.Errorf("case-insensitive header matching failed:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_MultipleHeaderValuesSorted(t *testing.T) {
	hc := NewHashComputer(nil, nil)

	headers := http.Header{}
	headers.Add("Accept", "text/html")
	headers.Add("Accept", "application/json")

	hash, err := hc.ComputeHash("GET", "https://example.com/", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Values should be sorted: application/json comes before text/html
	expected := sha256Hex("GET\nhttps://example.com/\nAccept:application/json\nAccept:text/html")
	if hash != expected {
		t.Errorf("multi-value header hash mismatch:\n  got:  %s\n  want: %s", hash, expected)
	}
}

func TestComputeHash_InvalidURL(t *testing.T) {
	hc := NewHashComputer(nil, nil)

	_, err := hc.ComputeHash("GET", "://invalid", nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestNewHashComputer_DefaultExcluded(t *testing.T) {
	hc := NewHashComputer(DefaultExcludedHeaders, nil)

	for _, h := range DefaultExcludedHeaders {
		canonical := http.CanonicalHeaderKey(h)
		if !hc.ExcludedHeaders[canonical] {
			t.Errorf("expected %q to be in ExcludedHeaders", canonical)
		}
	}

	if len(hc.IncludedHeaders) != 0 {
		t.Errorf("expected empty IncludedHeaders, got %d entries", len(hc.IncludedHeaders))
	}
}
