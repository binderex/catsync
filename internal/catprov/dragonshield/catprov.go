// Package dragonshield scrapes Dragon Shield card sleeves and converts them
// into catalog items.
package dragonshield

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/retrier"
)

const (
	baseURL        = "https://www.dragonshield.com"
	cardSleevesURL = baseURL + "/collections/card-sleeves"

	imageSmallWidth = 480
	imageLargeWidth = 800
)

// ErrSleevesNotFound is returned when the collection yields no usable sleeves.
var ErrSleevesNotFound = errors.New("sleeves not found")

// Provider scrapes the Dragon Shield card sleeve collection.
type Provider struct {
	collectionURL string
}

// New returns a Dragon Shield catalog provider.
func New() *Provider {
	return &Provider{collectionURL: cardSleevesURL}
}

// GetSleeves visits the card sleeve collection, follows its pagination and
// returns one catalog item per product variant.
func (p *Provider) GetSleeves(ctx context.Context) ([]catalog.Item, error) {
	collectionURL := p.collectionURL
	if collectionURL == "" {
		collectionURL = cardSleevesURL
	}

	browser := retrier.NewChromedp()

	var items []catalog.Item
	var productFilter retrier.Filter

	productFilter = func(response retrier.Response) bool {
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(response.Body))
		if err != nil {
			return false
		}

		pageItems := parseProductPage(doc.Selection)
		if len(pageItems) == 0 {
			log.Printf("dragonshield: product %s parsed 0 items (body %d bytes)", response.URL, len(response.Body))
		}

		items = append(items, pageItems...)
		return false
	}

	var collectionFilter retrier.Filter
	collectionFilter = func(response retrier.Response) bool {
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(response.Body))
		if err != nil {
			return false
		}

		pageURL, err := url.Parse(response.URL)
		if err != nil {
			return false
		}

		products := productLinks(doc.Selection, pageURL)
		if len(products) == 0 {
			products = productLinksFromMeta(doc.Selection, pageURL)
		}

		next := nextPageURL(doc.Selection, pageURL)
		log.Printf("dragonshield: collection %s: %d products, next=%q", response.URL, len(products), next)

		for _, productURL := range products {
			browser.Add(productURL, productFilter)
		}

		if next != "" {
			browser.Add(next, collectionFilter)
		}

		return false
	}

	browser.Add(collectionURL, collectionFilter)

	if err := browser.Run(ctx); err != nil {
		return nil, err
	}

	for pageURL, state := range browser.States() {
		switch {
		case state.Failed:
			log.Printf(
				"dragonshield: page %s failed after %d attempts: %s",
				pageURL, state.Attempts, state.Error,
			)
		case state.Invalid:
			log.Printf("dragonshield: page %s is invalid", pageURL)
		}
	}

	if len(items) == 0 {
		return nil, ErrSleevesNotFound
	}

	return items, nil
}

func productLinks(doc *goquery.Selection, pageURL *url.URL) []string {
	grid := doc.Find(".js-collection-grid").First()
	if grid.Length() == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	links := make([]string, 0)

	grid.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		resolved := resolveURL(pageURL, href)
		if resolved == nil || !strings.HasPrefix(resolved.Path, "/products/") {
			return
		}

		resolved.RawQuery = ""
		resolved.Fragment = ""

		absolute := resolved.String()
		if _, exists := seen[absolute]; exists {
			return
		}

		seen[absolute] = struct{}{}
		links = append(links, absolute)
	})

	return links
}

