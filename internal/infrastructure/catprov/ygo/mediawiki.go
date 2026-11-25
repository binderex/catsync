package ygo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/binderex/catsync/internal/domain"
	"github.com/binderex/catsync/internal/utility/retrier"
)

const (
	// MediaWiki API settings for the set index. The whole category is fetched
	// with Semantic MediaWiki (including the Medium property), so medium
	// filtering happens in memory.
	mediawikiAPIPath         = "/api.php"
	mediawikiAllSetsCategory = "Category:All sets"

	// mediawikiAskLimit is the number of Semantic MediaWiki results requested
	// per query. The server needs ~35s for 5000 results with printouts, close
	// to the client timeout, while 1000 results take ~5s, so the category is
	// paged in chunks of 1000.
	mediawikiAskLimit = 1000

	// Set mediums accepted by fetchMediawikiAllSets.
	MediumTCG      = "TCG"
	MediumOCG      = "OCG"
	MediumRushDuel = "Rush Duel"
)

// mediawikiSetMediumValues maps the mediums accepted by fetchMediawikiAllSets
// to the values stored in the Semantic MediaWiki Medium property.
var mediawikiSetMediumValues = map[string]string{
	MediumTCG:      "TCG",
	MediumOCG:      "OCG",
	MediumRushDuel: "Rush Duel",
}

// mediawikiMediumAliases normalizes the medium values some set pages store as
// the full product name instead of TCG/OCG.
var mediawikiMediumAliases = map[string]string{
	"yu-gi-oh! official card game": "OCG",
	"yu-gi-oh! trading card game":  "TCG",
	"yu-gi-oh! rush duel":          MediumRushDuel,
}

// mediawikiSetResponse is a set page listed in Category:All sets.
type mediawikiSetResponse struct {
	URL     string
	Name    string
	Mediums []string
	// Release is the earliest release date of the set in ISO 8601
	// (2006-01-02), or empty when the page has no release date annotated.
	Release string
	// Languages are the language editions of the set that expose a release
	// date, ordered by language.
	Languages []mediawikiSetLanguage
}

// mediawikiSetLanguage is a language edition of a set with its release date.
type mediawikiSetLanguage struct {
	Lang    domain.Lang
	Release string
}

// mediawikiAskResponse is the subset of the Semantic MediaWiki ask API response
// consumed by fetchMediawikiSetPages.
type mediawikiAskResponse struct {
	Error          json.RawMessage `json:"error"`
	ContinueOffset int             `json:"query-continue-offset"`
	Query          struct {
		Results mediawikiAskResults `json:"results"`
	} `json:"query"`
}

// mediawikiAskResults decodes ask results, which Semantic MediaWiki serializes
// as [] when the query has no matches and as an object otherwise.
type mediawikiAskResults map[string]mediawikiAskPage

func (results *mediawikiAskResults) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "", "null", "[]":
		*results = nil
		return nil
	}

	var pages map[string]mediawikiAskPage
	if err := json.Unmarshal(data, &pages); err != nil {
		return err
	}

	*results = pages

	return nil
}

// mediawikiAskPrintouts are the set page properties requested from the ask API.
type mediawikiAskPrintouts struct {
	Medium                        []string           `json:"Medium"`
	EnglishReleaseDate            []mediawikiAskDate `json:"English release date"`
	FrenchReleaseDate             []mediawikiAskDate `json:"French release date"`
	GermanReleaseDate             []mediawikiAskDate `json:"German release date"`
	ItalianReleaseDate            []mediawikiAskDate `json:"Italian release date"`
	PortugueseReleaseDate         []mediawikiAskDate `json:"Portuguese release date"`
	SpanishReleaseDate            []mediawikiAskDate `json:"Spanish release date"`
	JapaneseReleaseDate           []mediawikiAskDate `json:"Japanese release date"`
	AsianEnglishReleaseDate       []mediawikiAskDate `json:"Asian-English release date"`
	KoreanReleaseDate             []mediawikiAskDate `json:"Korean release date"`
	TraditionalChineseReleaseDate []mediawikiAskDate `json:"Traditional Chinese release date"`
	SimplifiedChineseReleaseDate  []mediawikiAskDate `json:"Simplified Chinese release date"`
}

// mediawikiAskDate is a date printout from Semantic MediaWiki.
type mediawikiAskDate struct {
	Timestamp mediawikiTimestamp `json:"timestamp"`
	Raw       string             `json:"raw"`
}

