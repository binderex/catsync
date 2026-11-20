package ygo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/catalog"
)

func TestFetchMediawikiSets(t *testing.T) {
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if action := r.URL.Query().Get("action"); action != "ask" {
			t.Errorf("unexpected action: %s", action)
		}

		query := r.URL.Query().Get("query")
		queries = append(queries, query)

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"query": {"results": {
			"Zoodiac Duel Set": {"fullurl": "https://yugipedia.com/wiki/Zoodiac_Duel_Set", "namespace": 0,
			 "printouts": {"Medium": ["TCG", "Official"]}},
			"Ancient Sanctuary": {"fullurl": "https://yugipedia.com/wiki/Ancient_Sanctuary", "namespace": 0,
			 "printouts": {"Medium": ["OCG", "TCG", "Official"]}},
			"Vol.1": {"fullurl": "https://yugipedia.com/wiki/Vol.1", "namespace": 0,
			 "printouts": {"Medium": ["Yu-Gi-Oh! Official Card Game"]}},
			"Category:TCG sets": {"fullurl": "https://yugipedia.com/wiki/Category:TCG_sets", "namespace": 14},
			"Missing URL": {"namespace": 0, "printouts": {"Medium": ["TCG"]}}
		}}}`)
	}))
	defer server.Close()

	sets, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, nil)
	if err != nil {
		t.Fatalf("fetch mediawiki sets: %v", err)
	}

	want := []mediawikiSetResponse{
		{
			URL:     "https://yugipedia.com/wiki/Ancient_Sanctuary",
			Name:    "Ancient Sanctuary",
			Mediums: []string{"OCG", "TCG"},
		},
		{
			URL:     "https://yugipedia.com/wiki/Vol.1",
			Name:    "Vol.1",
			Mediums: []string{"OCG"},
		},
		{
			URL:     "https://yugipedia.com/wiki/Zoodiac_Duel_Set",
			Name:    "Zoodiac Duel Set",
			Mediums: []string{"TCG"},
		},
	}
	if !reflect.DeepEqual(sets, want) {
		t.Fatalf("sets = %+v, want %+v", sets, want)
	}

	if len(queries) != 1 {
		t.Fatalf("queries = %v, want 1", queries)
	}
	for _, part := range []string{"[[Category:All sets]]", "?Medium", "|limit=1000"} {
		if !strings.Contains(queries[0], part) {
			t.Errorf("unexpected ask query %q: missing %q", queries[0], part)
		}
	}
}

// TestFetchMediawikiSetsRetries checks that pages go through retrier.HTTP: a
// transient 503 is retried instead of failing the whole set index. It costs the
// policy backoff (2s).
func TestFetchMediawikiSetsRetries(t *testing.T) {
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		_, _ = fmt.Fprint(w, `{"query": {"results": {
			"Ancient Sanctuary": {"fullurl": "https://yugipedia.com/wiki/Ancient_Sanctuary", "namespace": 0,
			 "printouts": {"Medium": ["TCG"]}}
		}}}`)
	}))
	defer server.Close()

	sets, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, nil)
	if err != nil {
		t.Fatalf("fetch mediawiki sets: %v", err)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2 (one retry)", requests)
	}
	if len(sets) != 1 || sets[0].Name != "Ancient Sanctuary" {
		t.Fatalf("sets = %+v", sets)
	}
}

func TestFetchMediawikiSetsErrors(t *testing.T) {
	// 400 is not retryable, so the test does not pay the backoff delay.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	if _, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, nil); !errors.Is(err, ErrMediawikiUnexpectedStatus) {
		t.Fatalf("expected ErrMediawikiUnexpectedStatus, got %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"query": {"results": []}}`)
	}))
	defer empty.Close()

	if _, err := fetchMediawikiSets(context.Background(), empty.Client(), empty.URL, nil); !errors.Is(err, ErrSetsNotFound) {
		t.Fatalf("expected ErrSetsNotFound, got %v", err)
	}
}

