package watch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Watcher periodically replays recorded requests and reports changes.
type Watcher struct {
	Store         *snapshot.SnapshotStore
	Engine        *diff.DiffEngine
	Interval      time.Duration
	InjectHeaders map[string]string // optional headers to inject (e.g. auth tokens)
}

// NewWatcher creates a watcher that polls the live API every interval.
func NewWatcher(store *snapshot.SnapshotStore, engine *diff.DiffEngine, interval time.Duration) *Watcher {
	return &Watcher{
		Store:    store,
		Engine:   engine,
		Interval: interval,
	}
}

// Watch starts the monitoring loop. Blocks until context is cancelled.
func (w *Watcher) Watch(ctx context.Context) error {
	all, err := w.Store.LoadAll()
	if err != nil {
		return fmt.Errorf("loading snapshots: %w", err)
	}

	if len(all) == 0 {
		return fmt.Errorf("no snapshots found - run 'etch record' first")
	}

	// collect all entries to replay
	var entries []entry
	for host, sf := range all {
		for hash, e := range sf {
			entries = append(entries, entry{host, hash, e})
		}
	}

	fmt.Printf("Watching %d endpoint(s) (polling every %s)\n", len(entries), w.Interval)
	if len(w.InjectHeaders) > 0 {
		fmt.Printf("Injecting %d header(s) into requests\n", len(w.InjectHeaders))
	}
	fmt.Println("Press Ctrl+C to stop.")

	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nWatch stopped.")
			return nil
		case <-ticker.C:
			w.poll(ctx, client, entries)
		}
	}
}

func (w *Watcher) poll(ctx context.Context, client *http.Client, entries []entry) {
	changes := 0
	for _, e := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// replay the request against the live API
		req, err := http.NewRequestWithContext(ctx, e.snap.Method, e.snap.URL, nil)
		if err != nil {
			continue
		}

		// inject configured headers (e.g. auth tokens)
		for k, v := range w.InjectHeaders {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			continue // API might be down, skip silently
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		live := &snapshot.SnapshotEntry{
			Method:     e.snap.Method,
			URL:        e.snap.URL,
			StatusCode: resp.StatusCode,
			Headers:    cloneHeaders(resp.Header),
			Body:       string(body),
		}

		stored := e.snap
		result := w.Engine.Compare(&stored, live)
		if !result.Matched {
			result.URL = e.snap.URL
			if changes == 0 {
				ts := time.Now().Format("15:04:05")
				fmt.Printf("\n[%s] Changes detected:\n", ts)
			}
			fmt.Print(w.Engine.FormatDiff(result, true))
			changes++
		}
	}

	if changes == 0 {
		fmt.Print(".")
	}
}

type entry struct {
	host string
	hash string
	snap snapshot.SnapshotEntry
}

func cloneHeaders(h http.Header) map[string][]string {
	clone := make(map[string][]string, len(h))
	for k, vv := range h {
		vals := make([]string, len(vv))
		copy(vals, vv)
		clone[k] = vals
	}
	return clone
}
