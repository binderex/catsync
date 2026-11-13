package catprov

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/binderex/catsync/internal/catalog"
)

var (
	cardCodeRegex = regexp.MustCompile(`^[A-Za-z0-9]+-[A-Za-z0-9]+$`)
	langMap       = map[string]catalog.Lang{
		"fr":      catalog.LangFR,
		"de":      catalog.LangDE,
		"it":      catalog.LangIT,
		"pt":      catalog.LangPT,
		"es":      catalog.LangES,
		"ja":      catalog.LangJP,
		"ko":      catalog.LangKR,
		"zh-hans": catalog.LangSC,
		"zh-hant": catalog.LangTC,
		"en":      catalog.LangEN,
	}
	validLangs = map[catalog.Lang]bool{
		catalog.LangEN: true,
		catalog.LangFR: true,
		catalog.LangDE: true,
		catalog.LangIT: true,
		catalog.LangPT: true,
		catalog.LangES: true,
		catalog.LangJP: true,
		catalog.LangAE: true,
		catalog.LangKR: true,
		catalog.LangTC: true,
		catalog.LangSC: true,
	}
)

type galleryItem struct {
	Code            string
	Rarity          string
	RarityCode      string
	Edition         string
	ExtendedArtwork bool
	Image           *catalog.Image
}

// mappedGalleryBoxes extracts every gallery box from a card gallery page,
// including its code, rarity, edition, extended artwork flag and image.
func mappedGalleryBoxes(doc *goquery.Document) []galleryItem {
	var items []galleryItem

	doc.Find("li.gallerybox").Each(func(_ int, box *goquery.Selection) {
		galleryText := box.Find(".gallerytext")
		if galleryText.Length() == 0 {
			return
		}

		p := galleryText.Find("p")
		if p.Length() == 0 {
			p = galleryText
		}

		links := p.Find("a")
		if links.Length() == 0 || strings.Contains(p.Text(), "Official Proxy") {
			return
		}
		code := strings.TrimSpace(links.First().Text())
		if !cardCodeRegex.MatchString(code) {
			return
		}

		var rarity, rarityCode, edition string

		links.Each(func(i int, a *goquery.Selection) {
			if i == 0 || a.ParentsFiltered("i").Length() > 0 {
				return
			}

			href, _ := a.Attr("href")
			title, _ := a.Attr("title")
			text := strings.TrimSpace(a.Text())

			if strings.Contains(href, "Edition") || strings.Contains(title, "Edition") || strings.Contains(text, "Edition") {
				if text != "" {
					edition = text
				} else {
					edition = title
				}
				return
			}

			if title != "" && title != "Official Proxy" {
				rarity = title
				rarityCode = text
			}
		})

		img := mappedImage(box.Find(".thumb"))

		items = append(items, galleryItem{
			Code:            code,
			Rarity:          rarity,
			RarityCode:      rarityCode,
			Edition:         edition,
			ExtendedArtwork: strings.Contains(strings.ToLower(galleryText.Text()), "extended artwork"),
			Image:           img,
		})
	})

	return items
}

// mappedImage extracts the small and large image URLs from the provided
// selection, deriving the full-size URL from the thumbnail path.
func mappedImage(sel *goquery.Selection) *catalog.Image {
	img := sel.Find("img")
	if img.Length() == 0 {
		return nil
	}
	src, exists := img.Attr("src")
	if !exists || src == "" {
		return nil
	}

	smallURL := src
	largeURL := src

	if before, after, ok := strings.Cut(src, "/thumb/"); ok {
		if lastSlash := strings.LastIndex(after, "/"); lastSlash != -1 {
			origPath := after[:lastSlash]
			largeURL = strings.TrimRight(before, "/") + "/" + origPath
		}
	}

	return &catalog.Image{
		URLSmall: smallURL,
		URLLarge: largeURL,
	}
}

// applyGalleryData merges gallery metadata into a catalog card print.
func applyGalleryData(item *catalog.CatalogItem, g galleryItem) {
	if g.Edition != "" {
		item.Edition = g.Edition
	}
	if g.RarityCode != "" {
		item.RarityCode = g.RarityCode
	}
	if item.Rarity == "" && g.Rarity != "" {
		item.Rarity = g.Rarity
	}
	if g.Image != nil {
		item.PrintImage = *g.Image
	}
	item.ExtendedArtwork = g.ExtendedArtwork
}

