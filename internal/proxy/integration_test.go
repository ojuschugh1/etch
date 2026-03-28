package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ojuschugh1/etch/internal/approval"
	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/noise"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Full round-trip: record through the proxy, then test through the proxy
// with the same upstream. Matching responses should produce zero mismatches.
func TestRecordThenTest_MatchingResponses(t *testing.T) {
	// deterministic upstream
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"user":"alice","id":1}`)
	}))
	defer upstream.Close()

	snapDir := t.TempDir()
	pendingDir := t.TempDir()
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	store := snapshot.NewSnapshotStore(snapDir)

	// --- record phase ---
	recHandler := NewRecordHandler(hc, store)
	recProxy, recAddr := startTestProxy(t, ModeRecord, recHandler)

	ctx, cancel := context.WithCancel(context.Background())
	go recProxy.Start(ctx)
	waitReady(t, recAddr)

	doProxyGet(t, recAddr, upstream.URL+"/api/user")
	cancel()
	time.Sleep(50 * time.Millisecond)

	// --- test phase ---
	de := diff.NewDiffEngineFull(nil, noise.NewNormalizer())
	am := approval.NewApprovalManager(store, pendingDir)
	testHandler := NewTestHandler(hc, store, de, am)
	testProxy, testAddr := startTestProxy(t, ModeTest, testHandler)

	ctx2, cancel2 := context.WithCancel(context.Background())
	go testProxy.Start(ctx2)
	waitReady(t, testAddr)

	doProxyGet(t, testAddr, upstream.URL+"/api/user")
	cancel2()
	time.Sleep(50 * time.Millisecond)

	summary := testHandler.GetSummary()
	if summary.Matches != 1 {
		t.Errorf("expected 1 match, got %d", summary.Matches)
	}
	if summary.Mismatches != 0 {
		t.Errorf("expected 0 mismatches, got %d", summary.Mismatches)
	}
}

// Record, then change the upstream response, then test. Should detect
// the mismatch and save a pending diff.
func TestRecordThenTest_DetectsMismatch(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		callCount++
		if callCount == 1 {
			fmt.Fprint(w, `{"name":"alice"}`)
		} else {
			fmt.Fprint(w, `{"name":"bob"}`)
		}
	}))
	defer upstream.Close()

	snapDir := t.TempDir()
	pendingDir := t.TempDir()
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	store := snapshot.NewSnapshotStore(snapDir)

	// record
	recHandler := NewRecordHandler(hc, store)
	recProxy, recAddr := startTestProxy(t, ModeRecord, recHandler)
	ctx, cancel := context.WithCancel(context.Background())
	go recProxy.Start(ctx)
	waitReady(t, recAddr)
	doProxyGet(t, recAddr, upstream.URL+"/data")
	cancel()
	time.Sleep(50 * time.Millisecond)

	// test (upstream now returns different body)
	de := diff.NewDiffEngineFull(nil, noise.NewNormalizer())
	am := approval.NewApprovalManager(store, pendingDir)
	testHandler := NewTestHandler(hc, store, de, am)
	testProxy, testAddr := startTestProxy(t, ModeTest, testHandler)
	ctx2, cancel2 := context.WithCancel(context.Background())
	go testProxy.Start(ctx2)
	waitReady(t, testAddr)
	doProxyGet(t, testAddr, upstream.URL+"/data")
	cancel2()
	time.Sleep(50 * time.Millisecond)

	summary := testHandler.GetSummary()
	if summary.Mismatches != 1 {
		t.Errorf("expected 1 mismatch, got %d", summary.Mismatches)
	}

	hasPending, _ := am.HasPendingDiffs()
	if !hasPending {
		t.Error("expected pending diffs after mismatch")
	}
}

// Record multiple endpoints, test them all, verify counts.
func TestRecordThenTest_MultipleEndpoints(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users":
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		case "/health":
			fmt.Fprint(w, `{"status":"ok"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":"not found"}`)
		}
	}))
	defer upstream.Close()

	snapDir := t.TempDir()
	pendingDir := t.TempDir()
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	store := snapshot.NewSnapshotStore(snapDir)

	// record both endpoints
	recHandler := NewRecordHandler(hc, store)
	recProxy, recAddr := startTestProxy(t, ModeRecord, recHandler)
	ctx, cancel := context.WithCancel(context.Background())
	go recProxy.Start(ctx)
	waitReady(t, recAddr)
	doProxyGet(t, recAddr, upstream.URL+"/users")
	doProxyGet(t, recAddr, upstream.URL+"/health")
	cancel()
	time.Sleep(50 * time.Millisecond)

	// test both
	de := diff.NewDiffEngineFull(nil, noise.NewNormalizer())
	am := approval.NewApprovalManager(store, pendingDir)
	testHandler := NewTestHandler(hc, store, de, am)
	testProxy, testAddr := startTestProxy(t, ModeTest, testHandler)
	ctx2, cancel2 := context.WithCancel(context.Background())
	go testProxy.Start(ctx2)
	waitReady(t, testAddr)
	doProxyGet(t, testAddr, upstream.URL+"/users")
	doProxyGet(t, testAddr, upstream.URL+"/health")
	cancel2()
	time.Sleep(50 * time.Millisecond)

	summary := testHandler.GetSummary()
	if summary.TotalRequests != 2 {
		t.Errorf("total: got %d, want 2", summary.TotalRequests)
	}
	if summary.Matches != 2 {
		t.Errorf("matches: got %d, want 2", summary.Matches)
	}
	if summary.Mismatches != 0 {
		t.Errorf("mismatches: got %d, want 0", summary.Mismatches)
	}
}

