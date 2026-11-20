package ygo

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/binderex/catsync/internal/catalog"
)

var (
	cardCodeRegex         = regexp.MustCompile(`^[A-Za-z0-9/]+-[A-Za-z0-9]+$`)
	extendedArtworkSuffix = regexp.MustCompile(`(?i)\s*\(extended art\)\s*`)
	numberRegex           = regexp.MustCompile(`\d+`)

	// boosterKeywords marks sets whose cards are distributed randomly. In
	// those sets GuaranteedCopies is left at zero, meaning "random".
	boosterKeywords = []string{
		"booster pack",
		"booster",
		"mega pack",
		"mini pack",
		"booster box",
		"booster case",
	}
)

// max is the cutoff used to decide whether a release date is authorized.
// Tests can override it to exercise future releases.
var max = time.Now().Add(24 * time.Hour)

// parseReleaseDate accepts the date formats used by Yugipedia set pages,
// release lists and card print tables.
func parseReleaseDate(value string) time.Time {
	value = cleanText(value)
	if value == "" {
		return time.Time{}
	}

	formats := []string{
		"January 2, 2006",
		"January 2006",
		"January 2 2006",
		"2006-01-02",
		"2006-01",
		"2006",
	}

	for _, format := range formats {
		if date, err := time.Parse(format, value); err == nil {
			return date
		}
	}

	// Yearless dates such as "November 12" or "December" default to the
	// current year.
	year := time.Now().Year()
	withYear := fmt.Sprintf("%s %d", value, year)
	for _, format := range []string{"January 2 2006", "January 2006"} {
		if date, err := time.Parse(format, withYear); err == nil {
			return date
		}
	}

	return time.Time{}
}

// parseNewReleaseDate keeps the historical name used by the release scraper.
func parseNewReleaseDate(value string) time.Time {
	return parseReleaseDate(value)
}

// isTooEarly reports whether a known release date is later than the allowed
// cutoff. Unknown dates are not considered too early.
func isTooEarly(date time.Time) bool {
	return !date.IsZero() && date.After(max)
}

// isReleaseAuthorized reports whether a release date may be included.
// Unknown dates are allowed; known future dates are rejected.
func isReleaseAuthorized(date time.Time) bool {
	return !isTooEarly(date)
}

func scrapeImage(selection *goquery.Selection) *catalog.Image {
	image := selection.Find("img").First()
	if image.Length() == 0 {
		return nil
	}

	source, exists := image.Attr("src")
	if !exists || source == "" {
		return nil
	}

	smallURL := source
	largeURL := source

	if before, after, ok := strings.Cut(source, "/thumb/"); ok {
		if before0, _, ok := strings.CutLast(after, "/"); ok {
			largeURL = strings.TrimRight(before, "/") + "/" + before0
		}
	}

	return &catalog.Image{
		URLSmall: smallURL,
		URLLarge: largeURL,
	}
}

func imageValue(image *catalog.Image) catalog.Image {
	if image == nil {
		return catalog.Image{}
	}
	return *image
}

func labelToLang(label string) (catalog.Lang, bool) {
	normalized := strings.ToLower(cleanText(strings.ReplaceAll(label, "\u00a0", " ")))
	if index := strings.Index(normalized, "("); index >= 0 {
		normalized = cleanText(normalized[:index])
	}

	languages := map[string]catalog.Lang{
		"en":                  catalog.LangEN,
		"english":             catalog.LangEN,
		"fr":                  catalog.LangFR,
		"french":              catalog.LangFR,
		"de":                  catalog.LangDE,
		"german":              catalog.LangDE,
		"it":                  catalog.LangIT,
		"italian":             catalog.LangIT,
		"pt":                  catalog.LangPT,
		"portuguese":          catalog.LangPT,
		"es":                  catalog.LangES,
		"sp":                  catalog.LangES,
		"spanish":             catalog.LangES,
		"jp":                  catalog.LangJP,
		"ja":                  catalog.LangJP,
		"japanese":            catalog.LangJP,
		"ae":                  catalog.LangAE,
		"asian-english":       catalog.LangAE,
		"kr":                  catalog.LangKR,
		"ko":                  catalog.LangKR,
		"korean":              catalog.LangKR,
		"tc":                  catalog.LangTC,
		"zh-hant":             catalog.LangTC,
		"traditional chinese": catalog.LangTC,
		"sc":                  catalog.LangSC,
		"zh-hans":             catalog.LangSC,
		"simplified chinese":  catalog.LangSC,
		"chinese":             catalog.LangSC,
	}

	lang, ok := languages[normalized]
	return lang, ok
}

