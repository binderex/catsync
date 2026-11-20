package ygo

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/catalog"
)

// setScrapeData accumulates the infobox information of a set page.
type setScrapeData struct {
	canonicalName string
	setType       string
	image         *catalog.Image
	brand         catalog.Brand
	prefixes      map[catalog.Lang]string
	localNames    map[catalog.Lang]string
	releaseDates  map[catalog.Lang]time.Time
}

// setListLayout describes the columns of one language set-list table.
type setListLayout struct {
	codeIdx    int
	englishIdx int
	localIdx   int
	rarityIdx  int
	lang       catalog.Lang
}

// cardName holds the parts of a card name cell.
type cardName struct {
	Name string
	Text string
	URL  string
}

type pendingPrint struct {
	print catalog.CardPrinted
	url   string
}

// setState keeps the prints of one set as stable pointers while card and
// gallery pages are processed. Set.CardsPrinted is only materialized at the
// end of the scrape.
type setState struct {
	Set    *catalog.Set
	Prints []*catalog.CardPrinted
}

// setListPage is a lazy-loaded Set Card Lists page referenced by one tab of a
// set page. Its prints belong to the states created by the parent set page.
type setListPage struct {
	URL     string
	States  []*setState
	Booster bool
}

// breakdownSection is one language section of the set page Breakdown. Name is
// empty for products whose breakdown applies to every language.
type breakdownSection struct {
	Name         string
	Text         string
	CardsPerPack uint
	PacksPerBox  uint
}

var (
	breakdownCardsPerPackRE = regexp.MustCompile(`(?i)(\d+)\s+(?:different\s+)?cards?\s+per\s+pack`)
	breakdownEachPackRE     = regexp.MustCompile(`(?i)each\s+pack\s+contains\s+(\d+)\s+(?:different\s+)?cards?`)
	breakdownPacksPerBoxRE  = regexp.MustCompile(`(?i)(\d+)\s+packs?\s+per\s+box`)
	breakdownEachBoxRE      = regexp.MustCompile(`(?i)each\s+box\s+contains\s+(\d+)\s+packs?`)
	breakdownRegularPacksRE = regexp.MustCompile(`(?i)(\d+)\s+regular\s+packs?\s+with\s+(\d+)\s+cards?\s+each`)
)

// scrapeSetBreakdown extracts the Breakdown section. Tables, style blocks and
// navigation boxes are excluded; only the readable product description is
// kept.
func scrapeSetBreakdown(root *goquery.Selection) []breakdownSection {
	heading := root.Find("h2#Breakdown").First()
	if heading.Length() == 0 {
		return nil
	}

	start := heading
	if parent := heading.Parent(); parent.Length() > 0 && parent.HasClass("mw-heading") {
		start = parent
	}

	var (
		sections []breakdownSection
		current  breakdownSection
		lines    []string
	)

	flush := func() {
		if len(lines) == 0 {
			current = breakdownSection{}
			return
		}

		current.Text = strings.Join(lines, "\n")
		current.CardsPerPack, current.PacksPerBox = parseBreakdownQuantities(current.Text)
		sections = append(sections, current)
		current = breakdownSection{}
		lines = nil
	}

	start.NextAll().EachWithBreak(func(_ int, node *goquery.Selection) bool {
		name, level := breakdownHeading(node)
		switch level {
		case 2:
			flush()
			return false
		case 3:
			flush()
			current = breakdownSection{Name: name}
			return true
		}

		lines = append(lines, renderBreakdownNode(node)...)
		return true
	})
	flush()

	return sections
}

func breakdownHeading(node *goquery.Selection) (string, int) {
	if node.Is("h2, h3") {
		return cleanText(node.Text()), map[string]int{"h2": 2, "h3": 3}[goquery.NodeName(node)]
	}

	if node.HasClass("mw-heading") {
		if heading := node.Find("h2").First(); heading.Length() > 0 {
			return cleanText(heading.Text()), 2
		}
		if heading := node.Find("h3").First(); heading.Length() > 0 {
			return cleanText(heading.Text()), 3
		}
	}

	return "", 0
}

