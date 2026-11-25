// Package ygo provides the Yu-Gi-Oh! catalog provider. It reads set releases
// from Yugipedia, builds catalog sets and prints, and resolves card ItemInfo
// from YGOPRODeck or from the card page itself.
package ygo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
)

const (
	yugipediaBaseURL  = "https://yugipedia.com"
	ygoprodeckBaseURL = "https://db.ygoprodeck.com/api/v7"
	clientTimeout     = 30 * time.Second
	browserUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

var (
	// ErrClientTimeout is returned when a YGOPRODeck request times out.
	ErrClientTimeout = errors.New("ygoprodeck client timeout")

	// ErrCardsNotFound is returned when the API has no cards to return.
	ErrCardsNotFound = errors.New("ygoprodeck cards not found")

	// ErrUnexpectedStatus is returned for an unexpected HTTP status.
	ErrUnexpectedStatus = errors.New("ygoprodeck unexpected status")

	// ErrNewsNotFound is returned when the Yugipedia news section is missing.
	ErrNewsNotFound = errors.New("yugipedia news section not found")

	// ErrSetsNotFound is returned when the MediaWiki API lists no set pages.
	ErrSetsNotFound = errors.New("yugipedia sets not found")

	// ErrUnknownSetMedium is returned when a medium is not recognized.
	ErrUnknownSetMedium = errors.New("yugipedia unknown set medium")

	// ErrMediawikiUnexpectedStatus is returned for an unexpected MediaWiki
	// HTTP status.
	ErrMediawikiUnexpectedStatus = errors.New("yugipedia mediawiki unexpected status")
)

// Provider retrieves Yu-Gi-Oh! releases and builds catalog sets from them.
type Provider struct {
	client *http.Client

	persistence ports.RetrierPersistence

	mu            sync.Mutex
	catalog       []ygoproCard
	catalogByName map[string]*ygoproCard
	catalogErr    error
	catalogLoaded bool
}

// NewYGOCatalogProvider creates a provider using the public YGOPRODeck and
// Yugipedia endpoints. persistence is optional; when set, the crawl progress is
// saved and later runs resume automatically.
func NewYGOCatalogProvider(persistence ...ports.RetrierPersistence) *Provider {
	provider := &Provider{
		client: &http.Client{
			Timeout:   clientTimeout,
			Transport: userAgentTransport{userAgent: browserUserAgent, base: http.DefaultTransport},
		},
	}
	if len(persistence) > 0 {
		provider.persistence = persistence[0]
	}
	return provider
}

// Name identifies the provider in retrier persistence.
func (p *Provider) Name() string {
	return "ygo"
}

// Release is one set release from Yugipedia. Lang is empty when the source
// does not distinguish languages, and Medium is TCG or OCG.
type Release struct {
	Name      string
	URL       string
	ReleaseAt time.Time
	Lang      domain.Lang
	Medium    string
	Brand     domain.Brand
}

// GetAllReleases fetches every set in Category:All sets and returns them with
// their card prints as denormalized items. opts.Mediums filters the set index
// (TCG, OCG or Rush Duel; Rush Duel is opt-in).
//
// When opts.Resume is false the previous checkpoint is discarded, unless the
// caller passed an unfinished retrier run (opts.ResumeState) and a checkpoint
// exists: in that case the crawl resumes automatically. The checkpoint is
// written when the run finishes or its context is cancelled.
func (p *Provider) GetAllReleases(ctx context.Context, opts domain.ProviderOptions) (items []domain.Item, err error) {
	includeRushDuel := includesMedium(opts.Mediums, MediumRushDuel)

	var (
		checkpoint releasesCheckpoint
		found      bool
	)
	if opts.Resume && opts.ResumeState != nil && len(opts.ResumeState.ProviderData) > 0 {
		checkpoint, found, err = decodeReleasesCheckpoint(opts.ResumeState.ProviderData)
		if err != nil {
			return nil, err
		}
	}

	if found && checkpoint.Complete {
		return checkpointItems(p, checkpoint, includeRushDuel)
	}
	if found && len(checkpoint.Releases) > 0 {
		scrape, stateErr := newReleaseScrapeFromCheckpoint(p, checkpoint)
		if stateErr != nil {
			return nil, stateErr
		}

		runErr := scrape.run(ctx, opts)
		items = supportedItems(scrape.items(), includeRushDuel)
		saveErr := p.saveProviderData(
			ctx,
			opts.Mode,
			scrape.snapshot(runErr == nil && ctx.Err() == nil && scrape.isComplete()),
		)
		if saveErr != nil {
			return items, fmt.Errorf("save provider checkpoint: %w", saveErr)
		}

		return items, runErr
	}

	mediums := opts.Mediums
	if len(mediums) == 0 {
		mediums = []string{MediumTCG, MediumOCG}
	}

	releases, err := p.fetchYugupediaAllSet(ctx, mediums...)
	if err != nil {
		return nil, err
	}

	scrape, err := newReleaseScrape(p, releases)
	if err != nil {
		return nil, err
	}

	runErr := scrape.run(ctx, opts)
	items = supportedItems(scrape.items(), includeRushDuel)

	saveErr := p.saveProviderData(
		ctx,
		opts.Mode,
		scrape.snapshot(runErr == nil && ctx.Err() == nil && scrape.isComplete()),
	)
	if saveErr != nil {
		return items, fmt.Errorf("save provider checkpoint: %w", saveErr)
	}

	return items, runErr
}

// saveProviderData stores the parsed crawl snapshot inside the active retrier
// run in data/retrier.json.
func (p *Provider) saveProviderData(
	ctx context.Context,
	mode string,
	checkpoint releasesCheckpoint,
) error {
	if p.persistence == nil {
		return nil
	}

	data, err := encodeReleasesCheckpoint(checkpoint)
	if err != nil {
		return err
	}

	return p.persistence.SaveProviderData(ctx, p.Name(), mode, data)
}

func checkpointItems(provider *Provider, checkpoint releasesCheckpoint, includeRushDuel bool) ([]domain.Item, error) {
	scrape, err := newReleaseScrapeFromCheckpoint(provider, checkpoint)
	if err != nil {
		return nil, err
	}
	return supportedItems(scrape.items(), includeRushDuel), nil
}

// supportedItems drops Rush Duel rows unless they were explicitly requested.
// Even then, only official English editions are kept; the catalog enables Rush
// Duel once an English release exists.
func supportedItems(items []domain.Item, includeRushDuel bool) []domain.Item {
	filtered := make([]domain.Item, 0, len(items))
	for _, item := range items {
		if item.Brand == domain.BrandYGORD {
			if !includeRushDuel || item.Lang != domain.LangEN {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func includesMedium(mediums []string, wanted string) bool {
	for _, medium := range mediums {
		if value, ok := normalizeMedium(medium); ok && value == wanted {
			return true
		}
	}
	return false
}

// GetNewReleases fetches the sets announced on the Yugipedia main page and
// returns them with their card prints.
func (p *Provider) GetNewReleases(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error) {
	releases, err := scrapeNewReleases(ctx)
	if err != nil {
		return nil, err
	}

	releases, err = filterReleases(releases, opts.Mediums...)
	if err != nil {
		return nil, err
	}

	return p.scrapeReleases(ctx, releases, opts)
}

func (p *Provider) fetchYugupediaAllSet(ctx context.Context, mediums ...string) ([]Release, error) {
	sets, err := fetchMediawikiSets(ctx, p.client, yugipediaBaseURL, mediums)
	if err != nil {
		return nil, err
	}

	return mediawikiReleases(sets), nil
}

func groupReleases(releases []Release) []Release {
	byURL := make(map[string]Release, len(releases))
	order := make([]string, 0, len(releases))

	for _, release := range releases {
		if release.URL == "" {
			continue
		}

		existing, ok := byURL[release.URL]
		if !ok {
			order = append(order, release.URL)
			byURL[release.URL] = release
			continue
		}

		if existing.ReleaseAt.IsZero() || (!release.ReleaseAt.IsZero() && release.ReleaseAt.Before(existing.ReleaseAt)) {
			existing.ReleaseAt = release.ReleaseAt
		}
		if existing.Brand == "" {
			existing.Brand = release.Brand
		}
		if existing.Medium == "" {
			existing.Medium = release.Medium
		}
		if existing.Name == "" {
			existing.Name = release.Name
		}
		byURL[release.URL] = existing
	}

	grouped := make([]Release, 0, len(order))
	for _, url := range order {
		grouped = append(grouped, byURL[url])
	}
	return grouped
}

func filterReleases(releases []Release, mediums ...string) ([]Release, error) {
	filters := make(map[string]bool, len(mediums))
	for _, medium := range mediums {
		value, ok := normalizeMedium(medium)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownSetMedium, medium)
		}
		filters[value] = true
	}
	if len(filters) == 0 {
		// Rush Duel stays opt-in until an official English release exists.
		filters[MediumTCG] = true
		filters[MediumOCG] = true
	}

	filtered := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.URL == "" || isTooEarly(release.ReleaseAt) {
			continue
		}
		if len(filters) > 0 && release.Medium != "" && !filters[release.Medium] {
			continue
		}
		filtered = append(filtered, release)
	}

	return filtered, nil
}

func normalizeMedium(medium string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(medium)) {
	case "tcg":
		return MediumTCG, true
	case "ocg":
		return MediumOCG, true
	case "rush duel", "rush", "ygord":
		return MediumRushDuel, true
	default:
		return "", false
	}
}

// scrapeReleases is kept for callers that do not need checkpointing.
func (p *Provider) scrapeReleases(
	ctx context.Context,
	releases []Release,
	opts domain.ProviderOptions,
) ([]domain.Item, error) {
	scrape, err := newReleaseScrape(p, releases)
	if err != nil {
		return nil, err
	}

	runErr := scrape.run(ctx, opts)

	return supportedItems(scrape.items(), includesMedium(opts.Mediums, MediumRushDuel)), runErr
}

// scrapeNewReleases extracts and filters the announced TCG, OCG and Rush Duel
// sets.
func scrapeNewReleases(ctx context.Context) ([]Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	collector := colly.NewCollector(
		colly.AllowedDomains("yugipedia.com", "www.yugipedia.com"),
		colly.UserAgent(browserUserAgent),
	)
	collector.SetRequestTimeout(clientTimeout)
	collector.OnRequest(func(request *colly.Request) {
		if ctx.Err() != nil {
			request.Abort()
		}
	})

	var found bool
	byURL := make(map[string]Release)

	collector.OnHTML(".main-page-element[data-grid-area='news'] .main-page-setslist", func(e *colly.HTMLElement) {
		if ctx.Err() != nil {
			return
		}
		found = true

		e.DOM.ChildrenFiltered("div").Each(func(_ int, section *goquery.Selection) {
			heading := strings.TrimSpace(section.Find("h3").First().Text())

			var medium string
			switch {
			case strings.EqualFold(heading, "TCG"):
				medium = MediumTCG
			case strings.EqualFold(heading, "OCG"):
				medium = MediumOCG
			case strings.EqualFold(heading, "Rush Duel"):
				medium = MediumRushDuel
			default:
				return
			}

			brand := domain.BrandYGO
			if medium == MediumRushDuel {
				brand = domain.BrandYGORD
			}

			section.Find("li").Each(func(_ int, item *goquery.Selection) {
				releaseText := strings.TrimSpace(item.Find("b").First().Text())
				if releaseText == "" {
					return
				}

				link := item.Find("i a").First()
				if link.Length() == 0 {
					link = item.Find("a").First()
				}

				href, exists := link.Attr("href")
				if !exists || href == "" {
					return
				}

				name := strings.TrimSpace(link.Text())
				if name == "" {
					return
				}

				releaseAt := parseNewReleaseDate(releaseText)
				if isTooEarly(releaseAt) {
					return
				}

				previous, exists := byURL[href]
				if !exists || releaseAt.Before(previous.ReleaseAt) {
					byURL[href] = Release{
						Name:      name,
						URL:       yugipediaBaseURL + href,
						ReleaseAt: releaseAt,
						Medium:    medium,
						Brand:     brand,
					}
				}
			})
		})
	})

	pageURL := fmt.Sprintf("%s/wiki/Yugipedia", yugipediaBaseURL)
	if err := collector.Visit(pageURL); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("visit yugipedia main page: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !found {
		return nil, ErrNewsNotFound
	}

	releases := make([]Release, 0, len(byURL))
	for _, release := range byURL {
		releases = append(releases, release)
	}

	sort.Slice(releases, func(i, j int) bool {
		if releases[i].ReleaseAt.Equal(releases[j].ReleaseAt) {
			return releases[i].Name < releases[j].Name
		}
		return releases[i].ReleaseAt.Before(releases[j].ReleaseAt)
	})

	return releases, nil
}