func langFromCardCode(code string) catalog.Lang {
	switch cardLanguageToken(code) {
	case "EN":
		return catalog.LangEN
	case "FR":
		return catalog.LangFR
	case "DE":
		return catalog.LangDE
	case "IT":
		return catalog.LangIT
	case "PT":
		return catalog.LangPT
	case "SP", "ES":
		return catalog.LangES
	case "JP", "JA":
		return catalog.LangJP
	case "AE":
		return catalog.LangAE
	case "KR", "KO":
		return catalog.LangKR
	case "TC":
		return catalog.LangTC
	case "SC":
		return catalog.LangSC
	default:
		return ""
	}
}

func cardCodeLangToken(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.ToUpper(strings.TrimRight(parts[1], "0123456789"))
}

// cardLanguageToken strips the trailing "S" used by special token prints such
// as BETB-JPS15, BETB-AES15 or BETB-KRS15.
func cardLanguageToken(code string) string {
	token := cardCodeLangToken(code)
	switch token {
	case "AES", "ENS", "FRS", "DES", "ITS", "PTS", "SPS", "JPS", "KRS", "TCS", "SCS":
		return strings.TrimSuffix(token, "S")
	default:
		return token
	}
}

func cardSerialCode(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimPrefix(parts[1], cardCodeLangToken(code))
}

func cardSetCode(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.ToUpper(parts[0])
}

func cardRegionalCode(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) < 2 {
		return ""
	}
	token := cardLanguageToken(code)
	if token == "" {
		return ""
	}
	return strings.ToUpper(parts[0]) + "-" + token
}

// canonicalPageURL decodes percent-escaped path characters so the same page
// always maps to the same key, regardless of whether Colly or MediaWiki
// encoded characters such as parentheses.
func canonicalPageURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	decoded, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		decoded = parsed.Path
	}

	canonical := parsed.Scheme + "://" + parsed.Host + decoded
	if parsed.RawQuery != "" {
		canonical += "?" + parsed.RawQuery
	}
	return canonical
}

func absoluteCardURL(pageURL, href string) string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}

	reference, err := url.Parse(href)
	if err != nil {
		return ""
	}

	return base.ResolveReference(reference).String()
}

// cardNameFromURL extracts a card name from a Yugipedia article URL. It is
// used as a last resort when the page title or card table is unavailable.
func cardNameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	name, err := url.PathUnescape(path.Base(parsed.Path))
	if err != nil {
		name = path.Base(parsed.Path)
	}

	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSuffix(name, " (card)")
	return cleanText(name)
}

func cardURLFromExternalID(externalID string) string {
	externalID = cleanText(externalID)
	if externalID == "" {
		return ""
	}

	slug := (&url.URL{Path: strings.ReplaceAll(externalID, " ", "_")}).EscapedPath()
	return strings.TrimSuffix(yugipediaBaseURL, "/") + "/wiki/" + slug
}

// cardFallbackURL returns the "(card)" URL variant when the current URL is not
// already that variant.
// func cardFallbackURL(externalID, currentURL string) string {
// 	base := cardURLFromExternalID(externalID)
// 	if base == "" {
// 		return ""
// 	}

// 	fallback := base + "_(card)"
// 	if currentURL == fallback {
// 		return ""
// 	}
// 	return fallback
// }

func isCardArticleHref(href string) bool {
	return strings.Contains(href, "/wiki/") &&
		!strings.Contains(href, "redlink=1") &&
		!strings.Contains(href, "action=edit")
}

func containsExtendedArt(value string) bool {
	return strings.Contains(strings.ToLower(value), "(extended art)")
}

func cleanCardName(value string) string {
	value = extendedArtworkSuffix.ReplaceAllString(value, " ")
	value = cleanText(value)
	value = strings.Trim(value, "\"“”")
	return cleanText(value)
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	return strings.Join(strings.Fields(value), " ")
}

// cleanRubyText returns the visible text of a node after removing the ruby
// annotations that Yugipedia adds to Japanese names.
func cleanRubyText(selection *goquery.Selection) string {
	clone := selection.Clone()
	clone.Find("rt, rp").Remove()
	return cleanText(clone.Text())
}

func normalizeHeader(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if cleanText(value) != "" {
			return value
		}
	}
	return ""
}

func hasPlaceholderCode(code string) bool {
	return strings.Contains(code, "?")
}

func readCellText(cell *goquery.Selection) string {
	if text := cleanRubyText(cell.Find("a").First()); text != "" {
		return text
	}
	return cleanRubyText(cell)
}

