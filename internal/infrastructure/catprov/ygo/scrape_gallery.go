package ygo

import (
	"bytes"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

// scrapeGalleryURL finds the card gallery linked from a card page.

func scrapeGalleryURL(pageURL string, root *goquery.Selection) string {
	var galleryURL string

	root.Find(`a[href^="/wiki/Card_Gallery:"]`).EachWithBreak(func(_ int, link *goquery.Selection) bool {
		if !strings.EqualFold(cleanText(link.Text()), "Gallery") {
			return true
		}

		href, exists := link.Attr("href")
		if !exists {
			return true
		}

		galleryURL = absoluteCardURL(pageURL, href)
		return false
	})

	return galleryURL
}

// galleryPrint is the metadata of one card gallery box.
type galleryPrint struct {
	Code            string
	Rarity          string
	RarityCode      string
	Edition         string
	ExtendedArtwork bool
	Image           domain.Image
	Lang            domain.Lang
}

// gallerySection is one heading-delimited gallery on a Card_Gallery page. A
// page can contain normal, Rush Duel, Anime, Manga and video-game sections,
// and some section IDs are duplicated, so sections are parsed in document
// order and selected by brand and language.
type gallerySection struct {
	Name   string
	Brand  domain.Brand
	Lang   domain.Lang
	Prints []galleryPrint
}

// scrapeGallery parses a Card_Gallery page into language/brand sections.
func scrapeGallery(body []byte) []gallerySection {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}

	var sections []gallerySection

	doc.Find("div.card-gallery").Each(func(_ int, section *goquery.Selection) {
		name := cleanText(section.Find("h2").First().Text())
		brand, lang, ok := gallerySectionInfo(name)
		if !ok {
			return
		}

		prints := scrapeGalleryBoxes(section.Find("li.gallerybox"))
		if len(prints) == 0 {
			return
		}

		sections = append(sections, gallerySection{
			Name:   name,
			Brand:  brand,
			Lang:   lang,
			Prints: prints,
		})
	})

	// Older gallery pages do not use the section wrapper. Keep a generic
	// fallback so those pages still work.
	if len(sections) == 0 {
		if prints := scrapeGalleryBoxes(doc.Find("li.gallerybox")); len(prints) > 0 {
			sections = append(sections, gallerySection{Prints: prints})
		}
	}

	return sections
}

func scrapeGalleryBoxes(boxes *goquery.Selection) []galleryPrint {
	var (
		items []galleryPrint
		seen  = make(map[string]bool)
	)

	boxes.Each(func(_ int, box *goquery.Selection) {
		item, ok := galleryItem(box)
		if !ok {
			return
		}

		key := strings.Join([]string{
			strings.ToUpper(item.Code),
			strings.ToLower(item.Rarity),
			strings.ToLower(item.Edition),
			boolKey(item.ExtendedArtwork),
		}, "|")
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, item)
	})

	return items
}

func gallerySectionInfo(name string) (domain.Brand, domain.Lang, bool) {
	lower := strings.ToLower(cleanText(name))

	switch {
	case strings.HasPrefix(lower, "rush duel"):
		brand := domain.BrandYGORD
		switch {
		case strings.Contains(lower, "japanese"):
			return brand, domain.LangJP, true
		case strings.Contains(lower, "korean"):
			return brand, domain.LangKR, true
		case strings.Contains(lower, "asian-english"):
			return brand, domain.LangAE, true
		case strings.Contains(lower, "simplified chinese"):
			return brand, domain.LangSC, true
		case strings.Contains(lower, "traditional chinese"):
			return brand, domain.LangTC, true
		default:
			return brand, "", true
		}

	case strings.HasPrefix(lower, "anime"),
		strings.HasPrefix(lower, "manga"),
		strings.HasPrefix(lower, "video games"),
		strings.HasPrefix(lower, "other"):
		return "", "", false

	case lower == "worldwide english",
		lower == "north american english",
		lower == "european english",
		lower == "australian english",
		lower == "english":
		return domain.BrandYGO, domain.LangEN, true
	case lower == "french", lower == "french-canadian":
		return domain.BrandYGO, domain.LangFR, true
	case lower == "german":
		return domain.BrandYGO, domain.LangDE, true
	case lower == "italian":
		return domain.BrandYGO, domain.LangIT, true
	case lower == "portuguese":
		return domain.BrandYGO, domain.LangPT, true
	case lower == "spanish":
		return domain.BrandYGO, domain.LangES, true
	case lower == "japanese":
		return domain.BrandYGO, domain.LangJP, true
	case lower == "asian-english":
		return domain.BrandYGO, domain.LangAE, true
	case lower == "korean":
		return domain.BrandYGO, domain.LangKR, true
	case lower == "simplified chinese":
		return domain.BrandYGO, domain.LangSC, true
	case lower == "traditional chinese":
		return domain.BrandYGO, domain.LangTC, true
	default:
		return "", "", false
	}
}