func TestFetchMediawikiMediums(t *testing.T) {
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		params := r.URL.Query()
		if params.Get("action") != "ask" {
			t.Errorf("unexpected action: %s", params.Get("action"))
		}
		if query := params.Get("query"); strings.Contains(query, "Medium::") {
			t.Errorf("mediums must be filtered in memory, got query: %s", query)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"query": {"results": {
			"Ancient Sanctuary": {"fullurl": "https://yugipedia.com/wiki/Ancient_Sanctuary", "namespace": 0,
			 "printouts": {"Medium": ["OCG", "TCG", "Official"]}},
			"Duel Devastator": {"fullurl": "https://yugipedia.com/wiki/Duel_Devastator", "namespace": 0,
			 "printouts": {"Medium": ["TCG", "Official"]}},
			"Vol.1": {"fullurl": "https://yugipedia.com/wiki/Vol.1", "namespace": 0,
			 "printouts": {"Medium": ["OCG", "Official"]}},
			"Over Rush Pack": {"fullurl": "https://yugipedia.com/wiki/Over_Rush_Pack", "namespace": 0,
			 "printouts": {"Medium": ["Yu-Gi-Oh! Rush Duel"]}},
			"Absolute Powerforce (World Championship 2010)": {"fullurl": "https://yugipedia.com/wiki/Absolute_Powerforce_(World_Championship_2010)", "namespace": 0,
			 "printouts": {"Medium": ["Video game"]}},
			"No medium": {"fullurl": "https://yugipedia.com/wiki/No_medium", "namespace": 0}
		}}}`)
	}))
	defer server.Close()

	sets, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, []string{"tcg", MediumOCG})
	if err != nil {
		t.Fatalf("fetch mediawiki sets: %v", err)
	}

	want := []mediawikiSetResponse{
		{
			URL:     "https://yugipedia.com/wiki/Ancient_Sanctuary",
			Name:    "Ancient Sanctuary",
			Mediums: []string{"OCG", "TCG"},
		},
		{
			URL:     "https://yugipedia.com/wiki/Duel_Devastator",
			Name:    "Duel Devastator",
			Mediums: []string{"TCG"},
		},
		{
			URL:     "https://yugipedia.com/wiki/Vol.1",
			Name:    "Vol.1",
			Mediums: []string{"OCG"},
		},
	}
	if !reflect.DeepEqual(sets, want) {
		t.Fatalf("sets = %+v, want %+v", sets, want)
	}

	// Filtering happens in memory, so asking for a single medium is another
	// full fetch, never a second request per medium within one call.
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	rush, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, []string{MediumRush})
	if err != nil {
		t.Fatalf("fetch mediawiki sets for rush: %v", err)
	}
	if len(rush) != 1 || rush[0].URL != "https://yugipedia.com/wiki/Over_Rush_Pack" {
		t.Fatalf("rush sets = %+v", rush)
	}
}

func TestFetchMediawikiMediumPagination(t *testing.T) {
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		queries = append(queries, query)

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(query, "|offset=1000") {
			_, _ = fmt.Fprint(w, `{"query": {"results": {
				"Zoodiac Duel Set": {"fullurl": "https://yugipedia.com/wiki/Zoodiac_Duel_Set", "namespace": 0,
				 "printouts": {"Medium": ["TCG"]}}
			}}}`)
			return
		}

		_, _ = fmt.Fprint(w, `{"query-continue-offset": 1000, "query": {"results": {
			"Ancient Sanctuary": {"fullurl": "https://yugipedia.com/wiki/Ancient_Sanctuary", "namespace": 0,
			 "printouts": {"Medium": ["TCG"]}}
		}}}`)
	}))
	defer server.Close()

	sets, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, []string{MediumTCG})
	if err != nil {
		t.Fatalf("fetch mediawiki sets: %v", err)
	}

	want := []mediawikiSetResponse{
		{
			URL:     "https://yugipedia.com/wiki/Ancient_Sanctuary",
			Name:    "Ancient Sanctuary",
			Mediums: []string{"TCG"},
		},
		{
			URL:     "https://yugipedia.com/wiki/Zoodiac_Duel_Set",
			Name:    "Zoodiac Duel Set",
			Mediums: []string{"TCG"},
		},
	}
	if !reflect.DeepEqual(sets, want) {
		t.Fatalf("sets = %+v, want %+v", sets, want)
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %v, want 2", queries)
	}
}

func TestFetchMediawikiMediumErrors(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"query": {"results": []}}`)
	}))
	defer empty.Close()

	if _, err := fetchMediawikiSets(context.Background(), empty.Client(), empty.URL, []string{MediumRush}); !errors.Is(err, ErrSetsNotFound) {
		t.Fatalf("expected ErrSetsNotFound, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unknown mediums must not reach the network")
	}))
	defer server.Close()

	if _, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, []string{"Speed Duel"}); !errors.Is(err, ErrUnknownSetMedium) {
		t.Fatalf("expected ErrUnknownSetMedium, got %v", err)
	}
}