// mediawikiTimestamp is a Unix timestamp that Semantic MediaWiki may serialize
// as a string or as a number.
type mediawikiTimestamp int64

func (timestamp *mediawikiTimestamp) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), `"`)
	if value == "" || value == "null" {
		*timestamp = 0
		return nil
	}

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}

	*timestamp = mediawikiTimestamp(seconds)

	return nil
}

// mediawikiAskPage is a single ask result.
type mediawikiAskPage struct {
	FullURL   string                `json:"fullurl"`
	Namespace int                   `json:"namespace"`
	Printouts mediawikiAskPrintouts `json:"printouts"`
}

// mediawikiFetcher requests MediaWiki API pages through the retrier utility,
// which owns the retry policy for throttled and failed requests.
type mediawikiFetcher struct {
	retrier *retrier.Retrier
}

func newMediawikiFetcher(client *http.Client) *mediawikiFetcher {
	return &mediawikiFetcher{
		retrier: retrier.NewHTTP(client, retrier.WithUserAgent(browserUserAgent)),
	}
}

// get returns the body of pageURL once the retrier reaches a terminal state.
func (f *mediawikiFetcher) get(ctx context.Context, pageURL string) ([]byte, error) {
	response, err := f.retrier.Fetch(ctx, pageURL)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s not found", ErrMediawikiUnexpectedStatus, pageURL)
		}
		return nil, fmt.Errorf("%w: %v", ErrMediawikiUnexpectedStatus, err)
	}

	return response.Body, nil
}

// fetchMediawikiAllSets returns the set pages of Category:All sets with their
// canonical URL, title and mediums, sorted by name. Mediums (TCG, OCG and/or
// Rush Duel) filter the result; with no mediums every set page is returned.
// The category is fetched once and filtered in memory, so asking for more
// mediums costs no extra requests.
func fetchMediawikiAllSets(ctx context.Context, mediums ...string) ([]mediawikiSetResponse, error) {
	client := &http.Client{Timeout: clientTimeout}

	return fetchMediawikiSets(ctx, client, yugipediaBaseURL, mediums)
}

// mediawikiReleases expands the mediawiki set pages into one Release per
// language edition. Sets without any language date produce a single release
// with an empty Lang and the set-level date.
func mediawikiReleases(sets []mediawikiSetResponse) []Release {
	releases := make([]Release, 0, len(sets))

	for _, set := range sets {
		if isMixedRushDuel(set.Mediums) {
			// A set that belongs to both a regular medium and Rush Duel is
			// ambiguous and is not imported.
			continue
		}

		brand := brandFromMediawikiMediums(set.Mediums)
		medium := firstMediawikiMedium(set.Mediums)

		// Rush Duel is implemented but stays inactive until an official
		// English release exists. Only English editions are emitted.
		if medium == MediumRushDuel {
			if release, ok := rushDuelEnglishRelease(set); ok {
				releases = append(releases, release)
			}
			continue
		}

		if len(set.Languages) == 0 {
			releases = append(releases, Release{
				Name:      set.Name,
				URL:       set.URL,
				ReleaseAt: parseReleaseDate(set.Release),
				Brand:     brand,
				Medium:    medium,
			})
			continue
		}

		for _, language := range set.Languages {
			releases = append(releases, Release{
				Name:      set.Name,
				URL:       set.URL,
				ReleaseAt: parseReleaseDate(language.Release),
				Lang:      language.Lang,
				Brand:     brand,
				Medium:    medium,
			})
		}
	}

	return releases
}

func firstMediawikiMedium(mediums []string) string {
	rushDuel := false

	for _, medium := range mediums {
		switch {
		case strings.EqualFold(medium, MediumTCG):
			return MediumTCG
		case strings.EqualFold(medium, MediumOCG):
			return MediumOCG
		case strings.EqualFold(medium, MediumRushDuel):
			rushDuel = true
		}
	}

	if rushDuel {
		return MediumRushDuel
	}
	return ""
}

func brandFromMediawikiMediums(mediums []string) domain.Brand {
	rushDuel := false

	for _, medium := range mediums {
		switch {
		case strings.EqualFold(medium, MediumTCG), strings.EqualFold(medium, MediumOCG):
			return domain.BrandYGO
		case strings.EqualFold(medium, MediumRushDuel):
			rushDuel = true
		}
	}

	if rushDuel {
		return domain.BrandYGORD
	}
	return domain.BrandYGO
}