// mappedPrints extracts every released print of a card from its Yugipedia page.
//
// The page contains translation rows inside "table.wikitable" and one
// "table[id^='cts--']" per language with the release date, card number, set and
// rarity columns. Placeholder codes such as MZMC-EN0?? are discarded, and
// prints whose own release date is more than one day in the future are skipped.
func mappedPrints(doc *goquery.Document, card catalog.CatalogItem, now time.Time) []catalog.CatalogItem {
	type cardLang struct {
		Name        string
		Description string
	}
	cardLangs := make(map[catalog.Lang]cardLang)

	doc.Find("table.wikitable tr").Each(func(_ int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() < 2 {
			return
		}

		span := tds.Eq(0).Find("span[lang]")
		langAttr, exists := span.Attr("lang")
		if !exists {
			return
		}

		lang, ok := langMap[strings.ToLower(langAttr)]
		if !ok {
			return
		}

		cleanSpan := span.Clone()
		cleanSpan.Find("rt, rp").Remove()

		cleanDesc := tds.Eq(1).Clone()
		cleanDesc.Find("br").ReplaceWithHtml("\n")

		cardLangs[lang] = cardLang{
			Name:        strings.TrimSpace(cleanSpan.Text()),
			Description: strings.TrimSpace(cleanDesc.Text()),
		}
	})

	cardLangs[catalog.LangEN] = cardLang{
		Name:        card.Name,
		Description: card.Description,
	}

	maxAllowedDate := now.Add(24 * time.Hour)
	items := make([]catalog.CatalogItem, 0)

	doc.Find("table[id^='cts--']").Each(func(_ int, table *goquery.Selection) {
		tableID, _ := table.Attr("id")
		langToken := strings.TrimPrefix(tableID, "cts--")
		langCode := catalog.Lang(langToken)
		// Yugipedia still names the Spanish table "SP"; the catalog uses ES.
		if langToken == "SP" {
			langCode = catalog.LangES
		}
		if !validLangs[langCode] {
			return
		}

		var (
			releaseIdx      = -1
			numberIdx       = -1
			setIdx          = -1
			setLocalizedIdx = -1
			rarityIdx       = -1
		)

		headerRow := table.Find("tr").First()
		headerRow.Find("th").Each(func(i int, th *goquery.Selection) {
			text := strings.ToLower(strings.TrimSpace(th.Text()))
			class, _ := th.Attr("class")
			class = strings.ToLower(class)

			switch {
			case strings.Contains(class, "cts__header--release") || text == "release":
				releaseIdx = i
			case strings.Contains(class, "cts__header--number") || text == "number":
				numberIdx = i
			case strings.Contains(class, "cts__header--set-localized") || strings.Contains(text, "name"):
				setLocalizedIdx = i
			case strings.Contains(class, "cts__header--set") || text == "set":
				setIdx = i
			case strings.Contains(class, "cts__header--rarity") || text == "rarity":
				rarityIdx = i
			}
		})

		table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() == 0 {
				return
			}

			var (
				releasedAt time.Time
				codeStr    string
				rawSetName string
			)

			if releaseIdx >= 0 && releaseIdx < tds.Length() {
				rawDate := strings.TrimSpace(tds.Eq(releaseIdx).Text())
				if rawDate != "" {
					releasedAt = mappedReleaseDate(rawDate)
				}
			}

			if numberIdx >= 0 && numberIdx < tds.Length() {
				codeStr = strings.TrimSpace(tds.Eq(numberIdx).Text())
			}

			// Unreleased cards are often listed with placeholder codes such as
			// MZMC-EN0??. They must never reach the catalog.
			if hasPlaceholderCode(codeStr) {
				return
			}

			if setLocalizedIdx >= 0 && setLocalizedIdx < tds.Length() {
				cleanSet := tds.Eq(setLocalizedIdx).Clone()
				cleanSet.Find("rt, rp").Remove()
				rawSetName = strings.TrimSpace(cleanSet.Text())
			}
			if rawSetName == "" && setIdx >= 0 && setIdx < tds.Length() {
				rawSetName = strings.TrimSpace(tds.Eq(setIdx).Text())
			}

			var rarities []string
			if rarityIdx >= 0 && rarityIdx < tds.Length() {
				rarityTd := tds.Eq(rarityIdx)
				rarityLinks := rarityTd.Find("a")
				if rarityLinks.Length() > 0 {
					rarityLinks.Each(func(_ int, a *goquery.Selection) {
						r := strings.TrimSpace(a.Text())
						if r != "" {
							rarities = append(rarities, r)
						}
					})
				} else {
					r := strings.TrimSpace(rarityTd.Text())
					if r != "" {
						rarities = append(rarities, r)
					}
				}
			}

			// Split "LED2-SP022" into set code, regional code and serial.
			var setCode, setRegionalCode, serialCode string
			if codeStr != "" {
				if parts := strings.Split(codeStr, "-"); len(parts) >= 2 {
					if parts[0] != "" {
						setCode = parts[0]
						setRegionalCode = fmt.Sprintf("%s-%s", parts[0], langToken)
					}
					serialCode = strings.TrimPrefix(parts[1], langToken)
				}
			}

			for _, rarity := range rarities {
				if !releasedAt.IsZero() && releasedAt.After(maxAllowedDate) {
					log.Printf("skipping card print %s (%s): release date is more than 1 day in the future", card.Name, card.ExternalID)
					continue
				}

				item := card
				item.Lang = langCode
				item.Code = codeStr
				item.SerialCode = serialCode
				item.ReleasedAt = releasedAt
				item.SetName = rawSetName
				item.SetCode = setCode
				item.SetRegionalCode = setRegionalCode
				item.Rarity = rarity

				if langData, ok := cardLangs[langCode]; ok {
					item.Name = langData.Name
					if langData.Description != "" {
						item.Description = langData.Description
					}
				}

				items = append(items, item)
			}
		})
	})

	return items
}

