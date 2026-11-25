package ygo

import (
	"context"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"

	"github.com/binderex/catsync/internal/domain"
	"github.com/binderex/catsync/internal/utility/retrier"
)

var crawlReportPath = filepath.Join("data", "ygo-crawl-report.json")

// releaseScrape is the resumable state of one release crawl. Sets and prints
// are kept as pointers while the crawl is running; they are converted to
// index-based references when the checkpoint is written.
type releaseScrape struct {
	provider *Provider
	releases []Release

	states       []*setState
	stateByPrint map[*domain.Item]*setState

	printsByCard map[string][]*domain.Item
	galleryRefs  map[string][]*domain.Item

	cardResults    map[string]cardScrapeResult
	galleryResults map[string][]gallerySection

	listPageTargets map[string]*setListPage

	processed map[string]bool
	pages     map[string]domain.URLState

	// resumable is true when the scrape was rebuilt from a checkpoint and
	// therefore already holds the parsed results of the completed URLs.
	resumable bool

	mu sync.Mutex
}

func newReleaseScrape(provider *Provider, releases []Release) (*releaseScrape, error) {
	// Callers already filtered the releases by medium; filtering again here
	// would drop the Rush Duel releases requested with -rush-duel.
	scrape := newEmptyReleaseScrape(provider, groupReleases(releases))
	return scrape, nil
}

func newEmptyReleaseScrape(provider *Provider, releases []Release) *releaseScrape {
	return &releaseScrape{
		provider:        provider,
		releases:        releases,
		stateByPrint:    make(map[*domain.Item]*setState),
		printsByCard:    make(map[string][]*domain.Item),
		galleryRefs:     make(map[string][]*domain.Item),
		cardResults:     make(map[string]cardScrapeResult),
		galleryResults:  make(map[string][]gallerySection),
		listPageTargets: make(map[string]*setListPage),
		processed:       make(map[string]bool),
		pages:           make(map[string]domain.URLState),
	}
}

func (s *releaseScrape) items() []domain.Item {
	return materializeItems(s.states)
}

func (s *releaseScrape) isComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.printsByCard) > 0 || len(s.galleryRefs) > 0 {
		return false
	}

	for _, release := range s.releases {
		if !s.processed[canonicalPageURL(release.URL)] {
			return false
		}
	}
	for key := range s.listPageTargets {
		if !s.processed[key] {
			return false
		}
	}

	return true
}

