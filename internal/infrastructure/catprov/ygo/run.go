package ygo

import (
	"context"
	"log"
	"os"
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

	// pending holds the prints whose card page or gallery is not complete yet.
	// They live in the set state while the crawl runs, but the snapshot only
	// stores them as pending resume data.
	pending map[*domain.Item]bool

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
		pending:         make(map[*domain.Item]bool),
		processed:       make(map[string]bool),
		pages:           make(map[string]domain.URLState),
	}
}

func (s *releaseScrape) items() []domain.Item {
	s.mu.Lock()
	pending := s.pendingSetLocked()
	s.mu.Unlock()

	return materializeCompleteItems(s.states, pending)
}

func (s *releaseScrape) isComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.printsByCard) > 0 || len(s.galleryRefs) > 0 || len(s.pending) > 0 {
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
	log.Printf("ygo: loading ygoprodeck catalog")
	cards, err := s.provider.ygoproCatalog(ctx)
	if err != nil {
		log.Printf("ygo: load ygoprodeck catalog: %v", err)
	} else {
		log.Printf("ygo: loaded %d ygoprodeck cards", len(cards))
	}

	collector := colly.NewCollector(
		colly.AllowedDomains("yugipedia.com", "www.yugipedia.com"),
		colly.UserAgent(browserUserAgent),
	)
	collector.SetRequestTimeout(clientTimeout)

	queueOptions := []retrier.Option{
		retrier.WithProvider("ygo"),
		retrier.WithMode(opts.Mode),
		retrier.WithPresenter(retrier.NewConsolePresenter(os.Stdout)),
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

			referenced := referencedPrints(printsByURL)

			s.mu.Lock()
			s.states = append(s.states, setStates...)
			for _, state := range setStates {
				for _, print := range state.Prints {
					s.stateByPrint[print] = state
					if referenced[print] {
						s.markPendingLocked(print)
					} else {
						s.markCompleteLocked(print)
					}
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

			s.mu.Lock()
			startLengths := make(map[*setState]int, len(target.States))
			for _, state := range target.States {
				startLengths[state] = len(state.Prints)
			}
			s.mu.Unlock()

			printsByURL, err := scrapeSetListPage(pageURL, body, target)
			if err != nil {
				log.Printf("set list page %s: %v", pageURL, err)
				return
			}

			referenced := referencedPrints(printsByURL)

			s.mu.Lock()
			for _, state := range target.States {
				for _, print := range state.Prints[startLengths[state]:] {
					s.stateByPrint[print] = state
					if referenced[print] {
						s.markPendingLocked(print)
					} else {
						s.markCompleteLocked(print)
					}
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
			s.applyCardResultLocked(result, refs)
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

	restoredURLs := make(map[string]bool)
	if opts.ResumeState != nil && s.resumable {
		queue.Restore(s.resumeStates(opts.ResumeState), opts.ResumeState.Order, resolveFilter)
		for rawURL := range opts.ResumeState.States {
			restoredURLs[canonicalPageURL(rawURL)] = true
		}
	}

	// Always add the URLs that are still unprocessed. restoredURLs uses the
	// canonical page key so a resumed run is not queued twice when the stored
	// URL and the current href encode the title differently.
	for _, release := range releases {
		key := canonicalPageURL(release.URL)
		if !s.isProcessed(key) && !restoredURLs[key] {
			queue.Add(release.URL, setFilter)
		}
	}
	for key, target := range listPages {
		if !s.isProcessed(key) && !restoredURLs[key] {
			queue.Add(target.URL, setFilter)
		}
	}
	for _, key := range cardURLs {
		if !s.isProcessed(key) && !restoredURLs[key] {
			if _, cached := s.cardResult(key); !cached {
				queue.Add(key, cardFilter)
			}
		}
	}
	for _, key := range galleryURLs {
		if !s.isProcessed(key) && !restoredURLs[key] {
			if _, cached := s.galleryResult(key); !cached {
				queue.Add(key, galleryFilter)
			}
		}
	}

	runErr := queue.Run(ctx)

	states := queue.States()
	log.Printf("ygo: crawl finished with %d URLs", len(states))
	s.mu.Lock()
	s.pages = states
	s.resolveTerminalPagesLocked()
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

		delete(s.printsByCard, cardURL)
		s.applyCardResultLocked(result, refs)
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
		s.markPendingLocked(refs...)
		return
	}

	s.applyCardResultLocked(result, refs)
}

// applyCardResultLocked enriches refs with a parsed card page and decides
// whether their gallery data is still missing. A print is complete as soon as
// its card page (and its gallery, when the page links one) has been parsed.
func (s *releaseScrape) applyCardResultLocked(result cardScrapeResult, refs []*domain.Item) {
	s.applyCardLocked(result, refs)
	if len(refs) == 0 {
		return
	}

	if result.GalleryURL == "" {
		s.markCompleteLocked(refs...)
		return
	}

	galleryKey := canonicalPageURL(result.GalleryURL)
	if gallery, ok := s.galleryResults[galleryKey]; ok {
		s.applyGalleryLocked(gallery, refs)
		return
	}

	s.galleryRefs[galleryKey] = append(s.galleryRefs[galleryKey], refs...)
	s.markPendingLocked(refs...)
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
		s.markCompleteLocked(print)

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

func referencedPrints(byURL map[string][]*domain.Item) map[*domain.Item]bool {
	referenced := make(map[*domain.Item]bool)
	for _, refs := range byURL {
		for _, print := range refs {
			if print != nil {
				referenced[print] = true
			}
		}
	}
	return referenced
}

// pendingSetLocked returns every print that cannot be part of the completed
// snapshot yet. It combines the explicit pending marks with the card/gallery
// queues so a print referenced by a retry is never stored as complete.
func (s *releaseScrape) pendingSetLocked() map[*domain.Item]bool {
	pending := make(map[*domain.Item]bool, len(s.pending))
	for print := range s.pending {
		if print != nil {
			pending[print] = true
		}
	}
	for _, refs := range s.printsByCard {
		for _, print := range refs {
			if print != nil {
				pending[print] = true
			}
		}
	}
	for _, refs := range s.galleryRefs {
		for _, print := range refs {
			if print != nil {
				pending[print] = true
			}
		}
	}
	return pending
}

func (s *releaseScrape) markPendingLocked(prints ...*domain.Item) {
	for _, print := range prints {
		if print != nil {
			s.pending[print] = true
		}
	}
}

func (s *releaseScrape) markCompleteLocked(prints ...*domain.Item) {
	for _, print := range prints {
		if print != nil {
			delete(s.pending, print)
		}
	}
}

func (s *releaseScrape) hasParsedLocked(key string) bool {
	if s.processed[key] {
		return true
	}
	if _, ok := s.cardResults[key]; ok {
		return true
	}
	_, ok := s.galleryResults[key]
	return ok
}

// resumeStates rebuilds the retrier queue from a persisted run. Completed
// pages without their parsed result (for example after a crash between the
// periodic URL save and the provider snapshot) and failed pages are retried
// instead of being skipped. Not-found and invalid pages stay terminal because
// there is no data to parse.
func (s *releaseScrape) resumeStates(run *domain.RetrierRun) map[string]domain.URLState {
	if run == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	states := make(map[string]domain.URLState, len(run.States))
	for rawURL, state := range run.States {
		state.URL = rawURL
		key := canonicalPageURL(rawURL)

		if state.Terminal() && !s.hasParsedLocked(key) {
			switch {
			case state.NotFound, state.Invalid:
				// Keep the terminal state: the page has no parseable data.
			default:
				// Retry completed-without-result and failed pages.
				state = domain.URLState{URL: rawURL}
			}
		}
		states[rawURL] = state
	}

	return states
}

// resolveTerminalPagesLocked unblocks pages that do not exist. A not-found or
// filtered page cannot add data, so its prints are complete with the data that
// the set list already provided. Failed pages remain pending and are retried.
func (s *releaseScrape) resolveTerminalPagesLocked() {
	pages := make(map[string]domain.URLState, len(s.pages))
	for rawURL, state := range s.pages {
		pages[canonicalPageURL(rawURL)] = state
	}

	for key, refs := range s.printsByCard {
		state, ok := pages[key]
		if !ok || (!state.NotFound && !state.Invalid) {
			continue
		}
		delete(s.printsByCard, key)
		s.markCompleteLocked(refs...)
	}

	for key, refs := range s.galleryRefs {
		state, ok := pages[key]
		if !ok || (!state.NotFound && !state.Invalid) {
			continue
		}
		delete(s.galleryRefs, key)
		s.markCompleteLocked(refs...)
	}

	for _, release := range s.releases {
		key := canonicalPageURL(release.URL)
		if s.processed[key] {
			continue
		}
		if state, ok := pages[key]; ok && (state.NotFound || state.Invalid) {
			s.processed[key] = true
		}
	}

	for key := range s.listPageTargets {
		if s.processed[key] {
			continue
		}
		if state, ok := pages[key]; ok && (state.NotFound || state.Invalid) {
			s.processed[key] = true
		}
	}
}