func renderBreakdownNode(node *goquery.Selection) []string {
	switch goquery.NodeName(node) {
	case "table", "style", "script":
		return nil
	case "p":
		if text := cleanText(node.Text()); text != "" {
			return []string{text}
		}
		return nil
	case "ul", "ol":
		return renderBreakdownList(node, 0)
	case "div", "section":
		clone := node.Clone()
		clone.Find("table, style, script, .mw-editsection, .navbox").Remove()

		var lines []string
		clone.Children().Each(func(_ int, child *goquery.Selection) {
			lines = append(lines, renderBreakdownNode(child)...)
		})
		if len(lines) == 0 {
			if text := cleanText(clone.Text()); text != "" {
				return []string{text}
			}
		}
		return lines
	default:
		if text := cleanText(node.Text()); text != "" {
			return []string{text}
		}
		return nil
	}
}

func renderBreakdownList(list *goquery.Selection, depth int) []string {
	var lines []string

	list.ChildrenFiltered("li").Each(func(_ int, item *goquery.Selection) {
		clone := item.Clone()
		clone.Find("ul, ol, table, style, script, .mw-editsection").Remove()

		if text := cleanText(clone.Text()); text != "" {
			lines = append(lines, strings.Repeat("  ", depth)+"- "+text)
		}

		item.ChildrenFiltered("ul, ol").Each(func(_ int, nested *goquery.Selection) {
			lines = append(lines, renderBreakdownList(nested, depth+1)...)
		})
	})

	return lines
}

func parseBreakdownQuantities(text string) (cardsPerPack, packsPerBox uint) {
	if matches := breakdownRegularPacksRE.FindStringSubmatch(text); len(matches) == 3 {
		packsPerBox = parseUint(matches[1])
		cardsPerPack = parseUint(matches[2])
		return cardsPerPack, packsPerBox
	}

	if matches := breakdownCardsPerPackRE.FindStringSubmatch(text); len(matches) == 2 {
		cardsPerPack = parseUint(matches[1])
	} else if matches := breakdownEachPackRE.FindStringSubmatch(text); len(matches) == 2 {
		cardsPerPack = parseUint(matches[1])
	}

	if matches := breakdownPacksPerBoxRE.FindStringSubmatch(text); len(matches) == 2 {
		packsPerBox = parseUint(matches[1])
	} else if matches := breakdownEachBoxRE.FindStringSubmatch(text); len(matches) == 2 {
		packsPerBox = parseUint(matches[1])
	}

	return cardsPerPack, packsPerBox
}

func parseUint(value string) uint {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0
	}
	return uint(number)
}

func breakdownForLang(sections []breakdownSection, lang catalog.Lang) (breakdownSection, bool) {
	for _, section := range sections {
		if section.Name == "" {
			continue
		}
		if sectionLang, ok := labelToLang(section.Name); ok && sectionLang == lang {
			return section, true
		}
	}

	if isTCGLang(lang) {
		if section, ok := breakdownByName(sections, "TCG"); ok {
			return section, true
		}
	}

	if isAsianLang(lang) {
		if section, ok := breakdownByName(sections, "Japanese"); ok {
			return section, true
		}
	}

	if section, ok := breakdownByName(sections, ""); ok {
		return section, true
	}

	return breakdownSection{}, false
}

func breakdownByName(sections []breakdownSection, name string) (breakdownSection, bool) {
	for _, section := range sections {
		if strings.EqualFold(strings.TrimSpace(section.Name), name) {
			return section, true
		}
	}
	return breakdownSection{}, false
}

func isTCGLang(lang catalog.Lang) bool {
	switch lang {
	case catalog.LangEN, catalog.LangFR, catalog.LangDE, catalog.LangIT, catalog.LangPT, catalog.LangES:
		return true
	default:
		return false
	}
}