func (s *releaseScrape) run(ctx context.Context, opts domain.ProviderOptions) error {
	if len(s.releases) == 0 {
		s.writeReport(map[string]domain.URLState{}, map[string]bool{}, true)
		return nil
	}

	// Loading the full YGOPRODeck catalog once avoids one request per card.
	// A failure is not fatal: card pages are used as the fallback source.
	if _, err := s.provider.ygoproCatalog(ctx); err != nil {
		log.Printf("load ygoprodeck catalog: %v", err)
	}

	collector := colly.NewCollector(
		colly.AllowedDomains("yugipedia.com", "www.yugipedia.com"),
		colly.UserAgent(browserUserAgent),
	)
	collector.SetRequestTimeout(clientTimeout)

	queueOptions := []retrier.Option{
		retrier.WithProvider("ygo"),
		retrier.WithMode(opts.Mode),
	}
	if opts.ResumeState != nil {
		queueOptions = append(queueOptions, retrier.WithRunID(opts.ResumeState.ID))
	}
	if s.provider.persistence != nil {
		queueOptions = append(queueOptions, retrier.WithObserver(s.provider.persistence))
	}
	queue := retrier.NewColly(collector, queueOptions...)
	setFilter := retrier.RejectSelector(".noarticletext")
	cardFilter := retrier.RejectSelector(".noarticletext", ".set-lists--navbar")
	galleryFilter := retrier.RejectSelector(".noarticletext")

	releasesByURL := make(map[string]Release, len(s.releases))
	setURLs := make(map[string]bool, len(s.releases))
	for _, release := range s.releases {
		key := canonicalPageURL(release.URL)
		releasesByURL[key] = release
		setURLs[key] = true
	}

	s.mu.Lock()
	s.reconcileCachedResultsLocked()
	s.mu.Unlock()

	collector.OnHTML("body", func(element *colly.HTMLElement) {
		if element.Response == nil || element.Response.StatusCode < 200 || element.Response.StatusCode >= 300 {
			return
		}

		pageURL := element.Request.URL.String()
		pageKey := canonicalPageURL(pageURL)
		body := element.Response.Body

		switch {
		case setURLs[pageKey]:
			release := releasesByURL[pageKey]
			setStates, printsByURL, listPages, err := scrapeSet(pageURL, body, release)
			if err != nil {
				log.Printf("set page %s: %v", pageURL, err)
				return
			}

			s.mu.Lock()
			s.states = append(s.states, setStates...)
			for _, state := range setStates {
				for _, print := range state.Prints {
					s.stateByPrint[print] = state
				}
			}
			for _, listPage := range listPages {
				s.listPageTargets[canonicalPageURL(listPage.URL)] = listPage
			}
			s.processed[pageKey] = true
			s.mu.Unlock()

			for cardURL, refs := range printsByURL {
				cardKey := canonicalPageURL(cardURL)
				s.mu.Lock()
				s.registerCardRefsLocked(cardKey, refs)
				s.mu.Unlock()
				s.addCardURL(queue, cardURL, cardFilter)
			}
			for _, listPage := range listPages {
				if !s.isProcessed(canonicalPageURL(listPage.URL)) {
					queue.Add(listPage.URL, setFilter)
				}
			}

		case s.listPage(pageKey) != nil:
			target := s.listPage(pageKey)
			printsByURL, err := scrapeSetListPage(pageURL, body, target)
			if err != nil {
				log.Printf("set list page %s: %v", pageURL, err)
				return
			}

			s.mu.Lock()
			for _, state := range target.States {
				for _, print := range state.Prints {
					s.stateByPrint[print] = state
				}
			}
			s.processed[pageKey] = true
			s.mu.Unlock()

			for cardURL, refs := range printsByURL {
				cardKey := canonicalPageURL(cardURL)
				s.mu.Lock()
				s.registerCardRefsLocked(cardKey, refs)
				s.mu.Unlock()
				s.addCardURL(queue, cardURL, cardFilter)
			}

		case strings.Contains(pageKey, "/wiki/Card_Gallery:"):
			gallery := scrapeGallery(body)

			s.mu.Lock()
			s.galleryResults[pageKey] = gallery
			refs := s.galleryRefs[pageKey]
			delete(s.galleryRefs, pageKey)
			s.applyGalleryLocked(gallery, refs)
			s.processed[pageKey] = true
			s.mu.Unlock()

		default:
			result, err := s.provider.scrapeCard(ctx, pageURL, body, nil)
			if err != nil {
				log.Printf("card page %s: %v", pageURL, err)
				return
			}

			// The release flow uses the prints built from the set list; the
			// card-page prints are only needed by direct scrapeCard callers.
			result.Prints = nil

			s.mu.Lock()
			s.cardResults[pageKey] = result
			refs := s.printsByCard[pageKey]
			delete(s.printsByCard, pageKey)
			s.applyCardLocked(result, refs)
			if result.GalleryURL != "" {
				galleryKey := canonicalPageURL(result.GalleryURL)
				s.galleryRefs[galleryKey] = append(s.galleryRefs[galleryKey], refs...)
				if gallery, ok := s.galleryResults[galleryKey]; ok {
					s.applyGalleryLocked(gallery, refs)
				}
			}
			s.processed[pageKey] = true
			s.mu.Unlock()

			if result.GalleryURL != "" {
				s.addGalleryURL(queue, result.GalleryURL, galleryFilter)
			}
		}
	})

	s.mu.Lock()
	releases := append([]Release(nil), s.releases...)
	listPages := make(map[string]*setListPage, len(s.listPageTargets))
	for key, target := range s.listPageTargets {
		listPages[key] = target
	}
	cardURLs := make([]string, 0, len(s.printsByCard))
	for key := range s.printsByCard {
		cardURLs = append(cardURLs, key)
	}
	galleryURLs := make([]string, 0, len(s.galleryRefs))
	for key := range s.galleryRefs {
		galleryURLs = append(galleryURLs, key)
	}
	s.mu.Unlock()

	resolveFilter := func(state domain.URLState) domain.Filter {
		key := canonicalPageURL(state.URL)
		switch {
		case setURLs[key]:
			return setFilter
		case strings.Contains(key, "/wiki/Set_Card_Lists:"):
			return setFilter
		case strings.Contains(key, "/wiki/Card_Gallery:"):
			return galleryFilter
		default:
			return cardFilter
		}
	}

	if opts.ResumeState != nil && s.resumable {
		queue.Restore(opts.ResumeState.States, opts.ResumeState.Order, resolveFilter)
	} else {
		for _, release := range releases {
			if !s.isProcessed(canonicalPageURL(release.URL)) {
				queue.Add(release.URL, setFilter)
			}
		}
		for key, target := range listPages {
			if !s.isProcessed(key) {
				queue.Add(target.URL, setFilter)
			}
		}
		for _, key := range cardURLs {
			if !s.isProcessed(key) {
				if _, cached := s.cardResult(key); !cached {
					queue.Add(key, cardFilter)
				}
			}
		}
		for _, key := range galleryURLs {
			if !s.isProcessed(key) {
				if _, cached := s.galleryResult(key); !cached {
					queue.Add(key, galleryFilter)
				}
			}
		}
	}

	runErr := queue.Run(ctx)

	states := queue.States()
	s.mu.Lock()
	s.pages = states
	s.mu.Unlock()

	logScrapeSummary(states, setURLs)
	s.writeReport(states, setURLs, runErr == nil && ctx.Err() == nil && s.isComplete())

	return runErr
}

