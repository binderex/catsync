package catprov

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/binderex/catsync/internal/catalog"

	"github.com/gocolly/colly/v2"
)

type YGOCatProv struct {
	ygoproBaseURL    string
	yugipediaBaseURL string
	now              time.Time
	client           *http.Client
}

func NewYGOCatProv() *YGOCatProv {
	return &YGOCatProv{
		ygoproBaseURL:    "https://db.ygoprodeck.com/api/v7",
		yugipediaBaseURL: "https://yugipedia.com",
		now:              time.Now(),
		client:           &http.Client{Timeout: 30 * time.Second},
	}
}

// isReleased reports whether date is a valid YYYY-MM-DD date that is not
// later than 10 hours from now.
func isReleased(date string, max time.Time) bool {
	t, err := time.Parse("2006-01-02", date)
	return err == nil && !t.After(max)
}

func (prov YGOCatProv) GetCards(name string, max time.Time) ([]catalog.CatalogItem, error) {
	var reqURL string
	if name != "" {
		reqURL = fmt.Sprintf("%s/cardinfo.php?fname=%s&misc=yes", prov.ygoproBaseURL, url.QueryEscape(name))
	} else {
		reqURL = fmt.Sprintf("%s/cardinfo.php?misc=yes", prov.ygoproBaseURL)
	}

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := prov.client.Do(req)
	if err != nil {
		var netErr net.Error
		if (errors.As(err, &netErr) && netErr.Timeout()) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrClientTimeout
		}
		return nil, fmt.Errorf("request ygopro cards: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrCardsNotFound
	}

	type YGOPROCard struct {
		ID          int                 `json:"id"`
		Name        string              `json:"name"`
		Types       string              `json:"humanReadableCardType"`
		Description string              `json:"desc"`
		Archetype   string              `json:"archetype"`
		ATK         int                 `json:"atk"`
		DEF         int                 `json:"def"`
		Race        string              `json:"race"`
		Attribute   string              `json:"attribute"`
		Level       int                 `json:"level"`
		Images      []catalog.CardImage `json:"card_images"`
		CardSets    []struct{}          `json:"card_sets"`
		MiscInfo    []struct {
			TCGDate string `json:"tcg_date"`
			OCGDate string `json:"ocg_date"`
		} `json:"misc_info"`
	}

	var response struct {
		Data []YGOPROCard `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode ygopro cards: %w", err)
	}

	items := make([]catalog.CatalogItem, 0, len(response.Data))

	for _, card := range response.Data {
		if strings.Contains(card.Types, "Skill") {
			log.Printf("skipping card %s (%d): It's a skill card", card.Name, card.ID)
			continue
		}

		if len(card.MiscInfo) > 0 {
			misc := card.MiscInfo[0]
			hasDate := misc.TCGDate != "" || misc.OCGDate != ""

			if hasDate && !isReleased(misc.TCGDate, max) && !isReleased(misc.OCGDate, max) {
				log.Printf("skipping card %s (%d): release date is more than 1 day in the future (tcg: %s, ocg: %s)",
					card.Name, card.ID, misc.TCGDate, misc.OCGDate)
				continue
			}
		}

		cardTypes := strings.Split(card.Types, " ")
		slices.Reverse(cardTypes)
		cardInfo := map[string]any{
			"card_types": cardTypes,
			"atk":        card.ATK,
			"def":        card.DEF,
			"race":       card.Race,
			"attribute":  card.Attribute,
			"level":      card.Level,
		}

		description := card.Description
		archetype := card.Archetype
		cardImages := card.Images

		items = append(items, catalog.CatalogItem{
			ExternalID:  fmt.Sprintf("%08d", card.ID),
			Name:        html.UnescapeString(card.Name),
			CardInfo:    cardInfo,
			Description: description,
			Archetype:   archetype,
			CardImages:  cardImages,
			Brand:       catalog.BrandYGO,
			Type:        catalog.CatalogTypeCard,
			Lang:        catalog.LangEN,
		})
	}

	if len(items) == 0 {
		return nil, ErrCardsNotFound
	}

	return items, nil
}

func (prov YGOCatProv) GetCardPrints(card catalog.CatalogItem) ([]catalog.CatalogItem, error) {
	candidates := []string{
		card.Name,
		card.Name + "_(card)",
		card.ExternalID,
	}

	c, err := newYugipediaCollector()
	if err != nil {
		return []catalog.CatalogItem{card}, err
	}

	var (
		status      int
		retryAfter  time.Duration
		invalidPage bool
		galleryURL  string
		body        []byte
	)

	c.OnResponse(func(r *colly.Response) {
		status = r.StatusCode
		retryAfter = parseRetryAfter(r.Headers.Get("Retry-After"))
		body = r.Body
	})
	c.OnError(func(r *colly.Response, err error) {
		status = r.StatusCode
		retryAfter = parseRetryAfter(r.Headers.Get("Retry-After"))
	})
	// Pages without article content or set pages are not valid card pages.
	c.OnHTML(".noarticletext, .set-lists--navbar", func(*colly.HTMLElement) {
		invalidPage = true
	})
	// The card page links to its prints gallery.
	c.OnHTML(`a[href^="/wiki/Card_Gallery:"]`, func(e *colly.HTMLElement) {
		if galleryURL == "" && strings.EqualFold(strings.TrimSpace(e.Text), "Gallery") {
			galleryURL = e.Request.AbsoluteURL(e.Attr("href"))
		}
	})

	// visitDoc fetches pageURL retrying transient errors and parses the page.
	visitDoc := func(pageURL string) (*goquery.Document, error) {
		galleryURL = ""
		body = nil

		if err := visitYugipediaPage(c, pageURL, &status, &retryAfter, &invalidPage); err != nil {
			return nil, err
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("parse yugipedia page %s: %w", pageURL, err)
		}
		return doc, nil
	}

	var cardDoc *goquery.Document

	for _, title := range candidates {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}

		pageURL := fmt.Sprintf("%s/wiki/%s", strings.TrimSuffix(prov.yugipediaBaseURL, "/"), wikiSlug(title))

		doc, err := visitDoc(pageURL)
		if err != nil {
			if errors.Is(err, errNextCandidate) || errors.Is(err, ErrInvalidPage) {
				continue
			}
			return []catalog.CatalogItem{card}, err
		}

		// The page was found: use it and stop trying candidates.
		cardDoc = doc
		break
	}

	if cardDoc == nil {
		return []catalog.CatalogItem{card}, ErrCardPrintsNotFound
	}

	prints := mappedPrints(cardDoc, card, prov.now)
	if len(prints) == 0 {
		return []catalog.CatalogItem{card}, ErrCardPrintsNotFound
	}

	if galleryURL == "" {
		return prints, nil
	}

	gallery := galleryURL
	galleryDoc, err := visitDoc(gallery)
	if err != nil {
		if errors.Is(err, errNextCandidate) || errors.Is(err, ErrInvalidPage) {
			return prints, nil
		}
		return []catalog.CatalogItem{card}, err
	}

	galleryItems := mappedGalleryBoxes(galleryDoc)
	if len(galleryItems) == 0 {
		return prints, nil
	}

	return mergeGalleryIntoPrints(prints, galleryItems), nil
}

func (prov YGOCatProv) GetSets(cards []catalog.CatalogItem) ([]catalog.CatalogItem, error) {
	setNames := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		if card.Lang != catalog.LangEN {
			continue
		}

		name := strings.TrimSpace(card.SetName)
		if name == "" {
			continue
		}

		setNames[name] = struct{}{}
	}

	if len(setNames) == 0 {
		return nil, ErrSetNotFound
	}

	c, err := newYugipediaCollector()
	if err != nil {
		return nil, err
	}

	var (
		status      int
		retryAfter  time.Duration
		invalidPage bool
		body        []byte
	)

	c.OnResponse(func(r *colly.Response) {
		status = r.StatusCode
		retryAfter = parseRetryAfter(r.Headers.Get("Retry-After"))
		body = r.Body
	})
	c.OnError(func(r *colly.Response, err error) {
		status = r.StatusCode
		retryAfter = parseRetryAfter(r.Headers.Get("Retry-After"))
	})
	c.OnHTML(".noarticletext", func(*colly.HTMLElement) {
		invalidPage = true
	})

	now := prov.now
	items := make([]catalog.CatalogItem, 0)
	var errs []error

	// Every set name is requested; there is no candidate fallback here.
	for setName := range setNames {
		pageURL := fmt.Sprintf("%s/wiki/%s", strings.TrimSuffix(prov.yugipediaBaseURL, "/"), wikiSlug(setName))
		body = nil

		if err := visitYugipediaPage(c, pageURL, &status, &retryAfter, &invalidPage); err != nil {
			errs = append(errs, fmt.Errorf("set %q: %w", setName, err))
			continue
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Errorf("set %q: parse page: %w", setName, err))
			continue
		}

		targetCards := make([]catalog.CatalogItem, 0)
		for _, card := range cards {
			if card.SetName != setName {
				continue
			}
			if hasPlaceholderCode(card.Code) {
				continue
			}
			targetCards = append(targetCards, card)
		}

		sets := mappedSetInfo(doc, setName, now)
		if len(sets) == 0 {
			// The set has no language released yet: do not leak its cards.
			continue
		}

		releasedPrefixes := make(map[string]bool)
		releasedLangs := make(map[catalog.Lang]bool)
		for _, s := range sets {
			releasedLangs[s.Lang] = true
			if s.SetRegionalCode != "" && !hasPlaceholderCode(s.SetRegionalCode) {
				releasedPrefixes[strings.ToUpper(s.SetRegionalCode)] = true
			}
		}

		// A card belongs to the released set when its regional code prefix
		// matches (IMPH-JP001 → IMPH-JP). Cards without a code fall back to
		// their language.
		releasedCards := make([]catalog.CatalogItem, 0, len(targetCards))
		for _, card := range targetCards {
			if strings.TrimSpace(card.Code) != "" {
				code := strings.ToUpper(strings.TrimSpace(card.Code))
				if len(releasedPrefixes) == 0 {
					if !releasedLangs[card.Lang] {
						continue
					}
				} else {
					matched := false
					for prefix := range releasedPrefixes {
						if strings.HasPrefix(code, prefix) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}
			} else if !releasedLangs[card.Lang] {
				continue
			}
			releasedCards = append(releasedCards, card)
		}

		items = append(items, sets...)
		items = append(items, mappedSetCarList(doc, sets[0], releasedCards)...)
	}

	if len(errs) > 0 {
		return items, errors.Join(errs...)
	}

	return items, nil
}

// newYugipediaCollector returns a collector configured for Yugipedia.
func newYugipediaCollector() (*colly.Collector, error) {
	c := colly.NewCollector(
		colly.AllowedDomains("yugipedia.com", "www.yugipedia.com"),
		colly.UserAgent(browserUserAgent),
		colly.AllowURLRevisit(),
	)
	c.SetRequestTimeout(30 * time.Second)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	})

	// One request at a time with a small delay to avoid getting rate limited.
	if err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       time.Second,
		RandomDelay: 500 * time.Millisecond,
	}); err != nil {
		return nil, fmt.Errorf("configure yugipedia rate limit: %w", err)
	}

	return c, nil
}

// visitYugipediaPage fetches pageURL retrying transient errors. It returns
// errNextCandidate when the page is not usable.
func visitYugipediaPage(c *colly.Collector, pageURL string, status *int, retryAfter *time.Duration, invalidPage *bool) error {
	for attempt := 1; ; attempt++ {
		*status, *retryAfter, *invalidPage = 0, 0, false

		visitErr := c.Visit(pageURL)
		if visitErr == nil {
			if *invalidPage {
				return ErrInvalidPage
			}
			return nil
		}

		if attempt >= cardPrintsAttempts(*status) {
			switch *status {
			case http.StatusTooManyRequests:
				return ErrTooManyRequests
			case http.StatusUnauthorized, http.StatusForbidden:
				return ErrClientForbidden
			default:
				return fmt.Errorf("visit %s after %d attempts: %w", pageURL, attempt, visitErr)
			}
		}

		if !isRetryableStatus(*status) {
			return fmt.Errorf("%w: %v", errNextCandidate, visitErr)
		}

		time.Sleep(cardPrintsWait(*status, attempt, *retryAfter))
	}
}

// cardPrintsAttempts returns the maximum number of attempts for a status.
func cardPrintsAttempts(status int) int {
	switch status {
	case http.StatusTooManyRequests:
		return cardPrintsTooManyAttempts
	case http.StatusUnauthorized, http.StatusForbidden:
		return cardPrintsBlockedAttempts
	default:
		return cardPrintsMaxAttempts
	}
}

// cardPrintsWait returns how long to wait before retrying a failed request.
func cardPrintsWait(status, attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, cardPrintsMaxDelay)
	}

	var base time.Duration
	switch status {
	case http.StatusTooManyRequests:
		base = cardPrintsTooManyBaseDelay
	case http.StatusUnauthorized, http.StatusForbidden:
		base = cardPrintsBlockedBaseDelay
	default:
		base = cardPrintsRetryDelay
	}

	return min(base<<(attempt-1), cardPrintsMaxDelay)
}

// isRetryableStatus reports whether a failed request may succeed if retried.
func isRetryableStatus(status int) bool {
	switch status {
	case 0, // network/timeout error
		http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// parseRetryAfter reads a Retry-After header expressed either in seconds or
// as an HTTP date. It returns zero when the header is absent or invalid.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if t, err := http.ParseTime(value); err == nil {
		if wait := time.Until(t); wait > 0 {
			return wait
		}
	}

	return 0
}

// wikiSlug converts an article title into the path segment used by Yugipedia.
func wikiSlug(title string) string {
	return url.PathEscape(strings.ReplaceAll(title, " ", "_"))
}
