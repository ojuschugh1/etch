package coverage

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Endpoint represents a unique API endpoint (method + path).
type Endpoint struct {
	Method string
	Path   string
	Host   string
}

func (e Endpoint) String() string {
	return e.Method + " " + e.Host + e.Path
}

// Report holds coverage data: which endpoints have snapshots.
type Report struct {
	Recorded []Endpoint
	Total    int
}

// Generate builds a coverage report from all recorded snapshots.
func Generate(store *snapshot.SnapshotStore) (*Report, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	seen := make(map[string]Endpoint)
	for host, sf := range all {
		for _, entry := range sf {
			parsed, err := url.Parse(entry.URL)
			if err != nil {
				continue
			}
			key := entry.Method + " " + host + parsed.Path
			seen[key] = Endpoint{
				Method: entry.Method,
				Path:   parsed.Path,
				Host:   host,
			}
		}
	}

	endpoints := make([]Endpoint, 0, len(seen))
	for _, ep := range seen {
		endpoints = append(endpoints, ep)
	}
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].String() < endpoints[j].String()
	})

	return &Report{
		Recorded: endpoints,
		Total:    len(endpoints),
	}, nil
}

// Format produces a human-readable coverage summary.
func (r *Report) Format() string {
	if r.Total == 0 {
		return "No recorded endpoints. Run 'etch record' first."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Recorded endpoints: %d\n\n", r.Total))

	// group by host
	byHost := make(map[string][]Endpoint)
	for _, ep := range r.Recorded {
		byHost[ep.Host] = append(byHost[ep.Host], ep)
	}

	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	for _, host := range hosts {
		b.WriteString(fmt.Sprintf("  %s\n", host))
		for _, ep := range byHost[host] {
			b.WriteString(fmt.Sprintf("    %-7s %s\n", ep.Method, ep.Path))
		}
	}

	return b.String()
}
