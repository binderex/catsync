// Package ygo provides the Yu-Gi-Oh! catalog provider. It reads set releases
// from Yugipedia, builds catalog sets and prints, and resolves card ItemInfo
// from YGOPRODeck or from the card page itself.
package ygo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"

	"github.com/binderex/catsync/internal/catalog"
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

	mu            sync.Mutex
	catalog       []ygoproCard
	catalogByName map[string]*ygoproCard
	catalogErr    error
	catalogLoaded bool
}

// NewYGOCatalogProvider creates a provider using the public YGOPRODeck and
// Yugipedia endpoints.
func NewYGOCatalogProvider() *Provider {
	return &Provider{
		client: &http.Client{
			Timeout:   clientTimeout,
			Transport: userAgentTransport{userAgent: browserUserAgent, base: http.DefaultTransport},
		},
	}
}

// Release is one set release from Yugipedia. Lang is empty when the source
// does not distinguish languages, and Medium is TCG, OCG or Rush Duel when
// known.
type Release struct {
	Name      string
	URL       string
	ReleaseAt time.Time
	Lang      catalog.Lang
	Medium    string
	Brand     catalog.Brand
}

// GetAllReleases fetches every set in Category:All sets and returns the sets
// with their card prints. mediums filter the set index (TCG, OCG, Rush Duel).
//
// When resume is false the previous checkpoint is discarded. When it is true
// the work continues from ygo-releases-state.json. The checkpoint is written
// when the run finishes or its context is cancelled.
func (p *Provider) GetAllReleases(ctx context.Context, resume bool, mediums ...string) (sets []catalog.Set, err error) {
	if resume {
		checkpoint, found, loadErr := loadReleasesCheckpoint(releasesStatePath)
		if loadErr != nil {
			return nil, loadErr
		}
		if found && checkpoint.Complete {
			return checkpointSets(p, checkpoint)
		}
		if found && len(checkpoint.Releases) > 0 {
			scrape, stateErr := newReleaseScrapeFromCheckpoint(p, checkpoint)
			if stateErr != nil {
				return nil, stateErr
			}

			runErr := scrape.run(ctx)
			sets = scrape.sets()
			saveErr := saveReleasesCheckpoint(
				releasesStatePath,
				scrape.snapshot(runErr == nil && ctx.Err() == nil && scrape.isComplete()),
			)
			if saveErr != nil {
				return sets, fmt.Errorf("save releases checkpoint: %w", saveErr)
			}

			return sets, runErr
		}
	} else if removeErr := os.Remove(releasesStatePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("remove old releases checkpoint: %w", removeErr)
	}

	releases, err := p.fetchYugupediaAllSet(ctx, mediums...)
	if err != nil {
		return nil, err
	}

	scrape, err := newReleaseScrape(p, releases)
	if err != nil {
		return nil, err
	}

	runErr := scrape.run(ctx)
	sets = scrape.sets()

	saveErr := saveReleasesCheckpoint(
		releasesStatePath,
		scrape.snapshot(runErr == nil && ctx.Err() == nil && scrape.isComplete()),
	)
	if saveErr != nil {
		return sets, fmt.Errorf("save releases checkpoint: %w", saveErr)
	}

	return sets, runErr
}

func checkpointSets(provider *Provider, checkpoint releasesCheckpoint) ([]catalog.Set, error) {
	scrape, err := newReleaseScrapeFromCheckpoint(provider, checkpoint)
	if err != nil {
		return nil, err
	}
	return scrape.sets(), nil
}

// GetNewReleases fetches the sets announced on the Yugipedia main page and
// returns them with their card prints.
func (p *Provider) GetNewReleases(ctx context.Context, mediums ...string) ([]catalog.Set, error) {
	releases, err := scrapeNewReleases(ctx)
	if err != nil {
		return nil, err
	}

	releases, err = filterReleases(releases, mediums...)
	if err != nil {
		return nil, err
	}

	return p.scrapeReleases(ctx, releases)
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
	case "rush", "rush duel", "yu-gi-oh! rush duel":
		return MediumRush, true
	default:
		return "", false
	}
}

// scrapeReleases is kept for callers that do not need checkpointing.
func (p *Provider) scrapeReleases(ctx context.Context, releases []Release) ([]catalog.Set, error) {
	scrape, err := newReleaseScrape(p, releases)
	if err != nil {
		return nil, err
	}

	runErr := scrape.run(ctx)

	return scrape.sets(), runErr
}

// applyPrintImageFallback uses the shared ItemInfo image or the set image when
// a print did not get a gallery image.
func applyPrintImageFallback(sets []catalog.Set) {
	for setIndex := range sets {
		set := &sets[setIndex]
		for printIndex := range set.CardsPrinted {
			print := &set.CardsPrinted[printIndex]
			if print.Image.URLSmall != "" || print.Image.URLLarge != "" {
				continue
			}
			if print.ItemInfo != nil && len(print.ItemInfo.Images) > 0 {
				print.Image = print.ItemInfo.Images[0]
				continue
			}
			if set.Image.URLSmall != "" || set.Image.URLLarge != "" {
				print.Image = set.Image
			}
		}
	}
}

// scrapeNewReleases extracts and filters the TCG/OCG set announcements.
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
			if !strings.EqualFold(heading, "TCG") && !strings.EqualFold(heading, "OCG") {
				return
			}

			medium := MediumTCG
			if strings.EqualFold(heading, "OCG") {
				medium = MediumOCG
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
						Brand:     catalog.BrandYGO,
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
