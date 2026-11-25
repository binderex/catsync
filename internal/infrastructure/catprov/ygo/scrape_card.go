package ygo

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

// cardLangData holds the localized name and description of a card.
type cardLangData struct {
	Name        string
	Description string
}

// cardPrintRow is one row of a card print table before it is expanded into one
// CardPrinted per rarity.
type cardPrintRow struct {
	ReleaseAt       time.Time
	Code            string
	SetName         string
	SetCode         string
	SetRegionalCode string
	SerialCode      string
	Rarities        []string
	Lang            domain.Lang
}

// cardPrintLayout describes the columns of one card print table.
type cardPrintLayout struct {
	releaseIdx      int
	numberIdx       int
	setIdx          int
	setLocalizedIdx int
	rarityIdx       int
}

// cardPageInfo is the information extracted from the card-table markup. It is
// shared by monster, spell, trap, Rush Duel, strategy and token cards.
type cardPageInfo struct {
	Name           string
	PageTitle      string
	CardType       string
	Attribute      string
	Types          []string
	Property       string
	Archetype      string
	Level          int
	ATK            int
	DEF            int
	HasATKDEF      bool
	Password       string
	EffectTypes    []string
	Status         string
	Description    string
	Image          *domain.Image
	RushDuel       bool
	IsToken        bool
	VerifiedSource bool
}

type cardKind int

const (
	cardKindMonster cardKind = iota
	cardKindSpellTrap
	cardKindRushDuel
	cardKindStrategy
	cardKindToken
)

func (kind cardKind) String() string {
	switch kind {
	case cardKindMonster:
		return "monster"
	case cardKindSpellTrap:
		return "spell-trap"
	case cardKindRushDuel:
		return "rush-duel"
	case cardKindStrategy:
		return "strategy"
	case cardKindToken:
		return "token"
	default:
		return "unknown"
	}
}

// cardScrapeResult is the result of scraping a card page. Prints is populated
// from the card print tables and carries the resolved card info for direct
// callers and tests. The release flow uses Info, Translations and GalleryURL to
// enrich the prints that already came from the set page.
type cardScrapeResult struct {
	Info         *domain.Item
	Prints       []domain.Item
	Translations map[domain.Lang]cardLangData
	GalleryURL   string
	Image        domain.Image
	Kind         cardKind
	OK           bool
}

// scrapeCard parses a card page. info is optional: when it is nil the card info
// is resolved from YGOPRODeck first and from the card page as a fallback.
func (p *Provider) scrapeCard(
	ctx context.Context,
	pageURL string,
	body []byte,
	info *domain.Item,
) (cardScrapeResult, error) {
	result := cardScrapeResult{
		Kind:         cardKindMonster,
		Translations: make(map[domain.Lang]cardLangData),
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return result, err
	}

	root := doc.Selection
	pageInfo, hasInfo := scrapeCardPageInfo(root)
	if hasInfo {
		result.Kind = pageInfo.kind()
		result.Image = imageValue(pageInfo.Image)
	}

	name := firstNonEmpty(pageInfo.Name, pageInfo.PageTitle, cardNameFromURL(pageURL))
	source := canonicalPageURL(pageURL)

	if info == nil {
		info = p.resolveCardItemInfo(ctx, source, name, pageInfo, hasInfo)
	} else if hasInfo {
		info.VerifiedSource = pageInfo.VerifiedSource
	}

	if info != nil {
		result.Info = info
		if !hasInfo {
			info.VerifiedSource = true
		}
	}

	translations := scrapeCardTranslations(root)
	for lang, localized := range scrapeCardAboveNames(root) {
		data := translations[lang]
		if data.Name == "" {
			data.Name = localized
			translations[lang] = data
		}
	}
	if _, exists := translations[domain.LangEN]; !exists {
		translations[domain.LangEN] = cardLangData{
			Name:        name,
			Description: pageInfo.Description,
		}
	}
	result.Translations = translations

	result.GalleryURL = scrapeGalleryURL(pageURL, root)

	rows, _ := scrapeCardPrintRows(pageURL, root)
	result.Prints = buildCardPagePrints(rows, translations, name, result, info)

	result.OK = hasInfo || len(rows) > 0
	return result, nil
}