// Test mode with an endpoint that was never recorded should count as unrecorded.
func TestTestMode_UnrecordedEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	snapDir := t.TempDir()
	pendingDir := t.TempDir()
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	store := snapshot.NewSnapshotStore(snapDir)
	de := diff.NewDiffEngineFull(nil, noise.NewNormalizer())
	am := approval.NewApprovalManager(store, pendingDir)

	// skip record, go straight to test
	testHandler := NewTestHandler(hc, store, de, am)
	testProxy, testAddr := startTestProxy(t, ModeTest, testHandler)
	ctx, cancel := context.WithCancel(context.Background())
	go testProxy.Start(ctx)
	waitReady(t, testAddr)
	doProxyGet(t, testAddr, upstream.URL+"/never-recorded")
	cancel()
	time.Sleep(50 * time.Millisecond)

	summary := testHandler.GetSummary()
	if summary.Unrecorded != 1 {
		t.Errorf("unrecorded: got %d, want 1", summary.Unrecorded)
	}
}

// Upstream returns 502 - proxy should still forward it and record it.
func TestProxy_UpstreamErrorForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, "bad gateway")
	}))
	defer upstream.Close()

	snapDir := t.TempDir()
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	store := snapshot.NewSnapshotStore(snapDir)
	recHandler := NewRecordHandler(hc, store)

	proxy, addr := startTestProxy(t, ModeRecord, recHandler)
	ctx, cancel := context.WithCancel(context.Background())
	go proxy.Start(ctx)
	waitReady(t, addr)

	resp, body := doProxyGet(t, addr, upstream.URL+"/fail")
	cancel()

	if resp.StatusCode != 502 {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
	if body != "bad gateway" {
		t.Errorf("expected 'bad gateway', got %q", body)
	}
}

// --- helpers ---

func startTestProxy(t *testing.T, mode ProxyMode, handler RequestHandler) (*ProxyServer, string) {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	return NewProxyServer(addr, mode, nil, handler), addr
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("proxy at %s didn't start in time", addr)
}

func doProxyGet(t *testing.T, proxyAddr, targetURL string) (*http.Response, string) {
	t.Helper()
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: proxyAddr}),
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		t.Fatalf("proxy GET %s: %v", targetURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, strings.TrimSpace(string(body))
}
