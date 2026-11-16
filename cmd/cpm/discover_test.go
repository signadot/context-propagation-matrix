package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// rewriteHost sends every request to the fake server, whatever host the
// production code names, so publishedReleases runs unmodified.
type rewriteHost struct{ to *url.URL }

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

// The repo holds well over 1000 releases (one backtest-* per measured pair),
// so publishedReleases must keep paging until the API runs out. The
// variant-* release sits past entry 1000 to catch a fixed page bound.
func TestPublishedReleasesPagesPastOneThousand(t *testing.T) {
	type asset struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	type release struct {
		TagName string  `json:"tag_name"`
		Draft   bool    `json:"draft,omitempty"`
		Assets  []asset `json:"assets,omitempty"`
	}
	const backtests = 1200
	var all []release
	for i := 0; i < backtests; i++ {
		all = append(all, release{TagName: fmt.Sprintf("backtest-otel-java-1.%d.0", i)})
	}
	all = append(all,
		release{TagName: "backtest-otel-java-draft", Draft: true},
		release{TagName: "variant-otel-java", Assets: []asset{{Name: "index.json", URL: "https://api.github.com/assets/7"}}},
	)

	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		if r.URL.Path != "/repos/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if perPage < 1 || perPage > 100 || page < 1 {
			http.Error(w, "bad paging", http.StatusUnprocessableEntity)
			return
		}
		lo := min((page-1)*perPage, len(all))
		hi := min(lo+perPage, len(all))
		json.NewEncoder(w).Encode(all[lo:hi])
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	client := &versions.Client{HTTP: &http.Client{Transport: rewriteHost{u}}}

	rels, err := publishedReleases(context.Background(), client, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(rels.backtests) != backtests {
		t.Errorf("saw %d backtest releases, want %d (requests: %v)", len(rels.backtests), backtests, requests)
	}
	seen := map[string]bool{}
	for _, tag := range rels.backtests {
		seen[tag] = true
	}
	for _, tag := range []string{"backtest-otel-java-1.0.0", "backtest-otel-java-1.999.0", "backtest-otel-java-1.1199.0"} {
		if !seen[tag] {
			t.Errorf("%s not seen", tag)
		}
	}
	if seen["backtest-otel-java-draft"] {
		t.Error("draft release counted as published")
	}
	if got := rels.indexes["otel-java"]; got != "https://api.github.com/assets/7" {
		t.Errorf("variant-otel-java index asset = %q, want the one listed past entry 1000", got)
	}
}