func (p *Provider) resolveCardItemInfo(
	ctx context.Context,
	pageURL string,
	name string,
	pageInfo cardPageInfo,
	hasPageInfo bool,
) *domain.Item {
	// Rush Duel, strategy and token cards are not in the YGOPRODeck database
	// in the shape used by this provider. Scraping their page is authoritative.
	if hasPageInfo && (pageInfo.RushDuel || pageInfo.IsToken || pageInfo.kind() == cardKindStrategy) {
		return pageInfo.toItem(pageURL)
	}

	if name != "" {
		if info, err := p.findItemInfoYGOPRODECK(ctx, name, pageURL); err == nil {
			info.VerifiedSource = true
			if hasPageInfo && !pageInfo.VerifiedSource {
				info.VerifiedSource = false
			}
			return info
		}
	}

	if hasPageInfo {
		return pageInfo.toItem(pageURL)
	}

	info := newCardItem(domain.BrandYGO, nil, pageURL, "", "", nil)
	info.VerifiedSource = true
	return info
}

func buildCardPagePrints(
	rows []cardPrintRow,
	translations map[domain.Lang]cardLangData,
	fallbackName string,
	result cardScrapeResult,
	info *domain.Item,
) []domain.Item {
	if len(rows) == 0 {
		return nil
	}

	brand := domain.BrandYGO
	if info != nil && info.Brand != "" {
		brand = info.Brand
	}

	seen := make(map[string]bool)
	prints := make([]domain.Item, 0, len(rows))

	for _, row := range rows {
		if !isReleaseAuthorized(row.ReleaseAt) {
			continue
		}

		for _, rarity := range row.Rarities {
			name := fallbackName
			description := ""
			if data, ok := translations[row.Lang]; ok {
				if data.Name != "" {
					name = data.Name
				}
				description = data.Description
			}

			image := result.Image
			if image.URLSmall == "" && image.URLLarge == "" {
				if images := infoImages(info); len(images) > 0 {
					image = images[0]
				}
			}

			print := domain.Item{
				Kind:        domain.KindCard,
				Brand:       brand,
				Lang:        row.Lang,
				Code:        row.Code,
				Name:        name,
				Description: description,
				Image:       image,
				ReleaseAt:   row.ReleaseAt,
				Properties:  make(map[string]any),
			}
			print.ID = uuid.NewString()
			print.Properties[domain.PropertySerialCode] = row.SerialCode
			print.Rarity = rarity
			applyCardInfo(&print, info)

			key := printVariantKey(print)
			if seen[key] {
				continue
			}
			seen[key] = true
			prints = append(prints, print)
		}
	}

	return prints
}

// infoImages returns the shared images of a card info item.
func infoImages(info *domain.Item) []domain.Image {
	if info == nil {
		return nil
	}
	return info.PropertyImages()
}

// applyCardInfo copies the shared card info fields into a print item.
func applyCardInfo(print *domain.Item, info *domain.Item) {
	if info == nil {
		return
	}

	if print.Brand == "" || info.Brand != "" {
		print.Brand = info.Brand
	}
	print.Source = info.Source
	print.VerifiedSource = info.VerifiedSource
	if print.Properties == nil {
		print.Properties = make(map[string]any)
	}
	for key, value := range info.Properties {
		if key == domain.PropertyVariant {
			continue
		}
		print.Properties[key] = value
	}
}