func isAsianLang(lang catalog.Lang) bool {
	switch lang {
	case catalog.LangJP, catalog.LangAE, catalog.LangKR, catalog.LangTC, catalog.LangSC:
		return true
	default:
		return false
	}
}

// scrapeSet parses a set page and returns its sets with their base prints. It
// also returns the prints grouped by card URL so the caller can queue the card
// pages and attach their ItemInfo later.
func scrapeSet(
	pageURL string,
	body []byte,
	release Release,
) ([]*setState, map[string][]*catalog.CardPrinted, []*setListPage, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, nil, err
	}

	data := scrapeSetDetails(doc.Selection)
	breakdowns := scrapeSetBreakdown(doc.Selection)
	sets := buildSetItems(pageURL, data, release, breakdowns)
	if len(sets) == 0 {
		return nil, nil, nil, nil
	}

	states := make([]*setState, 0, len(sets))
	stateBySet := make(map[*catalog.Set]*setState, len(sets))
	byLang := make(map[catalog.Lang]*catalog.Set, len(sets))
	byRegionalCode := make(map[string]*catalog.Set, len(sets))
	for _, set := range sets {
		state := &setState{Set: set}
		states = append(states, state)
		stateBySet[set] = state
		byLang[set.Lang] = set
		if set.RegionalCode != "" {
			byRegionalCode[strings.ToUpper(set.RegionalCode)] = set
		}
	}

	booster := detectBooster(data.setType, doc.Selection)
	var quantities map[string]uint
	if !booster {
		quantities = collectSetQuantities(doc.Selection)
	}

	printsByCardURL := scrapeSetCards(pageURL, doc.Selection, stateBySet, byLang, byRegionalCode, booster, quantities)
	listPages := collectSetListPages(pageURL, doc.Selection, states, booster)

	return states, printsByCardURL, listPages, nil
}

func collectSetListPages(
	pageURL string,
	root *goquery.Selection,
	states []*setState,
	booster bool,
) []*setListPage {
	var pages []*setListPage
	seen := make(map[string]bool)
	existing := existingSetListPages(pageURL, root)
	validLanguages := validSetLanguages(states)

	root.Find(".set-list-tab[data-page]").Each(func(_ int, tab *goquery.Selection) {
		title, exists := tab.Attr("data-page")
		if !exists || strings.TrimSpace(title) == "" {
			return
		}

		if lang, ok := setListPageLanguage(title); ok && !validLanguages[lang] {
			return
		}

		pageURL := cardURLFromExternalID(title)
		if pageURL == "" || seen[pageURL] {
			return
		}
		if len(existing) > 0 && !existing[canonicalPageURL(pageURL)] {
			return
		}
		seen[pageURL] = true
		pages = append(pages, &setListPage{
			URL:     pageURL,
			States:  states,
			Booster: booster,
		})
	})

	return pages
}

// existingSetListPages returns the Set Card Lists links that exist on the set
// page. Missing links are rendered as redlinks, so they are not queued.
func existingSetListPages(pageURL string, root *goquery.Selection) map[string]bool {
	pages := make(map[string]bool)

	root.Find(`.set-navigation a[href^="/wiki/Set_Card_Lists:"]`).Each(func(_ int, link *goquery.Selection) {
		href, exists := link.Attr("href")
		if !exists || strings.Contains(href, "redlink=1") {
			return
		}

		class, _ := link.Attr("class")
		if strings.Contains(class, "new") {
			return
		}

		if absolute := absoluteCardURL(pageURL, href); absolute != "" {
			pages[canonicalPageURL(absolute)] = true
		}
	})

	return pages
}

func validSetLanguages(states []*setState) map[catalog.Lang]bool {
	languages := make(map[catalog.Lang]bool, len(states))
	for _, state := range states {
		if state != nil && state.Set != nil {
			languages[state.Set.Lang] = true
		}
	}
	return languages
}