// hasPlaceholderCode reports whether a scraped code still contains unresolved
// placeholders (e.g. MZMC-EN0??), which means the product is not released yet.
func hasPlaceholderCode(code string) bool {
	return strings.Contains(code, "?")
}

// mappedSetCarList parses the card list of a Yugipedia set page, fills
// SetImage/SetType for every provided card and, for non-booster products,
// counts copies to populate GuaranteedCopies.
func mappedSetCarList(doc *goquery.Document, set catalog.CatalogItem, cards []catalog.CatalogItem) []catalog.CatalogItem {
	isBonusHeading := func(sel *goquery.Selection) bool {
		headline := strings.ToLower(strings.TrimSpace(sel.Find("span.mw-headline").Text()))
		if headline == "" {
			headline = strings.ToLower(strings.TrimSpace(sel.Find("h3").Text()))
		}
		if headline == "" {
			headline = strings.ToLower(strings.TrimSpace(sel.Text()))
		}
		return strings.Contains(headline, "bonus")
	}

	booster := false
	doc.Find("li, a, p").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if strings.Contains(text, "booster pack") ||
			strings.Contains(text, "mega pack") ||
			strings.Contains(text, "mini pack") {
			booster = true
			return false
		}
		return true
	})

	// If it is not a booster pack, extract card quantities from the tabber table.
	// Card counts are only tallied in sections other than "Bonus cards".
	qty := make(map[string]uint)
	if !booster {
		var (
			currentSectionIsBonus bool
			tabContainer          = doc.Find(".set-list-tab").First()
		)

		if tabContainer.Length() == 0 {
			tabContainer = doc.Find(".tabbertab").First()
		}

		tabContainer.Children().Each(func(_ int, node *goquery.Selection) {
			switch goquery.NodeName(node) {
			case "h3":
				currentSectionIsBonus = isBonusHeading(node)

			case "div":
				if node.HasClass("mw-heading") {
					currentSectionIsBonus = isBonusHeading(node)
					return
				}
				if !node.HasClass("set-list") || currentSectionIsBonus {
					return
				}
				node.Find("table tbody tr").Each(func(_ int, tr *goquery.Selection) {
					tds := tr.Find("td")
					if tds.Length() == 0 {
						return
					}
					code := strings.TrimSpace(tds.First().Find("a").First().Text())
					if code == "" {
						code = strings.TrimSpace(tds.First().Text())
					}
					if code == "" || hasPlaceholderCode(code) {
						return
					}

					var rarities []string
					if tds.Length() > 2 {
						rarityLinks := tds.Eq(2).Find("a")
						if rarityLinks.Length() > 0 {
							rarityLinks.Each(func(_ int, a *goquery.Selection) {
								r := strings.TrimSpace(a.Text())
								if r != "" {
									rarities = append(rarities, r)
								}
							})
						} else {
							r := strings.TrimSpace(tds.Eq(2).Text())
							if r != "" {
								rarities = append(rarities, r)
							}
						}
					}

					codeKey := strings.ToUpper(code)
					if len(rarities) == 0 {
						qty[codeKey]++
					} else {
						for _, r := range rarities {
							rKey := strings.ToLower(strings.TrimSpace(r))
							qty[codeKey+"|"+rKey]++
						}
					}
				})
			}
		})
	}

	for i, c := range cards {
		cards[i].SetImage = set.SetImage
		cards[i].SetType = set.SetType

		if !booster && c.Code != "" {
			codeKey := strings.ToUpper(c.Code)
			var copies uint

			if c.Rarity != "" {
				rKey := strings.ToLower(strings.TrimSpace(c.Rarity))
				copies = qty[codeKey+"|"+rKey]
			} else {
				copies = qty[codeKey]
			}

			cards[i].GuaranteedCopies = copies
		}
	}

	return cards
}

