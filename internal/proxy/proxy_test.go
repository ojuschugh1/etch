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
	"sync"
	"testing"
	"time"

	"github.com/ojuschugh1/etch/internal/approval"
	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// --- helpers ---

// fakeHandler records every HandleRequest call for inspection.
type fakeHandler struct {
	mu    sync.Mutex
	calls []handlerCall
}

type handlerCall struct {
	method string
	url    string
	status int
	body   string
}

func (f *fakeHandler) HandleRequest(req *http.Request, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, handlerCall{
		method: req.Method,
		url:    req.URL.String(),
		status: resp.StatusCode,
		body:   string(body),
	})
	return nil
}

func (f *fakeHandler) getCalls() []handlerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]handlerCall, len(f.calls))
	copy(cp, f.calls)
	return cp
}

// startProxy starts a ProxyServer on a random port and returns the address.
func startProxy(t *testing.T, mode ProxyMode, handler RequestHandler) (*ProxyServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // free the port so the proxy can bind

	ps := NewProxyServer(addr, mode, nil, handler)
	return ps, addr
}

// proxyGet sends a GET through the proxy using HTTP_PROXY style (absolute URL).
func proxyGet(t *testing.T, proxyAddr, targetURL string) (*http.Response, string) {
	t.Helper()
	transport := &http.Transport{
		Proxy: http.ProxyURL(mustParseURL(t, "http://"+proxyAddr)),
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		t.Fatalf("proxy GET %s: %v", targetURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}

// --- Tests ---


func TestHTTPForwardingRecordMode(t *testing.T) {
	// Start a mock upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "upstream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer upstream.Close()

	handler := &fakeHandler{}
	ps, addr := startProxy(t, ModeRecord, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ps.Start(ctx) }()

	// Wait for proxy to be ready.
	waitForProxy(t, addr)

	// Send request through the proxy.
	resp, body := proxyGet(t, addr, upstream.URL+"/api/test")

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if body != `{"status":"ok"}` {
		t.Errorf("expected body %q, got %q", `{"status":"ok"}`, body)
	}
	if resp.Header.Get("X-Test") != "upstream" {
		t.Errorf("expected X-Test header from upstream, got %q", resp.Header.Get("X-Test"))
	}

	// Verify handler was called.
	calls := handler.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 handler call, got %d", len(calls))
	}
	if calls[0].method != "GET" {
		t.Errorf("handler method = %q, want GET", calls[0].method)
	}
	if calls[0].status != 200 {
		t.Errorf("handler status = %d, want 200", calls[0].status)
	}
	if calls[0].body != `{"status":"ok"}` {
		t.Errorf("handler body = %q, want %q", calls[0].body, `{"status":"ok"}`)
	}

	cancel()
	<-errCh
}

func TestTestModeDetectsDiffAndReportsMismatch(t *testing.T) {
	// Set up snapshot store with a stored entry.
	snapDir := t.TempDir()
	store := snapshot.NewSnapshotStore(snapDir)
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	de := diff.NewDiffEngine()
	pendingDir := t.TempDir()
	am := approval.NewApprovalManager(store, pendingDir)

	// Record a baseline snapshot.
	reqHash, err := hc.ComputeHash("GET", "http://example.com/api/data", http.Header{})
	if err != nil {
		t.Fatalf("compute hash: %v", err)
	}
	storedEntry := snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "http://example.com/api/data",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"name":"Alice"}`,
	}
	if err := store.Record("example.com", reqHash, storedEntry); err != nil {
		t.Fatalf("record snapshot: %v", err)
	}

	// Create a TestHandler.
	th := NewTestHandler(hc, store, de, am)

	// Simulate a live response that differs.
	req, _ := http.NewRequest("GET", "http://example.com/api/data", nil)
	liveResp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"name":"Bob"}`)),
	}

	if err := th.HandleRequest(req, liveResp); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	summary := th.GetSummary()
	if summary.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", summary.TotalRequests)
	}
	if summary.Mismatches != 1 {
		t.Errorf("Mismatches = %d, want 1", summary.Mismatches)
	}
	if summary.Matches != 0 {
		t.Errorf("Matches = %d, want 0", summary.Matches)
	}

	// Verify a pending diff was saved.
	hasPending, err := am.HasPendingDiffs()
	if err != nil {
		t.Fatalf("HasPendingDiffs: %v", err)
	}
	if !hasPending {
		t.Error("expected pending diffs to be saved")
	}
}

func TestUnrecordedRequestHandling(t *testing.T) {
	snapDir := t.TempDir()
	store := snapshot.NewSnapshotStore(snapDir)
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	de := diff.NewDiffEngine()
	pendingDir := t.TempDir()
	am := approval.NewApprovalManager(store, pendingDir)

	th := NewTestHandler(hc, store, de, am)

	// Send a request with no stored snapshot.
	req, _ := http.NewRequest("GET", "http://unknown.example.com/missing", nil)
	liveResp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`hello`)),
	}

	if err := th.HandleRequest(req, liveResp); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	summary := th.GetSummary()
	if summary.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", summary.TotalRequests)
	}
	if summary.Unrecorded != 1 {
		t.Errorf("Unrecorded = %d, want 1", summary.Unrecorded)
	}
	if summary.Matches != 0 {
		t.Errorf("Matches = %d, want 0", summary.Matches)
	}
	if summary.Mismatches != 0 {
		t.Errorf("Mismatches = %d, want 0", summary.Mismatches)
	}
}