// scrapeCardPageInfo parses div.card-table.*-card, the markup used by modern
// Yugipedia card pages.
func scrapeCardPageInfo(root *goquery.Selection) (cardPageInfo, bool) {
	table := root.Find("div.card-table").First()
	if table.Length() == 0 {
		return cardPageInfo{}, false
	}

	info := cardPageInfo{
		Name:           cleanText(table.Find(".heading div").First().Text()),
		PageTitle:      cleanText(root.Find("h1#firstHeading .mw-page-title-main").First().Text()),
		Description:    cleanRubyText(table.Find(".lore").First()),
		Image:          scrapeImage(table.Find(".cardtable-main_image-wrapper").First()),
		RushDuel:       isRushDuelCard(table),
		IsToken:        table.HasClass("token-card") || table.Find(`a[href="/wiki/Monster_Token"], a[title="Monster Token"]`).Length() > 0,
		VerifiedSource: true,
	}

	if info.PageTitle == "" {
		info.PageTitle = cleanText(root.Find("h1#firstHeading").First().Text())
	}
	if info.Name == "" {
		info.Name = info.PageTitle
	}

	root.Find("div.hatnote, div[role='note']").Each(func(_ int, note *goquery.Selection) {
		text := strings.ToLower(cleanText(note.Text()))
		if strings.Contains(text, "english name given is not official") {
			info.VerifiedSource = false
		}
	})

	table.Find(".innertable tr").Each(func(_ int, row *goquery.Selection) {
		label := cleanText(row.Find("th").Text())
		cell := row.Find("td")
		if cell.Length() == 0 {
			return
		}

		switch label {
		case "Card type":
			info.CardType = firstLinkText(cell)
		case "Attribute":
			info.Attribute = firstLinkText(cell)
		case "Types":
			info.Types = linkTexts(cell)
		case "Property":
			info.Property = firstLinkText(cell)
		case "Archetype":
			info.Archetype = firstLinkText(cell)
		case "Level":
			info.Level = firstNumber(firstLinkText(cell))
		case "ATK / DEF":
			info.ATK, info.DEF = firstTwoNumbers(cleanText(cell.Text()))
			info.HasATKDEF = true
		case "Password":
			info.Password = firstLinkText(cell)
		case "Effect types":
			info.EffectTypes = linkTexts(cell)
		case "Status":
			cleanCell := cell.Clone()
			cleanCell.Find("style, script").Remove()
			info.Status = cleanText(cleanCell.Text())
		}
	})

	if info.IsToken && !strings.EqualFold(info.CardType, "Token") && !containsText(info.Types, "Token") {
		info.Types = append(info.Types, "Token")
	}

	return info, true
}

func (info cardPageInfo) kind() cardKind {
	switch {
	case info.IsToken:
		return cardKindToken
	case strings.EqualFold(info.CardType, "Strategy"):
		return cardKindStrategy
	case info.RushDuel:
		return cardKindRushDuel
	case strings.EqualFold(info.CardType, "Spell"), strings.EqualFold(info.CardType, "Trap"):
		return cardKindSpellTrap
	default:
		return cardKindMonster
	}
}

func (info cardPageInfo) cardInfo() map[string]any {
	data := make(map[string]any)

	if info.CardType != "" {
		data["card_type"] = info.CardType

		cardTypes := []string{info.CardType}
		switch {
		case len(info.Types) > 0:
			cardTypes = append(cardTypes, info.Types...)
		case info.Property != "":
			cardTypes = append(cardTypes, info.Property)
		}
		data["card_types"] = cardTypes
	}

	if info.Attribute != "" {
		data["attribute"] = info.Attribute
	}
	if len(info.Types) > 0 {
		data["race"] = info.Types[0]
	}
	if info.Property != "" {
		data["property"] = info.Property
	}
	if info.Archetype != "" {
		data["archetype"] = info.Archetype
	}
	if info.Level > 0 {
		data["level"] = info.Level
	}
	if info.HasATKDEF {
		data["atk"] = info.ATK
		data["def"] = info.DEF
	}
	if info.Password != "" {
		data["password"] = info.Password
	}
	if len(info.EffectTypes) > 0 {
		data["effect_types"] = info.EffectTypes
	}
	if info.Status != "" {
		data["status"] = info.Status
	}

	return data
}

func (info cardPageInfo) brand() domain.Brand {
	if info.RushDuel {
		return domain.BrandYGORD
	}
	return domain.BrandYGO
}

func (info cardPageInfo) toItem(source string) *domain.Item {
	var images []domain.Image
	if info.Image != nil {
		images = append(images, *info.Image)
	}

	cardInfo := info.cardInfo()
	cardTypes := ""
	if values, ok := cardInfo["card_types"].([]string); ok {
		cardTypes = strings.Join(values, " ")
	}

	item := newCardItem(
		info.brand(),
		images,
		source,
		info.Archetype,
		cardTypes,
		cardInfo,
	)
	item.VerifiedSource = info.VerifiedSource

	return item
}

