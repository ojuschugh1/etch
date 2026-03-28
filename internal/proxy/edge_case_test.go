package proxy

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ojuschugh1/etch/internal/approval"
	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/noise"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Concurrent requests to the same host shouldn't corrupt the snap file.
func TestConcurrentRecording(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":"%s"}`, r.URL.Path)
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

	// fire 20 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			doProxyGet(t, addr, fmt.Sprintf("%s/item/%d", upstream.URL, n))
		}(i)
	}
	wg.Wait()

	cancel()
	time.Sleep(100 * time.Millisecond)

	// all entries should be readable
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll after concurrent writes: %v", err)
	}

	total := 0
	for _, sf := range all {
		total += len(sf)
	}
	if total != 20 {
		t.Errorf("expected 20 entries, got %d", total)
	}
}

// Gzip-compressed upstream responses should be decompressed in snapshots.
func TestGzipResponseDecompressed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")

		gz := gzip.NewWriter(w)
		gz.Write([]byte(`{"name":"Alice","id":1}`))
		gz.Close()
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

	_, body := doProxyGet(t, addr, upstream.URL+"/user")
	cancel()
	time.Sleep(50 * time.Millisecond)

	// the response to the client should be decompressed
	if !strings.Contains(body, "Alice") {
		t.Errorf("client got compressed body: %q", body)
	}

	// the snapshot should contain readable JSON, not binary gzip
	all, _ := store.LoadAll()
	for _, sf := range all {
		for _, entry := range sf {
			if !strings.Contains(entry.Body, "Alice") {
				t.Errorf("snapshot contains compressed body: %q", entry.Body)
			}
		}
	}
}

// Record then test with gzip upstream - should still match.
func TestGzipRecordThenTest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		gz.Write([]byte(`{"status":"ok"}`))
		gz.Close()
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
	doProxyGet(t, recAddr, upstream.URL+"/health")
	cancel()
	time.Sleep(50 * time.Millisecond)

	// test
	de := diff.NewDiffEngineFull(nil, noise.NewNormalizer())
	am := approval.NewApprovalManager(store, pendingDir)
	testHandler := NewTestHandler(hc, store, de, am)
	testProxy, testAddr := startTestProxy(t, ModeTest, testHandler)
	ctx2, cancel2 := context.WithCancel(context.Background())
	go testProxy.Start(ctx2)
	waitReady(t, testAddr)
	doProxyGet(t, testAddr, upstream.URL+"/health")
	cancel2()
	time.Sleep(50 * time.Millisecond)

	summary := testHandler.GetSummary()
	if summary.Matches != 1 {
		t.Errorf("expected 1 match, got %d matches %d mismatches", summary.Matches, summary.Mismatches)
	}
}