// productLinksFromMeta reads the product handles from the collection page's
// embedded Shopify meta object. The visual grid is rendered by JavaScript with
// skeleton cards first, so the initial DOM may not contain the product links.
func productLinksFromMeta(doc *goquery.Selection, pageURL *url.URL) []string {
	if pageURL == nil {
		return nil
	}

	prefix := ""
	if index := strings.Index(pageURL.Path, "/collections/"); index > 0 {
		prefix = pageURL.Path[:index]
	}

	products := make([]string, 0)
	seen := make(map[string]struct{})

	doc.Find("script").EachWithBreak(func(_ int, script *goquery.Selection) bool {
		text := script.Text()

		metaIndex := strings.Index(text, "var meta")
		if metaIndex < 0 {
			return true
		}

		objectIndex := strings.Index(text[metaIndex:], "{")
		if objectIndex < 0 {
			return true
		}

		var payload struct {
			Products []struct {
				Handle string `json:"handle"`
			} `json:"products"`
		}

		raw := text[metaIndex+objectIndex:]
		if err := json.NewDecoder(strings.NewReader(raw)).Decode(&payload); err != nil {
			return true
		}
		if len(payload.Products) == 0 {
			return true
		}

		for _, product := range payload.Products {
			handle := strings.TrimSpace(product.Handle)
			if handle == "" {
				continue
			}

			productURL := (&url.URL{
				Scheme: pageURL.Scheme,
				Host:   pageURL.Host,
				Path:   prefix + "/products/" + handle,
			}).String()
			if _, exists := seen[productURL]; exists {
				continue
			}

			seen[productURL] = struct{}{}
			products = append(products, productURL)
		}

		return false
	})

	return products
}

func nextPageURL(doc *goquery.Selection, pageURL *url.URL) string {
	href, exists := doc.Find("a[rel='next'], link[rel='next']").First().Attr("href")
	if !exists {
		return ""
	}

	resolved := resolveURL(pageURL, href)
	if resolved == nil {
		return ""
	}

	resolved.Fragment = ""
	return resolved.String()
}

func resolveURL(base *url.URL, href string) *url.URL {
	href = strings.TrimSpace(href)
	if href == "" || base == nil {
		return nil
	}

	reference, err := url.Parse(href)
	if err != nil {
		return nil
	}

	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil
	}

	if resolved.Host == "" {
		resolved.Host = base.Host
	}

	return resolved
}

type productVariant struct {
	ID    string
	SKU   string
	Name  string
	Title string
}

type productOptions struct {
	Product struct {
		Variants []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Name  string `json:"name"`
			SKU   string `json:"sku"`
		} `json:"variants"`
	} `json:"product"`
}

type productSpec struct {
	key       string
	shared    string
	byVariant map[string]string
	fallback  string
}

func (s productSpec) value(variantID string) string {
	if variantID != "" {
		if value, exists := s.byVariant[variantID]; exists && value != "" {
			return value
		}
	}

	if s.shared != "" {
		return s.shared
	}

	return s.fallback
}

func parseProductPage(doc *goquery.Selection) []catalog.Item {
	name := cleanText(doc.Find("h1[class*='ProductDetails__title']").First().Text())
	if name == "" {
		return nil
	}

	sku := doc.Find("p[class*='ProductSpecifications__sku'] span[data-sku]").First()
	if sku.Length() == 0 {
		sku = doc.Find("span[data-sku]").First()
	}
	defaultSKU := cleanText(sku.Text())

	color := parseColor(doc)
	specs := parseSpecs(doc)
	image := parseImage(doc)

	variants := parseVariants(doc)
	if len(variants) == 0 {
		variants = []productVariant{{SKU: defaultSKU, Name: name}}
	}

	items := make([]catalog.Item, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))

	for _, variant := range variants {
		itemName := cleanText(variant.Name)
		if itemName == "" {
			itemName = name
		}

		code := strings.TrimSpace(variant.SKU)
		if code == "" {
			code = defaultSKU
		}
		if code == "" {
			continue
		}

		subtypes := make(map[string]any, len(specs)+2)
		if color != "" {
			subtypes["color"] = color
		}

		for _, spec := range specs {
			if value := spec.value(variant.ID); value != "" {
				subtypes[spec.key] = value
			}
		}

		if _, exists := subtypes["size"]; !exists {
			size := cleanText(variant.Title)
			if size != "" && !strings.EqualFold(size, "Default Title") {
				subtypes["size"] = size
			}
		}

		if strings.Contains(strings.ToLower(name), "clear") ||
			strings.Contains(strings.ToLower(itemName), "clear") {
			subtypes["clear"] = true
		}

		identity := code + "\x00" + itemName + "\x00" + subtypesKey(subtypes)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}

		item := catalog.NewSleeve(
			nil,
			catalog.BrandDS,
			subtypes,
			catalog.LangEN,
			code,
			itemName,
			"",
			image,
			time.Time{},
		)
		items = append(items, *item)
	}

	return items
}