// newCardItem builds a domain.Item that carries the shared card information.
// It is used as a template that is copied onto card prints.
func newCardItem(
	brand domain.Brand,
	images []domain.Image,
	source, archetype, cardTypes string,
	cardInfo map[string]any,
) *domain.Item {
	item := &domain.Item{
		Brand:          brand,
		Source:         source,
		VerifiedSource: true,
		Properties:     make(map[string]any),
	}
	if len(images) > 0 {
		item.SetPropertyImages(images)
	}
	if archetype != "" {
		item.SetProperty(domain.PropertyArchetype, archetype)
	}
	if cardTypes != "" {
		item.SetProperty(domain.PropertyCardTypes, cardTypes)
	}
	for key, value := range cardInfo {
		if key == domain.PropertyArchetype || key == domain.PropertyCardTypes {
			continue
		}
		item.SetProperty(key, value)
	}
	return item
}

func isRushDuelCard(table *goquery.Selection) bool {
	if table.Find(`a[href="/wiki/Yu-Gi-Oh!_Rush_Duel"], a[title="Yu-Gi-Oh! Rush Duel"]`).Length() > 0 {
		return true
	}
	return strings.Contains(strings.ToLower(table.Text()), "rush duel")
}

func firstLinkText(cell *goquery.Selection) string {
	if link := cell.Find("a").First(); link.Length() > 0 {
		if text := cleanRubyText(link); text != "" {
			return text
		}
	}
	return cleanRubyText(cell)
}

func linkTexts(cell *goquery.Selection) []string {
	var values []string

	cell.Find("a").Each(func(_ int, link *goquery.Selection) {
		if text := cleanRubyText(link); text != "" {
			values = append(values, text)
		}
	})

	if len(values) > 0 {
		return values
	}

	for _, part := range strings.Split(cleanRubyText(cell), "/") {
		if value := cleanText(part); value != "" {
			values = append(values, value)
		}
	}

	return values
}

func firstNumber(value string) int {
	match := numberRegex.FindString(value)
	if match == "" {
		return 0
	}
	number, _ := strconv.Atoi(match)
	return number
}

func firstTwoNumbers(value string) (int, int) {
	matches := numberRegex.FindAllString(value, 2)
	if len(matches) < 2 {
		return 0, 0
	}

	first, _ := strconv.Atoi(matches[0])
	second, _ := strconv.Atoi(matches[1])
	return first, second
}