// setListPageLanguage extracts the language token from a Set Card Lists title
// such as "Set Card Lists:Foo (TCG-EN)" or "Foo (OCG-JP-1E)".
func setListPageLanguage(title string) (catalog.Lang, bool) {
	title = cleanText(title)
	start := strings.LastIndex(title, "(")
	end := strings.LastIndex(title, ")")
	if start < 0 || end <= start {
		return "", false
	}

	parts := strings.Split(title[start+1:end], "-")
	for index := len(parts) - 1; index >= 0; index-- {
		if lang, ok := labelToLang(strings.TrimSpace(parts[index])); ok {
			return lang, true
		}
	}

	return "", false
}

// scrapeSetListPage parses one lazy-loaded Set Card Lists page and appends its
// prints to the sets created by the parent page.
func scrapeSetListPage(
	pageURL string,
	body []byte,
	target *setListPage,
) (map[string][]*catalog.CardPrinted, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	stateBySet := make(map[*catalog.Set]*setState, len(target.States))
	byLang := make(map[catalog.Lang]*catalog.Set, len(target.States))
	byRegionalCode := make(map[string]*catalog.Set, len(target.States))
	for _, state := range target.States {
		if state == nil || state.Set == nil {
			continue
		}
		stateBySet[state.Set] = state
		byLang[state.Set.Lang] = state.Set
		if state.Set.RegionalCode != "" {
			byRegionalCode[strings.ToUpper(state.Set.RegionalCode)] = state.Set
		}
	}

	var quantities map[string]uint
	if !target.Booster {
		quantities = collectSetQuantities(doc.Selection)
	}

	return scrapeSetCards(
		pageURL,
		doc.Selection,
		stateBySet,
		byLang,
		byRegionalCode,
		target.Booster,
		quantities,
	), nil
}

// scrapeSetDetails extracts names, image, type, brand, prefixes and release
// dates from every infobox present on the page.
func scrapeSetDetails(root *goquery.Selection) setScrapeData {
	data := setScrapeData{
		brand:        catalog.BrandYGO,
		prefixes:     make(map[catalog.Lang]string),
		localNames:   make(map[catalog.Lang]string),
		releaseDates: make(map[catalog.Lang]time.Time),
	}

	var inReleaseSection bool

	root.Find("table.infobox tr").Each(func(_ int, tr *goquery.Selection) {
		if above := tr.Find("th.infobox-above"); above.Length() > 0 && data.canonicalName == "" {
			data.canonicalName = cleanText(above.First().Text())
			return
		}

		if image := tr.Find("td.infobox-image"); image.Length() > 0 && data.image == nil {
			data.image = scrapeImage(image)
			return
		}

		if header := tr.Find("th.infobox-header"); header.Length() > 0 {
			inReleaseSection = strings.Contains(strings.ToLower(header.Text()), "release")
			return
		}

		label := cleanText(tr.Find("th.infobox-label").Text())
		value := tr.Find("td.infobox-data")
		if value.Length() == 0 {
			return
		}

		if lang, ok := labelToLang(label); ok {
			if span := value.Find("span[lang]"); span.Length() > 0 {
				name := cleanRubyText(span.First())
				if name != "" {
					data.localNames[lang] = name
					return
				}
			}
		}

		switch {
		case inReleaseSection:
			date := parseReleaseDate(cleanText(value.Text()))
			if date.IsZero() {
				return
			}
			for part := range strings.SplitSeq(label, ",") {
				lang, ok := labelToLang(part)
				if !ok {
					continue
				}
				if previous, exists := data.releaseDates[lang]; !exists || date.Before(previous) {
					data.releaseDates[lang] = date
				}
			}

		case label == "Medium":
			if strings.Contains(strings.ToLower(value.Text()), "rush duel") {
				data.brand = catalog.BrandYGORD
			}

		case label == "Type":
			if data.setType == "" {
				data.setType = cleanText(value.Text())
			}

		case label == "Prefix":
			value.Find("li").Each(func(_ int, item *goquery.Selection) {
				fields := strings.Fields(cleanText(item.Text()))
				if len(fields) == 0 {
					return
				}

				prefix := fields[0]
				if hasPlaceholderCode(prefix) {
					return
				}

				title, _ := item.Find("abbr").Attr("title")
				lang, ok := labelToLang(title)
				if !ok {
					lang = langFromCardCode(prefix)
				}
				if lang == "" {
					return
				}

				data.prefixes[lang] = prefix
			})
		}
	})

	if data.canonicalName == "" {
		data.canonicalName = cleanText(root.Find("h1#firstHeading").First().Text())
	}

	return data
}