func parseVariants(doc *goquery.Selection) []productVariant {
	raw := strings.TrimSpace(doc.Find("script.js-product-options-props").First().Text())
	if raw == "" {
		return nil
	}

	var payload productOptions
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}

	variants := make([]productVariant, 0, len(payload.Product.Variants))
	for _, variant := range payload.Product.Variants {
		id := ""
		if variant.ID != 0 {
			id = strconv.FormatInt(variant.ID, 10)
		}

		variants = append(variants, productVariant{
			ID:    id,
			SKU:   strings.TrimSpace(variant.SKU),
			Name:  cleanText(variant.Name),
			Title: cleanText(variant.Title),
		})
	}

	return variants
}

func parseColor(doc *goquery.Selection) string {
	current := doc.Find("small[data-current-swatch-title]").First()
	if current.Length() > 0 {
		if value := cleanText(current.AttrOr("data-current-swatch-title", "")); value != "" {
			return value
		}
		if value := cleanText(current.Text()); value != "" {
			return value
		}
	}

	original := doc.Find("small[data-original-swatch-title]").First()
	if original.Length() > 0 {
		if value := cleanText(original.AttrOr("data-original-swatch-title", "")); value != "" {
			return value
		}
		return cleanText(original.Text())
	}

	return ""
}

func parseSpecs(doc *goquery.Selection) []productSpec {
	list := doc.Find("div[class*='ProductSpecifications__list']").First()
	if list.Length() == 0 {
		return nil
	}

	specs := make([]productSpec, 0)
	list.ChildrenFiltered("div").Each(func(_ int, item *goquery.Selection) {
		title := cleanText(item.Find("h3[class*='ProductSpecifications_item-title']").First().Text())
		key := subtypeKey(title)
		if key == "" {
			return
		}

		spec := productSpec{
			key:       key,
			byVariant: make(map[string]string),
		}

		item.Find("div[class*='ProductSpecifications_item-description']").
			Each(func(_ int, description *goquery.Selection) {
				value := cleanText(description.Text())
				if value == "" {
					return
				}

				if spec.fallback == "" {
					spec.fallback = value
				}

				variantID := strings.TrimSpace(description.AttrOr("data-variant-id", ""))
				if variantID != "" {
					spec.byVariant[variantID] = value
					return
				}

				if spec.shared == "" {
					spec.shared = value
				}
			})

		active := cleanText(
			item.Find("div[class*='ProductSpecifications_item-description'][class*='--is-active']").
				First().
				Text(),
		)
		if active != "" {
			spec.fallback = active
		}

		specs = append(specs, spec)
	})

	return specs
}

func subtypeKey(title string) string {
	var builder strings.Builder
	lastUnderscore := false

	for _, char := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			builder.WriteRune(char)
			lastUnderscore = false

		case !lastUnderscore && builder.Len() > 0:
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(builder.String(), "_")
}

func parseImage(doc *goquery.Selection) catalog.Image {
	image := doc.Find(".js-product-gallery-image img").First()
	src := strings.TrimSpace(image.AttrOr("src", ""))
	if src == "" {
		src = strings.TrimSpace(image.AttrOr("data-src", ""))
	}
	if src == "" {
		src = strings.TrimSpace(
			doc.Find(".js-product-gallery-image a.js-lightbox-trigger").First().AttrOr("href", ""),
		)
	}
	if src == "" {
		return catalog.Image{}
	}

	return catalog.Image{
		URLSmall: imageURL(src, imageSmallWidth),
		URLLarge: imageURL(src, imageLargeWidth),
	}
}

func imageURL(raw string, width int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.Scheme != "https" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		parsed.Host = "www.dragonshield.com"
	}

	query := parsed.Query()
	query.Set("width", strconv.Itoa(width))
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func subtypesKey(subtypes map[string]any) string {
	if subtypes == nil {
		return ""
	}

	data, err := json.Marshal(subtypes)
	if err != nil {
		return fmt.Sprintf("%v", subtypes)
	}

	return string(data)
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
