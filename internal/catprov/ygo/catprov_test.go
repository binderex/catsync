package ygo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/catalog"
)

func TestParseNewReleaseDate(t *testing.T) {
	year := time.Now().Year()

	tests := []struct {
		value string
		want  time.Time
	}{
		{"November 12", time.Date(year, time.November, 12, 0, 0, 0, 0, time.UTC)},
		{"January 28, 2027", time.Date(2027, time.January, 28, 0, 0, 0, 0, time.UTC)},
		{"December", time.Date(year, time.December, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, test := range tests {
		if got := parseNewReleaseDate(test.value); !got.Equal(test.want) {
			t.Errorf("parseNewReleaseDate(%q) = %v, want %v", test.value, got, test.want)
		}
	}

	if got := parseNewReleaseDate("not a date"); !got.IsZero() {
		t.Errorf("invalid date = %v, want zero", got)
	}
}

func TestFilterReleases(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	releases := []Release{
		{Name: "TCG", URL: "https://example.com/tcg", ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Medium: MediumTCG},
		{Name: "OCG", URL: "https://example.com/ocg", ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Medium: MediumOCG},
		{Name: "Future", URL: "https://example.com/future", ReleaseAt: time.Date(2027, time.June, 1, 0, 0, 0, 0, time.UTC), Medium: MediumTCG},
		{Name: "No URL", ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}

	filtered, err := filterReleases(releases, MediumTCG)
	if err != nil {
		t.Fatalf("filter releases: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Name != "TCG" {
		t.Fatalf("filtered = %+v", filtered)
	}

	if _, err := filterReleases(releases, "unknown"); !errors.Is(err, ErrUnknownSetMedium) {
		t.Fatalf("expected ErrUnknownSetMedium, got %v", err)
	}
}

func TestGroupReleases(t *testing.T) {
	earlier := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.AddDate(0, 1, 0)

	grouped := groupReleases([]Release{
		{Name: "Set", URL: "https://example.com/set", ReleaseAt: later, Lang: catalog.LangEN, Medium: MediumTCG, Brand: catalog.BrandYGO},
		{Name: "Set", URL: "https://example.com/set", ReleaseAt: earlier, Lang: catalog.LangFR, Medium: MediumTCG, Brand: catalog.BrandYGO},
	})

	if len(grouped) != 1 {
		t.Fatalf("grouped = %+v", grouped)
	}
	if !grouped[0].ReleaseAt.Equal(earlier) {
		t.Errorf("ReleaseAt = %v, want %v", grouped[0].ReleaseAt, earlier)
	}
	if grouped[0].URL != "https://example.com/set" {
		t.Errorf("URL = %q", grouped[0].URL)
	}
}

func TestFetchYGOPRODECKCards(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	previousMax := max
	t.Cleanup(func() {
		max = previousMax
	})

	provider := NewYGOCatalogProvider()
	ctx := context.Background()

	items, err := provider.fetchYGOPRODECKCards(ctx, "ojama")
	if err != nil {
		t.Fatalf("fetch ojama cards: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected ojama cards, got none")
	}

	card, err := findYGOPROCard("Ojama Yellow", items)
	if err != nil {
		t.Fatal("expected Ojama Yellow in results")
	}
	if card.Name != "Ojama Yellow" {
		t.Errorf("card name = %q", card.Name)
	}
}

func TestScrapeNewReleases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	previousMax := max
	t.Cleanup(func() {
		max = previousMax
	})

	releases, err := scrapeNewReleases(context.Background())
	if err != nil {
		t.Fatalf("scrape new releases: %v", err)
	}
	if len(releases) == 0 {
		t.Fatal("expected new releases, got none")
	}

	for _, release := range releases {
		if release.Name == "" || release.URL == "" || release.ReleaseAt.IsZero() {
			t.Errorf("incomplete release: %+v", release)
		}
	}

	max = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	releases, err = scrapeNewReleases(context.Background())
	if err != nil {
		t.Fatalf("scrape new releases with past max: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("expected no releases with past max, got %d", len(releases))
	}
}

func TestGetNewReleasesLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	provider := NewYGOCatalogProvider()
	// Keep the live test focused on the crawl: card names will be queried
	// individually instead of downloading the full YGOPRODeck catalog.
	provider.catalogLoaded = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	releases, err := scrapeNewReleases(ctx)
	if err != nil {
		t.Fatalf("scrape new releases: %v", err)
	}
	if len(releases) == 0 {
		t.Skip("no releases to visit")
	}

	sets, err := provider.scrapeReleases(ctx, releases[:1])
	if err != nil {
		t.Fatalf("scrape releases: %v", err)
	}
	if len(sets) == 0 {
		t.Fatal("expected sets")
	}

	var foundSet, foundPrint bool
	for _, set := range sets {
		if set.ItemInfo == nil {
			t.Errorf("set %q has no ItemInfo", set.Name)
		}
		if len(set.CardsPrinted) > 0 {
			foundSet = true
			for _, print := range set.CardsPrinted {
				if print.ItemInfo != nil {
					foundPrint = true
					break
				}
			}
		}
	}

	if !foundSet {
		t.Error("expected sets with prints")
	}
	if !foundPrint {
		t.Error("expected at least one print with ItemInfo")
	}
}