func TestFetchMediawikiLanguages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"query": {"results": {
			"Ancient Sanctuary": {"fullurl": "https://yugipedia.com/wiki/Ancient_Sanctuary", "namespace": 0,
			 "printouts": {
				"Medium": ["OCG", "TCG", "Official"],
				"English release date": [{"timestamp": "1086048000", "raw": "1/2004/6/1"}],
				"French release date": [{"timestamp": "1086048000", "raw": "1/2004/6/1"}],
				"German release date": [{"timestamp": "1083369600", "raw": "1/2004/5/1"}],
				"Japanese release date": [],
				"Korean release date": [{"timestamp": "1125100800", "raw": "1/2005/8/27"}]
			}}
		}}}`)
	}))
	defer server.Close()

	sets, err := fetchMediawikiSets(context.Background(), server.Client(), server.URL, nil)
	if err != nil {
		t.Fatalf("fetch mediawiki sets: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("sets = %+v", sets)
	}

	want := mediawikiSetResponse{
		URL:     "https://yugipedia.com/wiki/Ancient_Sanctuary",
		Name:    "Ancient Sanctuary",
		Mediums: []string{"OCG", "TCG"},
		Release: "2004-05-01",
		Languages: []mediawikiSetLanguage{
			{Lang: catalog.LangEN, Release: "2004-06-01"},
			{Lang: catalog.LangFR, Release: "2004-06-01"},
			{Lang: catalog.LangDE, Release: "2004-05-01"},
			{Lang: catalog.LangKR, Release: "2005-08-27"},
		},
	}
	if !reflect.DeepEqual(sets[0], want) {
		t.Fatalf("set = %+v, want %+v", sets[0], want)
	}
}

func TestMediawikiReleases(t *testing.T) {
	sets := []mediawikiSetResponse{
		{
			URL:     "https://yugipedia.com/wiki/Ancient_Sanctuary",
			Name:    "Ancient Sanctuary",
			Release: "2004-05-01",
			Languages: []mediawikiSetLanguage{
				{Lang: catalog.LangEN, Release: "2004-06-01"},
				{Lang: catalog.LangDE, Release: "2004-05-01"},
			},
		},
		{
			URL:     "https://yugipedia.com/wiki/No_Dates",
			Name:    "No Dates",
			Release: "2019-10-10",
		},
	}

	want := []Release{
		{
			Name:      "Ancient Sanctuary",
			URL:       "https://yugipedia.com/wiki/Ancient_Sanctuary",
			ReleaseAt: time.Date(2004, time.June, 1, 0, 0, 0, 0, time.UTC),
			Lang:      catalog.LangEN,
			Brand:     catalog.BrandYGO,
		},
		{
			Name:      "Ancient Sanctuary",
			URL:       "https://yugipedia.com/wiki/Ancient_Sanctuary",
			ReleaseAt: time.Date(2004, time.May, 1, 0, 0, 0, 0, time.UTC),
			Lang:      catalog.LangDE,
			Brand:     catalog.BrandYGO,
		},
		{
			Name:      "No Dates",
			URL:       "https://yugipedia.com/wiki/No_Dates",
			ReleaseAt: time.Date(2019, time.October, 10, 0, 0, 0, 0, time.UTC),
			Brand:     catalog.BrandYGO,
		},
	}

	if got := mediawikiReleases(sets); !reflect.DeepEqual(got, want) {
		t.Fatalf("releases = %+v, want %+v", got, want)
	}
}

func TestFetchMediawikiAllSetsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx := context.Background()

	sets, err := fetchMediawikiAllSets(ctx)
	if err != nil {
		t.Fatalf("fetch mediawiki all sets: %v", err)
	}
	if len(sets) < 5000 {
		t.Fatalf("expected the full set index, got %d entries", len(sets))
	}

	seen := make(map[string]bool, len(sets))
	for _, set := range sets {
		if set.Name == "" || !strings.HasPrefix(set.URL, yugipediaBaseURL+"/wiki/") {
			t.Fatalf("incomplete set: %+v", set)
		}
		if seen[set.URL] {
			t.Fatalf("duplicated set URL: %s", set.URL)
		}
		seen[set.URL] = true
	}

	if !seen[yugipediaBaseURL+"/wiki/Ancient_Sanctuary"] {
		t.Error("expected Ancient Sanctuary in the set index")
	}

	filtered, err := fetchMediawikiAllSets(ctx, MediumTCG, MediumOCG, MediumRush)
	if err != nil {
		t.Fatalf("fetch mediawiki sets by medium: %v", err)
	}
	if len(filtered) < 3000 {
		t.Fatalf("expected the TCG/OCG/Rush index, got %d entries", len(filtered))
	}
	if len(filtered) >= len(sets) {
		t.Fatalf("filtered index (%d) should be smaller than the full one (%d)", len(filtered), len(sets))
	}

	byName := make(map[string]string, len(filtered))
	allowed := map[string]bool{MediumTCG: true, MediumOCG: true, "Yu-Gi-Oh! Rush Duel": true}
	for _, set := range filtered {
		byName[set.Name] = set.URL

		matched := false
		for _, medium := range set.Mediums {
			matched = matched || allowed[medium]
		}
		if !matched {
			t.Errorf("set %s has no TCG/OCG/Rush medium: %v", set.Name, set.Mediums)
		}
	}

	// Duel Devastator is TCG but has no medium category on the wiki, so it
	// only shows up through the Semantic MediaWiki property.
	for _, name := range []string{"Ancient Sanctuary", "Duel Devastator", "Vol.1", "Over Rush Pack"} {
		if byName[name] == "" {
			t.Errorf("expected %s in the filtered set index", name)
		}
	}

	// Video game and DDM sets must be filtered out.
	for _, name := range []string{"Absolute Powerforce (World Championship 2010)", "All at Random (World Championship 2006)"} {
		if byName[name] != "" {
			t.Errorf("did not expect %s in the filtered set index", name)
		}
	}
}