// buildSetItems creates one set per released language.
func buildSetItems(
	pageURL string,
	data setScrapeData,
	release Release,
	breakdowns []breakdownSection,
) []*catalog.Set {
	setName := data.canonicalName
	if setName == "" {
		return nil
	}

	if release.Brand != "" {
		data.brand = release.Brand
	}

	strict := false
	for _, date := range data.releaseDates {
		if date.Year() >= 2021 {
			strict = true
			break
		}
	}

	setImage := imageValue(data.image)

	canonicalCode := ""
	for _, lang := range []catalog.Lang{
		catalog.LangEN,
		catalog.LangJP,
		catalog.LangAE,
		catalog.LangKR,
		catalog.LangTC,
		catalog.LangSC,
		catalog.LangFR,
		catalog.LangDE,
		catalog.LangIT,
		catalog.LangPT,
		catalog.LangES,
	} {
		if prefix := data.prefixes[lang]; prefix != "" {
			canonicalCode = strings.SplitN(prefix, "-", 2)[0]
			break
		}
	}
	if canonicalCode == "" {
		for _, prefix := range data.prefixes {
			if code := strings.SplitN(prefix, "-", 2)[0]; code != "" {
				canonicalCode = code
				break
			}
		}
	}

	setInfo := catalog.NewSetInfo(
		data.brand,
		setImages(setImage),
		canonicalPageURL(pageURL),
		data.setType,
		canonicalCode,
	)
	setInfo.SetVerifiedSource(true)

	items := make([]*catalog.Set, 0, len(data.prefixes))

	for lang, prefix := range data.prefixes {
		if prefix == "" || hasPlaceholderCode(prefix) {
			continue
		}

		date, hasDate := data.releaseDates[lang]
		if !hasDate && release.Lang == lang {
			date = release.ReleaseAt
			hasDate = !date.IsZero()
		}
		if strict && (!hasDate || !isReleaseAuthorized(date)) {
			continue
		}
		if hasDate && !isReleaseAuthorized(date) {
			continue
		}

		var setCode, setRegionalCode string
		if parts := strings.SplitN(prefix, "-", 2); len(parts) >= 2 && parts[0] != "" {
			setCode = parts[0]
			setRegionalCode = prefix
		}

		name := setName
		if localized, ok := data.localNames[lang]; ok && localized != "" {
			name = localized
		}

		description, cardsPerPack, packsPerBox := breakdownValues(breakdowns, lang)

		item := catalog.NewItem(
			&setInfo.ID,
			catalog.KindSet,
			lang,
			setCode,
			name,
			description,
			setImage,
			date,
		)
		item.Brand = data.brand
		item.ItemInfo = setInfo

		set := catalog.NewSet(*item, setRegionalCode, cardsPerPack, packsPerBox)
		set.ID = uuid.New()
		set.ItemInfo = setInfo
		set.ItemInfoID = &setInfo.ID

		items = append(items, set)
	}

	// Legacy fallback for sets without prefixes.
	if len(items) == 0 && !strict {
		name := setName
		if localized, ok := data.localNames[catalog.LangEN]; ok && localized != "" {
			name = localized
		}

		date := time.Time{}
		if releaseDate, ok := data.releaseDates[catalog.LangEN]; ok {
			date = releaseDate
		}
		if date.IsZero() {
			date = release.ReleaseAt
		}

		description, cardsPerPack, packsPerBox := breakdownValues(breakdowns, catalog.LangEN)

		item := catalog.NewItem(
			&setInfo.ID,
			catalog.KindSet,
			catalog.LangEN,
			canonicalCode,
			name,
			description,
			setImage,
			date,
		)
		item.Brand = data.brand
		item.ItemInfo = setInfo

		set := catalog.NewSet(*item, "", cardsPerPack, packsPerBox)
		set.ID = uuid.New()
		set.ItemInfo = setInfo
		set.ItemInfoID = &setInfo.ID

		items = append(items, set)
	}

	return items
}

