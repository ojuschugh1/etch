package mock

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func setupStore(t *testing.T) *snapshot.SnapshotStore {
	t.Helper()
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)

	store.Record("api.example.com", "h1", snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "http://api.example.com/users",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `[{"id":1,"name":"Alice"}]`,
	})
	store.Record("api.example.com", "h2", snapshot.SnapshotEntry{
		Method:     "POST",
		URL:        "http://api.example.com/users",
		StatusCode: 201,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"id":2,"name":"Bob"}`,
	})
	store.Record("api.example.com", "h3", snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "http://api.example.com/health",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       "ok",
	})

	return store
}

func TestNewServer_LoadsEntries(t *testing.T) {
	store := setupStore(t)
	srv, err := NewServer(store, ":0")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv.EntryCount() != 3 {
		t.Fatalf("expected 3 entries, got %d", srv.EntryCount())
	}
}

func TestNewServer_EmptyStore(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	srv, _ := NewServer(store, ":0")
	err := srv.Start()
	if err == nil {
		t.Fatal("expected error for empty store")
	}
}

func TestMockServer_ServesRecordedResponse(t *testing.T) {
	store := setupStore(t)
	srv, _ := NewServer(store, ":0")

	handler := http.HandlerFunc(srv.handle)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/users")
	if err != nil {
		t.Fatalf("GET /users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type: got %q", resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[{"id":1,"name":"Alice"}]` {
		t.Errorf("body: got %q", body)
	}
}

func TestMockServer_POST(t *testing.T) {
	store := setupStore(t)
	srv, _ := NewServer(store, ":0")

	handler := http.HandlerFunc(srv.handle)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/users", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("status: got %d, want 201", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"id":2,"name":"Bob"}` {
		t.Errorf("body: got %q", body)
	}
}

func TestMockServer_UnknownEndpoint_404(t *testing.T) {
	store := setupStore(t)
	srv, _ := NewServer(store, ":0")

	handler := http.HandlerFunc(srv.handle)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/nonexistent")
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