type pageFailure struct {
	URL      string `json:"url"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Attempts int    `json:"attempts"`
}

type crawlReport struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Complete    bool          `json:"complete"`
	Total       int           `json:"total"`
	Completed   int           `json:"completed"`
	Failed      int           `json:"failed"`
	NotFound    int           `json:"not_found"`
	Invalid     int           `json:"invalid"`
	Pending     int           `json:"pending"`
	Sets        int           `json:"sets"`
	Prints      int           `json:"prints"`
	Failures    []pageFailure `json:"failures"`
}

func (s *releaseScrape) writeReport(
	states map[string]domain.URLState,
	setURLs map[string]bool,
	complete bool,
) {
	completed, failed, notFound, invalid, pending := countStates(states)

	s.mu.Lock()
	sets := len(s.states)
	prints := 0
	for _, state := range s.states {
		prints += len(state.Prints)
	}
	s.mu.Unlock()

	report := crawlReport{
		GeneratedAt: time.Now().UTC(),
		Complete:    complete,
		Total:       len(states),
		Completed:   completed,
		Failed:      failed,
		NotFound:    notFound,
		Invalid:     invalid,
		Pending:     pending,
		Sets:        sets,
		Prints:      prints,
		Failures:    pageFailures(states, setURLs),
	}

	if err := writeJSONAtomic(crawlReportPath, report); err != nil {
		log.Printf("ygo report: %v", err)
		return
	}

	log.Printf("ygo: crawl report written to %s", crawlReportPath)
}

func countStates(states map[string]domain.URLState) (completed, failed, notFound, invalid, pending int) {
	for _, state := range states {
		switch {
		case state.Completed:
			completed++
		case state.Failed:
			failed++
		case state.NotFound:
			notFound++
		case state.Invalid:
			invalid++
		default:
			pending++
		}
	}
	return completed, failed, notFound, invalid, pending
}

func logScrapeSummary(states map[string]domain.URLState, setURLs map[string]bool) {
	completed, failed, notFound, invalid, pending := countStates(states)

	log.Printf(
		"ygo summary: total=%d completed=%d failed=%d not_found=%d invalid=%d pending=%d",
		len(states),
		completed,
		failed,
		notFound,
		invalid,
		pending,
	)

	for _, failure := range pageFailures(states, setURLs) {
		log.Printf(
			"ygo error: kind=%s attempts=%d url=%s reason=%s",
			failure.Kind,
			failure.Attempts,
			failure.URL,
			failure.Reason,
		)
	}
}

func pageFailures(states map[string]domain.URLState, setURLs map[string]bool) []pageFailure {
	failures := make([]pageFailure, 0)
	for url, state := range states {
		var reason string
		switch {
		case state.Failed:
			reason = state.Error
		case state.NotFound:
			reason = "HTTP 404 not found"
		case state.Invalid:
			reason = "rejected by page filter"
		default:
			continue
		}

		failures = append(failures, pageFailure{
			URL:      url,
			Kind:     pageKind(url, setURLs),
			Reason:   reason,
			Attempts: state.Attempts,
		})
	}

	sort.Slice(failures, func(i, j int) bool {
		if failures[i].Kind == failures[j].Kind {
			return failures[i].URL < failures[j].URL
		}
		return failures[i].Kind < failures[j].Kind
	})

	return failures
}

func pageKind(url string, setURLs map[string]bool) string {
	key := canonicalPageURL(url)
	switch {
	case setURLs[key]:
		return "set"
	case strings.Contains(key, "/wiki/Set_Card_Lists:"):
		return "set-list"
	case strings.Contains(key, "/wiki/Card_Gallery:"):
		return "gallery"
	default:
		return "card"
	}
}

func (s *releaseScrape) reconcileCachedResultsLocked() {
	for cardURL, refs := range s.printsByCard {
		result, ok := s.cardResults[cardURL]
		if !ok {
			continue
		}

		s.applyCardLocked(result, refs)
		delete(s.printsByCard, cardURL)

		if result.GalleryURL == "" {
			continue
		}

		galleryKey := canonicalPageURL(result.GalleryURL)
		if gallery, ok := s.galleryResults[galleryKey]; ok {
			s.applyGalleryLocked(gallery, refs)
			continue
		}
		s.galleryRefs[galleryKey] = append(s.galleryRefs[galleryKey], refs...)
	}

	for galleryURL, refs := range s.galleryRefs {
		if gallery, ok := s.galleryResults[galleryURL]; ok {
			s.applyGalleryLocked(gallery, refs)
			delete(s.galleryRefs, galleryURL)
		}
	}
}

func (s *releaseScrape) registerCardRefsLocked(cardURLKey string, refs []*domain.Item) {
	if cardURLKey == "" || len(refs) == 0 {
		return
	}

	result, processed := s.cardResults[cardURLKey]
	if !processed {
		s.printsByCard[cardURLKey] = append(s.printsByCard[cardURLKey], refs...)
		return
	}

	s.applyCardLocked(result, refs)
	if result.GalleryURL == "" {
		return
	}

	galleryKey := canonicalPageURL(result.GalleryURL)
	if gallery, ok := s.galleryResults[galleryKey]; ok {
		s.applyGalleryLocked(gallery, refs)
		return
	}
	s.galleryRefs[galleryKey] = append(s.galleryRefs[galleryKey], refs...)
}

func (s *releaseScrape) applyCardLocked(result cardScrapeResult, refs []*domain.Item) {
	if result.Info == nil {
		return
	}
	for _, print := range refs {
		if print == nil {
			continue
		}
		applyCardInfo(print, result.Info)

		if translation, ok := result.Translations[print.Lang]; ok {
			// The English name from the set list is authoritative. Card
			// pages sometimes use a different capitalization (for example
			// "Ojama clogging 1" for a strategy card).
			if print.Lang != domain.LangEN && translation.Name != "" {
				print.Name = translation.Name
			}
			if translation.Description != "" {
				print.Description = translation.Description
			}
		}

		if print.Image.URLSmall == "" && print.Image.URLLarge == "" {
			if result.Image.URLSmall != "" || result.Image.URLLarge != "" {
				print.Image = result.Image
			} else if images := result.Info.PropertyImages(); len(images) > 0 {
				print.Image = images[0]
			}
		}
	}
}

func (s *releaseScrape) applyGalleryLocked(gallery []gallerySection, refs []*domain.Item) {
	for _, print := range refs {
		if print == nil {
			continue
		}
		applyGalleryPrint(print, gallery)

		state := s.stateByPrint[print]
		if state == nil {
			continue
		}
		for _, variant := range galleryVariants(*print, gallery) {
			variantCopy := variant
			printCopy := &variantCopy
			state.Prints = append(state.Prints, printCopy)
			s.stateByPrint[printCopy] = state
		}
	}
}

func (s *releaseScrape) addCardURL(queue *retrier.Retrier, url string, filter domain.Filter) {
	key := canonicalPageURL(url)
	if s.isProcessed(key) {
		return
	}
	if _, cached := s.cardResult(key); cached {
		return
	}
	queue.Add(url, filter)
}

func (s *releaseScrape) addGalleryURL(queue *retrier.Retrier, url string, filter domain.Filter) {
	key := canonicalPageURL(url)
	if s.isProcessed(key) {
		return
	}
	if _, cached := s.galleryResult(key); cached {
		return
	}
	queue.Add(url, filter)
}

func (s *releaseScrape) isProcessed(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processed[key]
}

func (s *releaseScrape) listPage(key string) *setListPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listPageTargets[key]
}

func (s *releaseScrape) cardResult(key string) (cardScrapeResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.cardResults[key]
	return result, ok
}

func (s *releaseScrape) galleryResult(key string) ([]gallerySection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.galleryResults[key]
	return result, ok
}