// mappedSetInfo parses a Yugipedia set infobox and returns one CatalogItem per
// released language, each with its own release date.
func mappedSetInfo(doc *goquery.Document, externalID string, now time.Time) []catalog.CatalogItem {
	const releaseThreshold = 2 * 24 * time.Hour

	var (
		setName          string
		setType          string
		setImage         *catalog.Image
		prefixes         = make(map[catalog.Lang]string)
		localNames       = make(map[catalog.Lang]string)
		releaseDates     = make(map[catalog.Lang]time.Time)
		inNamesSection   bool
		inReleaseSection bool
	)

	doc.Find("table.infobox tr").Each(func(_ int, tr *goquery.Selection) {
		if above := tr.Find("th.infobox-above"); above.Length() > 0 && setName == "" {
			setName = strings.TrimSpace(above.Text())
			return
		}

		if imgTd := tr.Find("td.infobox-image"); imgTd.Length() > 0 && setImage == nil {
			setImage = mappedImage(imgTd)
			return
		}

		if hdr := tr.Find("th.infobox-header"); hdr.Length() > 0 {
			hdrText := strings.ToLower(strings.TrimSpace(hdr.Text()))
			inNamesSection = strings.Contains(hdrText, "name")
			inReleaseSection = strings.Contains(hdrText, "release")
			return
		}

		label := strings.TrimSpace(tr.Find("th.infobox-label").Text())
		td := tr.Find("td.infobox-data")
		if td.Length() == 0 {
			return
		}

		switch {
		case inNamesSection:
			lang, ok := labelToLang(label)
			if ok {
				span := td.Find("span[lang]")
				var name string
				if span.Length() > 0 {
					name = strings.TrimSpace(span.First().Text())
				} else {
					td.Find("rt, rp").Remove()
					name = strings.TrimSpace(td.Text())
				}
				if name != "" {
					localNames[lang] = name
				}
			}

		case inReleaseSection:
			date := mappedReleaseDate(strings.TrimSpace(td.Text()))
			if date.IsZero() {
				return
			}
			for _, part := range strings.Split(label, ",") {
				lang, ok := labelToLang(part)
				if !ok {
					continue
				}
				if prev, ok := releaseDates[lang]; !ok || date.Before(prev) {
					releaseDates[lang] = date
				}
			}

		case label == "Type":
			a := td.Find("a").First()
			if a.Length() > 0 {
				setType = strings.TrimSpace(a.Text())
			} else {
				setType = strings.TrimSpace(td.Text())
			}

		case label == "Prefix":
			td.Find("li").Each(func(_ int, li *goquery.Selection) {
				abbr := li.Find("abbr")
				langAttr, exists := abbr.Attr("title")
				if !exists {
					return
				}
				lang, ok := labelToLang(langAttr)
				if !ok {
					return
				}
				raw := strings.Fields(strings.TrimSpace(li.Text()))
				if len(raw) == 0 {
					return
				}
				prefix := raw[0]
				if hasPlaceholderCode(prefix) {
					return
				}
				prefixes[lang] = prefix
			})
		}
	})

	if setName == "" {
		setName = externalID
	}

	strict := false
	for _, date := range releaseDates {
		if date.Year() >= 2021 {
			strict = true
			break
		}
	}

	var setImageValue catalog.Image
	if setImage != nil {
		setImageValue = *setImage
	}

	items := make([]catalog.CatalogItem, 0, len(prefixes))
	for lang, prefix := range prefixes {
		if hasPlaceholderCode(prefix) {
			continue
		}

		date, hasDate := releaseDates[lang]
		if strict && (!hasDate || date.After(now.Add(releaseThreshold))) {
			continue
		}

		var setCode, setRegionalCode string
		if parts := strings.SplitN(prefix, "-", 2); len(parts) >= 2 && parts[0] != "" {
			setCode = parts[0]
			setRegionalCode = prefix
		}

		name := setName
		if n, ok := localNames[lang]; ok {
			name = n
		}

		releasedAt := time.Time{}
		if hasDate {
			releasedAt = date
		}

		items = append(items, catalog.CatalogItem{
			ExternalID:      externalID,
			Name:            name,
			SetName:         name,
			SetType:         setType,
			SetCode:         setCode,
			SetRegionalCode: setRegionalCode,
			SetImage:        setImageValue,
			Lang:            lang,
			Brand:           catalog.BrandYGO,
			Type:            catalog.CatalogTypeSet,
			ReleasedAt:      releasedAt,
		})
	}

	// Legacy fallback: sets without prefixes keep behaving as before.
	if len(items) == 0 && !strict {
		name := setName
		if n, ok := localNames[catalog.LangEN]; ok {
			name = n
		}
		releasedAt := time.Time{}
		if date, ok := releaseDates[catalog.LangEN]; ok {
			releasedAt = date
		}
		items = append(items, catalog.CatalogItem{
			ExternalID: externalID,
			Name:       name,
			SetName:    name,
			SetType:    setType,
			SetImage:   setImageValue,
			Lang:       catalog.LangEN,
			Brand:      catalog.BrandYGO,
			Type:       catalog.CatalogTypeSet,
			ReleasedAt: releasedAt,
		})
	}

	return items
}