func selectGallerySection(print *domain.Item, sections []gallerySection) *gallerySection {
	brand := print.Brand
	if brand == "" {
		brand = domain.BrandYGO
	}

	for index := range sections {
		section := &sections[index]
		if section.Brand == brand && section.Lang == print.Lang {
			return section
		}
	}
	for index := range sections {
		section := &sections[index]
		if section.Brand == brand && section.Lang == "" {
			return section
		}
	}
	for index := range sections {
		section := &sections[index]
		if section.Brand == brand {
			return section
		}
	}
	for index := range sections {
		section := &sections[index]
		if section.Brand == "" {
			return section
		}
	}
	if len(sections) > 0 {
		return &sections[0]
	}
	return nil
}

func galleryItem(box *goquery.Selection) (galleryPrint, bool) {
	galleryText := box.Find(".gallerytext")
	if galleryText.Length() == 0 {
		return galleryPrint{}, false
	}

	// Official Proxy images are reference-only. They must never become catalog
	// prints, even when the marker uses different capitalization or sits
	// outside the paragraph that contains the links.
	if strings.Contains(strings.ToLower(galleryText.Text()), "official proxy") {
		return galleryPrint{}, false
	}

	paragraph := galleryText.Find("p")
	if paragraph.Length() == 0 {
		paragraph = galleryText
	}

	links := paragraph.Find("a")
	if links.Length() == 0 {
		return galleryPrint{}, false
	}

	code := cleanText(links.First().Text())
	if !cardCodeRegex.MatchString(code) {
		return galleryPrint{}, false
	}

	var rarity, rarityCode, edition string

	links.Each(func(index int, link *goquery.Selection) {
		if index == 0 || link.ParentsFiltered("i").Length() > 0 {
			return
		}

		href, _ := link.Attr("href")
		title, _ := link.Attr("title")
		text := cleanText(link.Text())

		if strings.Contains(href, "Edition") || strings.Contains(title, "Edition") || strings.Contains(text, "Edition") {
			edition = firstNonEmpty(text, title)
			return
		}

		if title != "" && !strings.Contains(strings.ToLower(title), "official proxy") {
			rarity = title
			rarityCode = text
		}
	})

	item := galleryPrint{
		Code:            code,
		Rarity:          rarity,
		RarityCode:      rarityCode,
		Edition:         edition,
		ExtendedArtwork: strings.Contains(strings.ToLower(galleryText.Text()), "extended artwork"),
		Lang:            langFromCardCode(code),
	}

	if image := scrapeImage(box.Find(".thumb")); image != nil {
		item.Image = *image
	}

	return item, true
}

// applyGalleryPrint enriches a print with the matching gallery metadata from
// the section that corresponds to its brand and language.
func applyGalleryPrint(print *domain.Item, sections []gallerySection) {
	if print == nil {
		return
	}

	section := selectGallerySection(print, sections)
	if section == nil {
		return
	}
	gallery := section.Prints

	var match *galleryPrint
	for index := range gallery {
		item := &gallery[index]
		if !strings.EqualFold(item.Code, print.Code) {
			continue
		}
		if item.Rarity != "" && !strings.EqualFold(item.Rarity, print.Rarity) {
			continue
		}
		if item.Edition != "" && print.Edition != "" && !strings.EqualFold(item.Edition, print.Edition) {
			continue
		}
		if item.ExtendedArtwork != print.ExtendedArt {
			continue
		}
		match = item
		break
	}

	if match == nil {
		// Fall back to code+rarity when the set list did not declare an edition
		// or extended-art variant.
		for index := range gallery {
			item := &gallery[index]
			if strings.EqualFold(item.Code, print.Code) && strings.EqualFold(item.Rarity, print.Rarity) {
				match = item
				break
			}
		}
	}

	if match == nil {
		return
	}

	if match.RarityCode != "" {
		print.SetProperty(domain.PropertyRarityCode, match.RarityCode)
	}
	if match.Edition != "" {
		print.Edition = match.Edition
	}
	if match.ExtendedArtwork {
		print.ExtendedArt = true
	}
	if match.Image.URLSmall != "" || match.Image.URLLarge != "" {
		print.Image = match.Image
	}
}

// galleryVariants returns extra print variants that the gallery declares but
// the set list did not. They are clones of the base print.
func galleryVariants(base domain.Item, sections []gallerySection) []domain.Item {
	section := selectGallerySection(&base, sections)
	if section == nil {
		return nil
	}
	gallery := section.Prints

	var variants []domain.Item
	seen := map[string]bool{printVariantKey(base): true}

	for _, item := range gallery {
		if !strings.EqualFold(item.Code, base.Code) || !strings.EqualFold(item.Rarity, base.Rarity) {
			continue
		}

		variant := base
		variant.ID = uuid.NewString()
		variant.Edition = item.Edition
		variant.SetProperty(domain.PropertyRarityCode, item.RarityCode)
		variant.ExtendedArt = item.ExtendedArtwork
		if item.Image.URLSmall != "" || item.Image.URLLarge != "" {
			variant.Image = item.Image
		}
		if variant.Edition == "" {
			variant.Edition = base.Edition
		}

		key := printVariantKey(variant)
		if seen[key] {
			continue
		}
		seen[key] = true
		variants = append(variants, variant)
	}

	return variants
}
