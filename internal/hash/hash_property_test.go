package hash

import (
	"net/http"
	"testing"

	"pgregory.net/rapid"
)

// Same inputs should always produce the same hash. Sounds obvious but
// this catches any accidental nondeterminism (map iteration order, etc).
func TestHashDeterminism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}).Draw(rt, "method")
		rawURL := genURL(rt)
		headers := genHeaders(rt)

		hc := NewHashComputer(DefaultExcludedHeaders, nil)

		h1, err := hc.ComputeHash(method, rawURL, headers)
		if err != nil {
			rt.Fatalf("first hash failed: %v", err)
		}

		h2, err := hc.ComputeHash(method, rawURL, headers)
		if err != nil {
			rt.Fatalf("second hash failed: %v", err)
		}

		if h1 != h2 {
			rt.Fatalf("got different hashes for identical input: %q vs %q (method=%q url=%q)", h1, h2, method, rawURL)
		}
	})
}

func genURL(t *rapid.T) string {
	scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(t, "scheme")
	host := rapid.StringMatching(`[a-z][a-z0-9]{0,15}\.[a-z]{2,4}`).Draw(t, "host")
	path := rapid.StringMatching(`(/[a-z0-9]{1,10}){0,4}`).Draw(t, "path")

	n := rapid.IntRange(0, 4).Draw(t, "num_params")
	query := ""
	for i := 0; i < n; i++ {
		key := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "qkey")
		val := rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(t, "qval")
		if i == 0 {
			query += "?"
		} else {
			query += "&"
		}
		query += key + "=" + val
	}

	return scheme + "://" + host + path + query
}

func genHeaders(t *rapid.T) http.Header {
	h := make(http.Header)
	n := rapid.IntRange(0, 5).Draw(t, "num_headers")
	for i := 0; i < n; i++ {
		key := rapid.StringMatching(`[A-Z][a-z]{1,10}(-[A-Z][a-z]{1,10}){0,2}`).Draw(t, "hkey")
		val := rapid.StringMatching(`[a-zA-Z0-9 /;=]{1,30}`).Draw(t, "hval")
		h.Set(key, val)
	}
	return h
}

// ?b=2&a=1 and ?a=1&b=2 should hash the same. We generate URLs with
// random query params, shuffle them, and check both produce identical hashes.
func TestHashIgnoresQueryParamOrder(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(rt, "scheme")
		host := rapid.StringMatching(`[a-z][a-z0-9]{0,15}\.[a-z]{2,4}`).Draw(rt, "host")
		path := rapid.StringMatching(`(/[a-z0-9]{1,10}){0,4}`).Draw(rt, "path")

		// need at least 2 params for ordering to matter
		n := rapid.IntRange(2, 6).Draw(rt, "num_params")

		type kv struct{ key, val string }
		params := make([]kv, n)
		for i := range params {
			params[i] = kv{
				key: rapid.StringMatching(`[a-z]{1,8}`).Draw(rt, "qkey"),
				val: rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(rt, "qval"),
			}
		}

		buildURL := func(ps []kv) string {
			q := ""
			for i, p := range ps {
				if i == 0 {
					q += "?"
				} else {
					q += "&"
				}
				q += p.key + "=" + p.val
			}
			return scheme + "://" + host + path + q
		}

		url1 := buildURL(params)

		shuffled := make([]kv, len(params))
		copy(shuffled, params)
		perm := rapid.Permutation(shuffled).Draw(rt, "perm")
		url2 := buildURL(perm)

		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH"}).Draw(rt, "method")
		headers := genHeaders(rt)
		hc := NewHashComputer(DefaultExcludedHeaders, nil)

		h1, _ := hc.ComputeHash(method, url1, headers)
		h2, _ := hc.ComputeHash(method, url2, headers)

		if h1 != h2 {
			rt.Fatalf("query param order affected hash:\n  %q -> %q\n  %q -> %q", url1, h1, url2, h2)
		}
	})
}

// Changing Date, Authorization, X-Request-Id etc. shouldn't affect the hash
// since those are excluded by default. We pick a random excluded header,
// set it to two different values, and verify the hash stays the same.
func TestHashIgnoresExcludedHeaders(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}).Draw(rt, "method")
		rawURL := genURL(rt)
		headers := genHeaders(rt)

		excluded := rapid.SampledFrom(DefaultExcludedHeaders).Draw(rt, "excluded")

		val1 := rapid.StringMatching(`[a-zA-Z0-9]{1,30}`).Draw(rt, "val1")
		val2 := rapid.StringMatching(`[a-zA-Z0-9]{1,30}`).Draw(rt, "val2")

		h1 := headers.Clone()
		h1.Set(excluded, val1)

		h2 := headers.Clone()
		h2.Set(excluded, val2)

		hc := NewHashComputer(DefaultExcludedHeaders, nil)

		hash1, _ := hc.ComputeHash(method, rawURL, h1)
		hash2, _ := hc.ComputeHash(method, rawURL, h2)

		if hash1 != hash2 {
			rt.Fatalf("excluded header %q affected hash: %q vs %q (vals: %q, %q)",
				excluded, hash1, hash2, val1, val2)
		}
	})
}