func TestPortInUseErrorMessage(t *testing.T) {
	// Occupy a port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	occupiedAddr := ln.Addr().String()

	ps := NewProxyServer(occupiedAddr, ModeRecord, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ps.Start(ctx)
	if err == nil {
		t.Fatal("expected error when port is in use, got nil")
	}
	if !strings.Contains(err.Error(), occupiedAddr) && !strings.Contains(err.Error(), "listen") {
		t.Errorf("error should mention the port or listen failure, got: %v", err)
	}
}

// Shutdown should wait for in-flight requests to finish before returning.
func TestGracefulShutdownDrainsInflightRequests(t *testing.T) {
	// Create an upstream that blocks until we signal it.
	unblock := make(chan struct{})
	requestStarted := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-unblock
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "done")
	}))
	defer upstream.Close()

	handler := &fakeHandler{}
	ps, addr := startProxy(t, ModeRecord, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ps.Start(ctx) }()

	waitForProxy(t, addr)

	// Start an in-flight request in a goroutine.
	var clientResp *http.Response
	var clientBody string
	var clientErr error
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		transport := &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, "http://"+addr)),
		}
		client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
		resp, err := client.Get(upstream.URL + "/slow")
		if err != nil {
			clientErr = err
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		clientResp = resp
		clientBody = string(body)
	}()

	// Wait for the request to reach the upstream.
	<-requestStarted

	// Initiate graceful shutdown.
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- ps.Shutdown(context.Background())
	}()

	// Give shutdown a moment to start draining, then unblock the upstream.
	time.Sleep(50 * time.Millisecond)
	close(unblock)

	// Wait for shutdown to complete.
	if err := <-shutdownDone; err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Wait for the client request to finish.
	<-clientDone
	if clientErr != nil {
		t.Fatalf("client request error: %v", clientErr)
	}
	if clientResp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", clientResp.StatusCode)
	}
	if clientBody != "done" {
		t.Errorf("expected body %q, got %q", "done", clientBody)
	}

	cancel()
	<-errCh
}

func TestRecordHandlerPersistsSnapshot(t *testing.T) {
	snapDir := t.TempDir()
	store := snapshot.NewSnapshotStore(snapDir)
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	rh := NewRecordHandler(hc, store)

	req, _ := http.NewRequest("GET", "http://api.example.com/users", nil)
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`[{"id":1}]`)),
	}

	if err := rh.HandleRequest(req, resp); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	// Verify snapshot was persisted.
	reqHash, _ := hc.ComputeHash("GET", "http://api.example.com/users", http.Header{})
	entry, err := store.Lookup("api.example.com", reqHash)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if entry == nil {
		t.Fatal("expected snapshot entry, got nil")
	}
	if entry.Body != `[{"id":1}]` {
		t.Errorf("body = %q, want %q", entry.Body, `[{"id":1}]`)
	}
	if entry.StatusCode != 200 {
		t.Errorf("status = %d, want 200", entry.StatusCode)
	}
}

// When the live response matches the stored snapshot, it should count as a match.
func TestTestModeMatchingSnapshot(t *testing.T) {
	snapDir := t.TempDir()
	store := snapshot.NewSnapshotStore(snapDir)
	hc := hash.NewHashComputer(hash.DefaultExcludedHeaders, nil)
	de := diff.NewDiffEngine()
	pendingDir := t.TempDir()
	am := approval.NewApprovalManager(store, pendingDir)

	reqHash, _ := hc.ComputeHash("GET", "http://example.com/ok", http.Header{})
	entry := snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "http://example.com/ok",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       "hello",
	}
	if err := store.Record("example.com", reqHash, entry); err != nil {
		t.Fatalf("record: %v", err)
	}

	th := NewTestHandler(hc, store, de, am)

	req, _ := http.NewRequest("GET", "http://example.com/ok", nil)
	liveResp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("hello")),
	}

	if err := th.HandleRequest(req, liveResp); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	summary := th.GetSummary()
	if summary.Matches != 1 {
		t.Errorf("Matches = %d, want 1", summary.Matches)
	}
	if summary.Mismatches != 0 {
		t.Errorf("Mismatches = %d, want 0", summary.Mismatches)
	}
}

func TestProxyModeString(t *testing.T) {
	tests := []struct {
		mode ProxyMode
		want string
	}{
		{ModeRecord, "record"},
		{ModeTest, "test"},
		{ProxyMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("ProxyMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

// --- helpers ---

func waitForProxy(t *testing.T, addr string) {
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
	t.Fatalf("proxy at %s did not become ready", addr)
}
