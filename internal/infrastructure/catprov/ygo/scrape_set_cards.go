package ygo

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

// scrapeSetCards parses every language table of the set page and appends the
// base CardPrinted rows to each set.
func scrapeSetCards(
	pageURL string,
	root *goquery.Selection,
	stateBySet map[*domain.Item]*setState,
	byLang map[domain.Lang]*domain.Item,
	byRegionalCode map[string]*domain.Item,
	booster bool,
	quantities map[string]uint,
) map[string][]*domain.Item {
	pending := make(map[*domain.Item][]pendingPrint)
	seen := make(map[*domain.Item]map[string]bool)

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
				var copies uint
				if !booster {
					copies = quantityFor(quantities, code, rarity)
				}

				source := cardSourcePage(cardURL, localizedName, code, set.Source)

				print := domain.Item{
					Kind:           domain.KindCard,
					Brand:          set.Brand,
					Lang:           set.Lang,
					Code:           code,
					Name:           localizedName,
					Description:    "",
					ReleaseAt:      set.ReleaseAt,
					SetID:          &set.ID,
					Rarity:         rarity,
					Edition:        "",
					ExtendedArt:    extendedArtwork,
					Source:         source,
					VerifiedSource: true,
					Properties:     make(map[string]any),
				}
				print.ID = uuid.NewString()
				print.SetProperty(domain.PropertySerialCode, cardSerialCode(code))
				print.SetProperty(domain.PropertyGuaranteedCopies, copies)

				key := printVariantKey(print)
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
								existing.SetProperty(domain.PropertyGuaranteedCopies, quantityFor(quantities, code, rarity))
								break
							}
						}
					}
					continue
				}
				seen[set][key] = true

				pending[set] = append(pending[set], pendingPrint{print: print, url: cardURL})
			}
		})
	})

	printsByCardURL := make(map[string][]*domain.Item)

	for set, rows := range pending {
		printed := make([]domain.Item, len(rows))
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

// materializeItems flattens every set and its prints into a single list of
// domain items. Sets come first so the persistence repository can resolve
// their IDs before writing prints.
func materializeItems(states []*setState) []domain.Item {
	return materializeCompleteItems(states, nil)
}

// materializeCompleteItems flattens the sets and the prints that are not in
// pending. Prints from an unfinished card or gallery page are resume state and
// must never be treated as scraped catalog rows.
func materializeCompleteItems(states []*setState, pending map[*domain.Item]bool) []domain.Item {
	items := make([]domain.Item, 0, len(states))
	for _, state := range states {
		if state == nil || state.Set == nil {
			continue
		}

		items = append(items, *state.Set)
		for _, print := range state.Prints {
			if print == nil || pending[print] {
				continue
			}
			applyPrintImageFallback(print, state.Set)
			items = append(items, *print)
		}
	}
	return items
}

// applyPrintImageFallback uses the shared card images or the set image when a
// print did not get a gallery image.
func applyPrintImageFallback(print, set *domain.Item) {
	if print == nil || set == nil {
		return
	}
	if print.Image.URLSmall != "" || print.Image.URLLarge != "" {
		return
	}
	if images := print.PropertyImages(); len(images) > 0 {
		print.Image = images[0]
		return
	}
	if set.Image.URLSmall != "" || set.Image.URLLarge != "" {
		print.Image = set.Image
	}
}

func setForPrint(
	byLang map[domain.Lang]*domain.Item,
	byRegionalCode map[string]*domain.Item,
	lang domain.Lang,
	code string,
) *domain.Item {
	if regionalCode := cardRegionalCode(code); regionalCode != "" {
		if set := byRegionalCode[strings.ToUpper(regionalCode)]; set != nil {
			return set
		}
	}

	upperCode := strings.ToUpper(code)
	var best *domain.Item
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