func containsText(values []string, value string) bool {
	for _, current := range values {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

// scrapeCardTranslations extracts localized names and descriptions.
func scrapeCardTranslations(root *goquery.Selection) map[domain.Lang]cardLangData {
	translations := make(map[domain.Lang]cardLangData)

	root.Find("table.wikitable tr").Each(func(_ int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() < 2 {
			return
		}

		span := tds.Eq(0).Find("span[lang]")
		langAttr, exists := span.Attr("lang")
		if !exists {
			return
		}

		lang, ok := labelToLang(langAttr)
		if !ok {
			return
		}

		cleanDescription := tds.Eq(1).Clone()
		cleanDescription.Find("br").ReplaceWithHtml("\n")

		translations[lang] = cardLangData{
			Name:        cleanRubyText(span.First()),
			Description: cleanText(cleanDescription.Text()),
		}
	})

	return translations
}

// scrapeCardAboveNames extracts localized names from the compact dl shown
// above the card image, such as the Japanese name.
func scrapeCardAboveNames(root *goquery.Selection) map[domain.Lang]string {
	names := make(map[domain.Lang]string)

	root.Find("div.card-table .above dl").Each(func(_ int, list *goquery.Selection) {
		var currentLang domain.Lang
		hasLang := false

		list.Children().Each(func(_ int, node *goquery.Selection) {
			switch goquery.NodeName(node) {
			case "dt":
				lang, ok := labelToLang(cleanText(node.Text()))
				currentLang = lang
				hasLang = ok
			case "dd":
				if !hasLang {
					return
				}
				span := node.Find("span[lang]").First()
				if span.Length() == 0 {
					return
				}
				if name := cleanRubyText(span); name != "" {
					names[currentLang] = name
				}
			}
		})
	})

	return names
}

// scrapeCardPrintRows extracts every print row and the unique set links.
func scrapeCardPrintRows(pageURL string, root *goquery.Selection) ([]cardPrintRow, []string) {
	var (
		rows     []cardPrintRow
		setURLs  []string
		seenSets = make(map[string]bool)
	)

	root.Find("table[id^='cts--']").Each(func(_ int, table *goquery.Selection) {
		tableID, _ := table.Attr("id")
		langToken := strings.TrimPrefix(tableID, "cts--")

		lang, ok := labelToLang(langToken)
		if !ok {
			lang = ""
		}

		layout := parseCardPrintLayout(table)
		if layout.numberIdx < 0 {
			return
		}

		table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() == 0 || layout.numberIdx >= tds.Length() {
				return
			}

			row := cardPrintRow{
				Lang: lang,
			}

			if layout.releaseIdx >= 0 && layout.releaseIdx < tds.Length() {
				row.ReleaseAt = parseReleaseDate(cleanText(tds.Eq(layout.releaseIdx).Text()))
			}

			row.Code = readCellText(tds.Eq(layout.numberIdx))
			if row.Code == "" || hasPlaceholderCode(row.Code) {
				return
			}
			if row.Lang == "" {
				row.Lang = langFromCardCode(row.Code)
			}

			row.SetName = readCardPrintSetName(tds, layout)
			row.SetCode = cardSetCode(row.Code)
			row.SetRegionalCode = cardRegionalCode(row.Code)
			row.SerialCode = cardSerialCode(row.Code)
			row.Rarities = readRarities(tds, layout.rarityIdx)
			if len(row.Rarities) == 0 {
				row.Rarities = []string{""}
			}

			for _, index := range []int{layout.setLocalizedIdx, layout.setIdx} {
				if index < 0 || index >= tds.Length() {
					continue
				}

				link := tds.Eq(index).Find("a").First()
				href, exists := link.Attr("href")
				if !exists || !isCardArticleHref(href) {
					continue
				}

				setURL := absoluteCardURL(pageURL, href)
				if setURL != "" && !seenSets[setURL] {
					seenSets[setURL] = true
					setURLs = append(setURLs, setURL)
				}
				break
			}

			rows = append(rows, row)
		})
	})

	return rows, setURLs
}

func parseCardPrintLayout(table *goquery.Selection) cardPrintLayout {
	layout := cardPrintLayout{
		releaseIdx:      -1,
		numberIdx:       -1,
		setIdx:          -1,
		setLocalizedIdx: -1,
		rarityIdx:       -1,
	}

	header := table.Find("tr").First()
	header.Find("th").Each(func(index int, cell *goquery.Selection) {
		text := normalizeHeader(cell.Text())
		class, _ := cell.Attr("class")
		class = strings.ToLower(class)

		switch {
		case strings.Contains(class, "cts__header--release") || text == "release":
			layout.releaseIdx = index
		case strings.Contains(class, "cts__header--number") || text == "number":
			layout.numberIdx = index
		case strings.Contains(class, "cts__header--set-localized") || strings.Contains(text, "name"):
			layout.setLocalizedIdx = index
		case strings.Contains(class, "cts__header--set") || text == "set":
			layout.setIdx = index
		case strings.Contains(class, "cts__header--rarity") || text == "rarity":
			layout.rarityIdx = index
		}
	})

	if layout.setLocalizedIdx < 0 {
		layout.setLocalizedIdx = layout.setIdx
	}
	if layout.setIdx < 0 {
		layout.setIdx = layout.setLocalizedIdx
	}

	return layout
}

func readCardPrintSetName(tds *goquery.Selection, layout cardPrintLayout) string {
	if layout.setLocalizedIdx >= 0 && layout.setLocalizedIdx < tds.Length() {
		if name := cleanRubyText(tds.Eq(layout.setLocalizedIdx)); name != "" {
			return name
		}
	}

	if layout.setIdx >= 0 && layout.setIdx < tds.Length() {
		return cleanRubyText(tds.Eq(layout.setIdx))
	}

	return ""
}