// mergeGalleryIntoPrints applies gallery metadata to the prints extracted from
// the card page. Gallery boxes that share code and rarity but differ in edition
// or extended artwork produce additional prints, because those fields are part
// of the catalog identity.
func mergeGalleryIntoPrints(prints []catalog.CatalogItem, gallery []galleryItem) []catalog.CatalogItem {
	matched := make(map[int]bool)
	seen := make(map[string]bool, len(prints))
	for i := range prints {
		seen[printVariantKey(prints[i])] = true
	}

	var extra []catalog.CatalogItem

	for _, g := range gallery {
		matchIdx := -1
		for i := range prints {
			if prints[i].Code == "" || !strings.EqualFold(prints[i].Code, g.Code) {
				continue
			}
			if g.Rarity != "" && prints[i].Rarity != "" && !strings.EqualFold(prints[i].Rarity, g.Rarity) {
				continue
			}
			matchIdx = i
			break
		}

		if matchIdx == -1 {
			continue
		}

		if !matched[matchIdx] {
			matched[matchIdx] = true
			delete(seen, printVariantKey(prints[matchIdx]))
			applyGalleryData(&prints[matchIdx], g)
			seen[printVariantKey(prints[matchIdx])] = true
			continue
		}

		cardCopy := prints[matchIdx]
		applyGalleryData(&cardCopy, g)
		key := printVariantKey(cardCopy)
		if seen[key] {
			continue
		}
		seen[key] = true
		extra = append(extra, cardCopy)
	}

	return append(prints, extra...)
}

func printVariantKey(p catalog.CatalogItem) string {
	return strings.ToUpper(strings.TrimSpace(p.Code)) + "|" +
		strings.ToLower(strings.TrimSpace(p.Rarity)) + "|" +
		strings.ToLower(strings.TrimSpace(p.Edition)) + "|" +
		strconv.FormatBool(p.ExtendedArtwork)
}

// mappedReleaseDate attempts to parse a date string using common Yugipedia formats.
func mappedReleaseDate(s string) time.Time {
	formats := []string{
		"January 2, 2006",
		"February 2, 2006",
		"January 2006",
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// labelToLang maps a human-readable language label or a short language code
// (from an abbr title attribute) to catalog.Lang.
func labelToLang(label string) (catalog.Lang, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(label, "\u00a0", " ")))
	if i := strings.Index(normalized, "("); i >= 0 {
		normalized = strings.TrimSpace(normalized[:i])
	}

	m := map[string]catalog.Lang{
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
	l, ok := m[normalized]
	return l, ok
}