func breakdownValues(sections []breakdownSection, lang catalog.Lang) (string, uint, uint) {
	section, ok := breakdownForLang(sections, lang)
	if !ok {
		return "", 0, 0
	}
	return section.Text, section.CardsPerPack, section.PacksPerBox
}

func setImages(image catalog.Image) []catalog.Image {
	if image.URLSmall == "" && image.URLLarge == "" {
		return nil
	}
	return []catalog.Image{image}
}

// scrapeSetCards parses every language table of the set page and appends the
// base CardPrinted rows to each set.
func scrapeSetCards(
	pageURL string,
	root *goquery.Selection,
	stateBySet map[*catalog.Set]*setState,
	byLang map[catalog.Lang]*catalog.Set,
	byRegionalCode map[string]*catalog.Set,
	booster bool,
	quantities map[string]uint,
) map[string][]*catalog.CardPrinted {
	pending := make(map[*catalog.Set][]pendingPrint)
	seen := make(map[*catalog.Set]map[string]bool)

	root.Find(".set-list").Each(func(_ int, list *goquery.Selection) {
		table := list.Find("table").First()
		if table.Length() == 0 {
			return
		}

		layout := parseSetListLayout(table)
		if layout.codeIdx < 0 {
			return
		}

		lang := layout.lang

		table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() == 0 || layout.codeIdx >= tds.Length() {
				return
			}

			code := readCellText(tds.Eq(layout.codeIdx))
			if code == "" || hasPlaceholderCode(code) {
				return
			}

			if lang == "" {
				lang = langFromCardCode(code)
			}

			set := setForPrint(byLang, byRegionalCode, lang, code)
			if set == nil {
				return
			}

			english := readCardName(tds, layout.englishIdx)
			localized := readCardName(tds, layout.localIdx)

			englishName := cleanCardName(firstNonEmpty(english.Name, localized.Name, english.Text, localized.Text))
			localizedName := cleanCardName(firstNonEmpty(localized.Name, english.Name, localized.Text, english.Text))
			if englishName == "" {
				return
			}

			href := english.URL
			if href == "" {
				href = localized.URL
			}
			cardURL := ""
			if href != "" {
				cardURL = absoluteCardURL(pageURL, href)
			}

			extendedArtwork := containsExtendedArt(english.Text) || containsExtendedArt(localized.Text)
			rarities := readRarities(tds, layout.rarityIdx)
			if len(rarities) == 0 {
				rarities = []string{""}
			}

			if seen[set] == nil {
				seen[set] = make(map[string]bool)
				if state := stateBySet[set]; state != nil {
					for _, print := range state.Prints {
						if print != nil {
							seen[set][printVariantKey(*print)] = true
						}
					}
				}
			}

			for _, rarity := range rarities {
				item := catalog.NewItem(
					nil,
					catalog.KindCard,
					set.Lang,
					code,
					localizedName,
					"",
					catalog.Image{},
					set.ReleaseAt,
				)
				item.Brand = set.Brand

				var copies uint
				if !booster {
					copies = quantityFor(quantities, code, rarity)
				}

				print := catalog.NewCardPrinted(
					*item,
					&set.ID,
					cardSerialCode(code),
					rarity,
					"",
					"",
					extendedArtwork,
					copies,
				)
				print.ID = uuid.New()

				key := printVariantKey(*print)
				if seen[set][key] {
					// A lazy Set Card Lists page may repeat the inline list.
					// Keep the existing print but refresh its copy count with
					// the (usually more reliable) list-page quantities.
					if !booster {
						if state := stateBySet[set]; state != nil {
							for _, existing := range state.Prints {
								if existing == nil || printVariantKey(*existing) != key {
									continue
								}
								existing.GuaranteedCopies = quantityFor(quantities, code, rarity)
								break
							}
						}
					}
					continue
				}
				seen[set][key] = true

				pending[set] = append(pending[set], pendingPrint{print: *print, url: cardURL})
			}
		})
	})

	printsByCardURL := make(map[string][]*catalog.CardPrinted)

	for set, rows := range pending {
		printed := make([]catalog.CardPrinted, len(rows))
		for index, row := range rows {
			printed[index] = row.print
		}

		state := stateBySet[set]
		if state != nil {
			for index := range printed {
				state.Prints = append(state.Prints, &printed[index])
			}
		}

		for index, row := range rows {
			if row.url == "" {
				continue
			}
			printsByCardURL[row.url] = append(printsByCardURL[row.url], &printed[index])
		}
	}

	return printsByCardURL
}

