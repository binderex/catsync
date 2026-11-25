package ygo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/domain"
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
		{Name: "Rush", URL: "https://example.com/rush", ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Medium: MediumRushDuel},
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

	// With no explicit medium, Rush Duel is excluded.
	defaulted, err := filterReleases(releases)
	if err != nil {
		t.Fatalf("filter default releases: %v", err)
	}
	if len(defaulted) != 2 {
		t.Fatalf("defaulted = %+v", defaulted)
	}

	rush, err := filterReleases(releases, MediumRushDuel)
	if err != nil {
		t.Fatalf("filter rush releases: %v", err)
	}
	if len(rush) != 1 || rush[0].Name != "Rush" {
		t.Fatalf("rush = %+v", rush)
	}

	if _, err := filterReleases(releases, "unknown"); !errors.Is(err, ErrUnknownSetMedium) {
		t.Fatalf("expected ErrUnknownSetMedium, got %v", err)
	}
}

func TestSupportedItemsRushDuelOptIn(t *testing.T) {
	items := []domain.Item{
		{Brand: domain.BrandYGO, Lang: domain.LangEN},
		{Brand: domain.BrandYGORD, Lang: domain.LangEN},
		{Brand: domain.BrandYGORD, Lang: domain.LangJP},
	}

	if got := supportedItems(items, false); len(got) != 1 {
		t.Fatalf("without rush duel = %+v", got)
	}

	withRush := supportedItems(items, true)
	if len(withRush) != 2 {
		t.Fatalf("with rush duel = %+v", withRush)
	}
	for _, item := range withRush {
		if item.Brand == domain.BrandYGORD && item.Lang != domain.LangEN {
			t.Errorf("non-English rush duel item = %+v", item)
		}
	}
}

func TestGroupReleases(t *testing.T) {
	earlier := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.AddDate(0, 1, 0)

	grouped := groupReleases([]Release{
		{Name: "Set", URL: "https://example.com/set", ReleaseAt: later, Lang: domain.LangEN, Medium: MediumTCG, Brand: domain.BrandYGO},
		{Name: "Set", URL: "https://example.com/set", ReleaseAt: earlier, Lang: domain.LangFR, Medium: MediumTCG, Brand: domain.BrandYGO},
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
	// individually instead of downloading the full YGOPRODeck domain.
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

	items, err := provider.scrapeReleases(ctx, releases[:1], domain.ProviderOptions{Mode: "new"})
	if err != nil {
		t.Fatalf("scrape releases: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected sets")
	}

	var foundSet, foundPrint bool
	for _, item := range items {
		switch item.Kind {
		case domain.KindSet:
			if item.Source == "" {
				t.Errorf("set %q has no source", item.Name)
			}
			foundSet = true
		case domain.KindCard:
			if item.Source != "" {
				foundPrint = true
			}
		}
	}

	if !foundSet {
		t.Error("expected sets with prints")
	}
	if !foundPrint {
		t.Error("expected at least one print with a source")
	}
}