// isMixedRushDuel reports whether a set belongs to Rush Duel and to a regular
// medium (TCG or OCG) at the same time. Those sets are ambiguous and are not
// imported.
func isMixedRushDuel(mediums []string) bool {
	rushDuel := false
	regular := false

	for _, medium := range mediums {
		switch {
		case strings.EqualFold(medium, MediumRushDuel):
			rushDuel = true
		case strings.EqualFold(medium, MediumTCG), strings.EqualFold(medium, MediumOCG):
			regular = true
		}
	}

	return rushDuel && regular
}

// rushDuelEnglishRelease returns the English edition of a Rush Duel set. Rush
// Duel remains disabled while no official English release exists, so Japanese
// or other language editions are ignored.
func rushDuelEnglishRelease(set mediawikiSetResponse) (Release, bool) {
	for _, language := range set.Languages {
		if language.Lang != domain.LangEN {
			continue
		}

		when := parseReleaseDate(language.Release)
		if when.IsZero() {
			return Release{}, false
		}

		return Release{
			Name:      set.Name,
			URL:       set.URL,
			ReleaseAt: when,
			Lang:      domain.LangEN,
			Brand:     domain.BrandYGORD,
			Medium:    MediumRushDuel,
		}, true
	}

	return Release{}, false
}

// fetchMediawikiSets validates the requested mediums, fetches every set page
// once and filters in memory. client and baseURL are parameters so tests can
// point them at a fake server.
func fetchMediawikiSets(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	mediums []string,
) ([]mediawikiSetResponse, error) {
	filter := make(map[string]bool, len(mediums))
	for _, medium := range mediums {
		value, ok := mediawikiMediumValue(medium)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownSetMedium, medium)
		}
		filter[value] = true
	}

	fetcher := newMediawikiFetcher(client)

	sets, err := fetchMediawikiSetPages(ctx, fetcher, baseURL)
	if err != nil {
		return nil, err
	}

	if len(filter) > 0 {
		sets = filterMediawikiSets(sets, filter)
	}

	if len(sets) == 0 {
		return nil, ErrSetsNotFound
	}

	sortMediawikiSets(sets)

	return sets, nil
}

// fetchMediawikiSetPages asks Semantic MediaWiki for every page in
// Category:All sets together with its Medium property, following the query
// offset until the API stops paging.
func fetchMediawikiSetPages(
	ctx context.Context,
	fetcher *mediawikiFetcher,
	baseURL string,
) ([]mediawikiSetResponse, error) {
	var (
		sets []mediawikiSetResponse
		seen = make(map[string]bool, mediawikiAskLimit)
	)

	for offset := 0; ; {
		body, err := fetcher.get(ctx, mediawikiAskURL(baseURL, offset))
		if err != nil {
			return nil, err
		}

		var response mediawikiAskResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode mediawiki sets: %w", err)
		}
		if len(response.Error) > 0 {
			return nil, fmt.Errorf("mediawiki ask: %s", response.Error)
		}

		for title, page := range response.Query.Results {
			if page.Namespace != 0 || page.FullURL == "" || title == "" || seen[page.FullURL] {
				continue
			}
			seen[page.FullURL] = true

			languages := mediawikiSetLanguages(page.Printouts)

			sets = append(sets, mediawikiSetResponse{
				URL:       page.FullURL,
				Name:      title,
				Mediums:   normalizeMediawikiMediums(page.Printouts.Medium),
				Release:   mediawikiReleaseDate(languages),
				Languages: languages,
			})
		}

		if response.ContinueOffset == 0 || response.ContinueOffset <= offset {
			break
		}
		offset = response.ContinueOffset
	}

	return sets, nil
}

// filterMediawikiSets keeps the sets that belong to at least one medium.
func filterMediawikiSets(sets []mediawikiSetResponse, mediums map[string]bool) []mediawikiSetResponse {
	filtered := make([]mediawikiSetResponse, 0, len(sets))

	for _, set := range sets {
		for _, medium := range set.Mediums {
			if mediums[medium] {
				filtered = append(filtered, set)
				break
			}
		}
	}

	return filtered
}

// mediawikiMediumValue resolves a medium name case-insensitively.
func mediawikiMediumValue(medium string) (string, bool) {
	medium = strings.TrimSpace(medium)
	for name, value := range mediawikiSetMediumValues {
		if strings.EqualFold(name, medium) {
			return value, true
		}
	}

	return "", false
}