func readRarities(tds *goquery.Selection, index int) []string {
	if index < 0 || index >= tds.Length() {
		return nil
	}

	cell := tds.Eq(index)
	var rarities []string

	cell.Find("a").Each(func(_ int, link *goquery.Selection) {
		rarity := cleanRubyText(link)
		if rarity != "" {
			rarities = append(rarities, rarity)
		}
	})

	if len(rarities) > 0 {
		return rarities
	}

	text := cleanRubyText(cell)
	if text == "" {
		return nil
	}

	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '\n'
	}) {
		if rarity := cleanText(part); rarity != "" {
			rarities = append(rarities, rarity)
		}
	}

	return rarities
}

func printVariantKey(print catalog.CardPrinted) string {
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(print.Code)),
		strings.ToLower(strings.TrimSpace(print.Rarity)),
		strings.ToLower(strings.TrimSpace(print.Edition)),
		boolKey(print.ExtendedArt),
		string(print.Lang),
	}, "|")
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func detectBooster(setType string, root *goquery.Selection) bool {
	if strings.TrimSpace(setType) != "" {
		return containsBoosterKeyword(setType)
	}

	infobox := root.Find("table.infobox").First()
	if infobox.Length() > 0 && containsBoosterKeyword(infobox.Text()) {
		return true
	}

	return false
}

func containsBoosterKeyword(value string) bool {
	value = strings.ToLower(cleanText(value))
	for _, keyword := range boosterKeywords {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

// collectSetQuantities counts how often each code and code+rarity appears in
// the set list. The count becomes GuaranteedCopies for non-random products.
func collectSetQuantities(root *goquery.Selection) map[string]uint {
	quantities := make(map[string]uint)

	visitList := func(list *goquery.Selection) {
		table := list.Find("table").First()
		if table.Length() == 0 {
			return
		}

		layout := parseSetListLayout(table)
		if layout.codeIdx < 0 {
			return
		}

		table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() == 0 || layout.codeIdx >= tds.Length() {
				return
			}

			code := readCellText(tds.Eq(layout.codeIdx))
			if code == "" || hasPlaceholderCode(code) {
				return
			}

			codeKey := strings.ToUpper(code)
			rarities := readRarities(tds, layout.rarityIdx)
			if len(rarities) == 0 {
				quantities[codeKey]++
				return
			}

			for _, rarity := range rarities {
				quantities[codeKey+"|"+strings.ToLower(cleanText(rarity))]++
			}
		})
	}

	container := root.Find(".set-list-tab").First()
	if container.Length() == 0 {
		container = root.Find(".tabbertab").First()
	}

	if container.Length() == 0 {
		root.Find(".set-list").Each(func(_ int, list *goquery.Selection) {
			if !isBonusSetList(list) {
				visitList(list)
			}
		})
		return quantities
	}

	currentSectionIsBonus := false
	container.Children().Each(func(_ int, node *goquery.Selection) {
		switch goquery.NodeName(node) {
		case "h2", "h3", "h4":
			currentSectionIsBonus = isBonusHeading(node)
		case "div":
			if node.HasClass("mw-heading") {
				currentSectionIsBonus = isBonusHeading(node)
				return
			}
			if node.HasClass("set-list") && !currentSectionIsBonus {
				visitList(node)
			}
		}
	})

	return quantities
}

func isBonusHeading(selection *goquery.Selection) bool {
	headline := strings.ToLower(cleanText(selection.Find("span.mw-headline").Text()))
	if headline == "" {
		headline = strings.ToLower(cleanText(selection.Find("h2, h3, h4").Text()))
	}
	if headline == "" {
		headline = strings.ToLower(cleanText(selection.Text()))
	}
	return strings.Contains(headline, "bonus")
}

func isBonusSetList(list *goquery.Selection) bool {
	for node := list; node.Length() > 0; node = node.Parent() {
		for previous := node.Prev(); previous.Length() > 0; previous = previous.Prev() {
			heading := previous
			if !previous.Is("h2, h3, h4") && !previous.HasClass("mw-heading") {
				heading = previous.Find("h2, h3, h4, .mw-heading").Last()
			}
			if heading.Length() > 0 {
				return strings.Contains(strings.ToLower(cleanText(heading.Text())), "bonus")
			}
		}
	}
	return false
}

func quantityFor(quantities map[string]uint, code, rarity string) uint {
	codeKey := strings.ToUpper(strings.TrimSpace(code))
	if rarity == "" {
		return quantities[codeKey]
	}
	return quantities[codeKey+"|"+strings.ToLower(strings.TrimSpace(rarity))]
}