func materializeSets(states []*setState) []catalog.Set {
	sets := make([]catalog.Set, len(states))
	for index, state := range states {
		if state == nil || state.Set == nil {
			continue
		}

		state.Set.CardsPrinted = make([]catalog.CardPrinted, len(state.Prints))
		for printIndex, print := range state.Prints {
			if print != nil {
				state.Set.CardsPrinted[printIndex] = *print
			}
		}
		sets[index] = *state.Set
	}
	return sets
}

func setForPrint(
	byLang map[catalog.Lang]*catalog.Set,
	byRegionalCode map[string]*catalog.Set,
	lang catalog.Lang,
	code string,
) *catalog.Set {
	if regionalCode := cardRegionalCode(code); regionalCode != "" {
		if set := byRegionalCode[strings.ToUpper(regionalCode)]; set != nil {
			return set
		}
	}

	upperCode := strings.ToUpper(code)
	var best *catalog.Set
	bestLength := 0
	for setCode, set := range byRegionalCode {
		if strings.HasPrefix(upperCode, setCode) && len(setCode) > bestLength {
			best = set
			bestLength = len(setCode)
		}
	}
	if best != nil {
		return best
	}
	if set := byLang[lang]; set != nil {
		return set
	}
	if set := byLang[langFromCardCode(code)]; set != nil {
		return set
	}
	return nil
}

// parseSetListLayout finds the relevant columns of one language table.
func parseSetListLayout(table *goquery.Selection) setListLayout {
	layout := setListLayout{
		codeIdx:    -1,
		englishIdx: -1,
		localIdx:   -1,
		rarityIdx:  -1,
	}

	header := table.Find("tr").First()
	header.Find("th").Each(func(index int, cell *goquery.Selection) {
		text := normalizeHeader(cell.Text())

		switch {
		case text == "card number" || text == "number":
			layout.codeIdx = index
		case text == "english name":
			layout.englishIdx = index
		case text == "rarity":
			layout.rarityIdx = index
		case text == "name":
			if layout.englishIdx == -1 {
				layout.englishIdx = index
			}
			if layout.localIdx == -1 {
				layout.localIdx = index
			}
		case strings.HasSuffix(text, " name") && text != "english name":
			layout.localIdx = index
			if lang, ok := labelToLang(strings.TrimSuffix(text, " name")); ok {
				layout.lang = lang
			}
		}
	})

	if layout.englishIdx == -1 {
		layout.englishIdx = layout.localIdx
	}
	if layout.localIdx == -1 {
		layout.localIdx = layout.englishIdx
	}

	return layout
}

func readCardName(tds *goquery.Selection, index int) cardName {
	if index < 0 || index >= tds.Length() {
		return cardName{}
	}

	cell := tds.Eq(index)
	name := cardName{
		Text: cleanRubyText(cell),
	}

	cell.Find("a").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href, exists := link.Attr("href")
		if !exists || !isCardArticleHref(href) {
			return true
		}

		name.Name = cleanRubyText(link)
		name.URL = href
		return false
	})

	if name.Name == "" {
		name.Name = name.Text
	}

	return name
}