// mediawikiSetLanguages returns the language editions of a set that expose a
// release date, ordered by language.
func mediawikiSetLanguages(printouts mediawikiAskPrintouts) []mediawikiSetLanguage {
	dates := []struct {
		lang  domain.Lang
		dates []mediawikiAskDate
	}{
		{domain.LangEN, printouts.EnglishReleaseDate},
		{domain.LangFR, printouts.FrenchReleaseDate},
		{domain.LangDE, printouts.GermanReleaseDate},
		{domain.LangIT, printouts.ItalianReleaseDate},
		{domain.LangPT, printouts.PortugueseReleaseDate},
		{domain.LangES, printouts.SpanishReleaseDate},
		{domain.LangJP, printouts.JapaneseReleaseDate},
		{domain.LangAE, printouts.AsianEnglishReleaseDate},
		{domain.LangKR, printouts.KoreanReleaseDate},
		{domain.LangTC, printouts.TraditionalChineseReleaseDate},
		{domain.LangSC, printouts.SimplifiedChineseReleaseDate},
	}

	languages := make([]mediawikiSetLanguage, 0, len(dates))
	for _, entry := range dates {
		release := mediawikiDate(entry.dates)
		if release == "" {
			continue
		}

		languages = append(languages, mediawikiSetLanguage{Lang: entry.lang, Release: release})
	}

	if len(languages) == 0 {
		return nil
	}

	return languages
}

// mediawikiReleaseDate returns the earliest release date of the set in ISO
// 8601, or "" when no language exposes a date.
func mediawikiReleaseDate(languages []mediawikiSetLanguage) string {
	earliest := ""

	for _, language := range languages {
		if earliest == "" || language.Release < earliest {
			earliest = language.Release
		}
	}

	return earliest
}

// mediawikiDate returns the earliest date of a printout in ISO 8601, or "" when
// it has no value.
func mediawikiDate(dates []mediawikiAskDate) string {
	var earliest time.Time

	for _, date := range dates {
		if date.Timestamp <= 0 {
			continue
		}

		when := time.Unix(int64(date.Timestamp), 0).UTC()
		if earliest.IsZero() || when.Before(earliest) {
			earliest = when
		}
	}

	if earliest.IsZero() {
		return ""
	}

	return earliest.Format("2006-01-02")
}

// normalizeMediawikiMediums drops the "Official" marker, expands the full
// product names some pages use and removes duplicates.
func normalizeMediawikiMediums(values []string) []string {
	mediums := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "Official") {
			continue
		}

		if alias, ok := mediawikiMediumAliases[strings.ToLower(value)]; ok {
			value = alias
		}
		if seen[value] {
			continue
		}
		seen[value] = true

		mediums = append(mediums, value)
	}

	if len(mediums) == 0 {
		return nil
	}

	sort.Strings(mediums)

	return mediums
}

// sortMediawikiSets orders sets by name, ignoring case, with a stable
// tie-breaker for names that only differ in case.
func sortMediawikiSets(sets []mediawikiSetResponse) {
	sort.Slice(sets, func(i, j int) bool {
		left := strings.ToLower(sets[i].Name)
		right := strings.ToLower(sets[j].Name)
		if left == right {
			return sets[i].Name < sets[j].Name
		}
		return left < right
	})
}

// mediawikiAskURL builds a Semantic MediaWiki ask request for every set page
// with its medium and the release date of each language. The limit and offset
// belong to the SMW query string, not to the API parameters.
func mediawikiAskURL(baseURL string, offset int) string {
	values := url.Values{
		"action": {"ask"},
		"format": {"json"},
	}

	query := fmt.Sprintf(
		"[[%s]]|?Medium"+
			"|?English release date"+
			"|?French release date"+
			"|?German release date"+
			"|?Italian release date"+
			"|?Portuguese release date"+
			"|?Spanish release date"+
			"|?Japanese release date"+
			"|?Asian-English release date"+
			"|?Korean release date"+
			"|?Traditional Chinese release date"+
			"|?Simplified Chinese release date"+
			"|limit=%d",
		mediawikiAllSetsCategory,
		mediawikiAskLimit,
	)
	if offset > 0 {
		query += fmt.Sprintf("|offset=%d", offset)
	}
	values.Set("query", query)

	return strings.TrimSuffix(baseURL, "/") + mediawikiAPIPath + "?" + values.Encode()
}

// userAgentTransport injects the browser User-Agent on every request, which
// retrier.HTTP would not set on its own.
type userAgentTransport struct {
	userAgent string
	base      http.RoundTripper
}

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", t.userAgent)

	return t.base.RoundTrip(clone)
}
