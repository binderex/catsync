package dragonshield

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/binderex/catsync/internal/domain"
)

func TestDragonshield(t *testing.T) {
	t.Run("parses one item per variant", func(t *testing.T) {
		doc := parseDocument(t, `
<!doctype html>
<html>
<body>
  <h1 class="tvg_product_details_DNznPq_ProductDetails__title">
    Black - Matte Sleeves
  </h1>

  <h5 class="tvg_product_swatches_ImE-kW_ProductSwatches__title">
    Color :
    <small
      class="js-product-swatches-current-swatch-title"
      data-original-swatch-title="Black"
      data-current-swatch-title="Black"
    >Black</small>
  </h5>

  <section class="js-product-gallery">
    <div class="js-product-gallery-image">
      <img
        class="tvg_core_picture_HHz8EW_image"
        src="//www.dragonshield.com/cdn/shop/files/black.png?v=1773377387&amp;width=800"
        alt="Black - Matte Sleeves"
      >
    </div>
  </section>

  <div class="ProductSpecifications">
    <div class="ProductSpecifications__list">
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Size</h3>
        <div class="ProductSpecifications_item-description --is-active" data-variant-id="101">Standard</div>
        <div class="ProductSpecifications_item-description" data-variant-id="102">Japanese</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Texture</h3>
        <div class="ProductSpecifications_item-description --is-active">Matte</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Glare Type</h3>
        <div class="ProductSpecifications_item-description --is-active">Glare</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Quality</h3>
        <div class="ProductSpecifications_item-description --is-active">120µm</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Items per case</h3>
        <div class="ProductSpecifications_item-description --is-active" data-variant-id="101">100</div>
        <div class="ProductSpecifications_item-description" data-variant-id="102">60</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Material</h3>
        <div class="ProductSpecifications_item-description --is-active">PP</div>
      </div>
      <div class="ProductSpecifications_item">
        <h3 class="ProductSpecifications_item-title">Layer</h3>
        <div class="ProductSpecifications_item-description --is-active">Regular</div>
      </div>
    </div>

    <p class="ProductSpecifications__sku">
      Product Number: <span data-sku>AT-11002</span>
    </p>
  </div>

  <script type="application/json" class="js-product-options-props">
  {
    "product": {
      "variants": [
        {"id": 101, "title": "Standard", "name": "Black - Matte Sleeves - Standard", "sku": "AT-11002"},
        {"id": 102, "title": "Japanese", "name": "Black - Matte Sleeves - Japanese", "sku": "AT-11102"}
      ]
    }
  }
  </script>
</body>
</html>`)

		items := parseProductPage(doc.Selection, "https://www.dragonshield.com/products/black-matte-sleeves")
		if len(items) != 2 {
			t.Fatalf("parseProductPage() returned %d items, want 2", len(items))
		}

		byCode := make(map[string]domain.Item, len(items))
		for _, item := range items {
			byCode[item.Code] = item
		}

		standard, ok := byCode["AT-11002"]
		if !ok {
			t.Fatal("missing standard variant AT-11002")
		}

		if standard.Kind != domain.KindSleeve {
			t.Errorf("standard.Kind = %q, want %q", standard.Kind, domain.KindSleeve)
		}
		if standard.Brand != domain.BrandDS {
			t.Errorf("standard.Brand = %q, want %q", standard.Brand, domain.BrandDS)
		}
		if standard.Lang != domain.LangEN {
			t.Errorf("standard.Lang = %q, want %q", standard.Lang, domain.LangEN)
		}
		if standard.Name != "Black - Matte Sleeves - Standard" {
			t.Errorf("standard.Name = %q", standard.Name)
		}
		if standard.Description != "" {
			t.Errorf("standard.Description = %q, want empty", standard.Description)
		}
		if standard.Source != "https://www.dragonshield.com/products/black-matte-sleeves" {
			t.Errorf("standard.Source = %q", standard.Source)
		}
		if !standard.VerifiedSource {
			t.Error("standard.VerifiedSource = false, want true")
		}
		if !standard.ReleaseAt.IsZero() {
			t.Errorf("standard.ReleaseAt = %v, want zero", standard.ReleaseAt)
		}
		if standard.Image.URLSmall != "https://www.dragonshield.com/cdn/shop/files/black.png?v=1773377387&width=480" {
			t.Errorf("standard.Image.URLSmall = %q", standard.Image.URLSmall)
		}
		if standard.Image.URLLarge != "https://www.dragonshield.com/cdn/shop/files/black.png?v=1773377387&width=800" {
			t.Errorf("standard.Image.URLLarge = %q", standard.Image.URLLarge)
		}

		wantSubtypes := map[string]string{
			"color":          "Black",
			"size":           "Standard",
			"texture":        "Matte",
			"glare_type":     "Glare",
			"quality":        "120µm",
			"items_per_case": "100",
			"material":       "PP",
			"layer":          "Regular",
		}
		standardVariants := standard.Variant()
		for key, want := range wantSubtypes {
			if got := subtypeString(t, standardVariants, key); got != want {
				t.Errorf("standard.Variant()[%q] = %q, want %q", key, got, want)
			}
		}
		if _, exists := standardVariants["clear"]; exists {
			t.Errorf("standard.Variant()[clear] exists, want absent")
		}

		japanese, ok := byCode["AT-11102"]
		if !ok {
			t.Fatal("missing japanese variant AT-11102")
		}
		if japanese.Name != "Black - Matte Sleeves - Japanese" {
			t.Errorf("japanese.Name = %q", japanese.Name)
		}
		japaneseVariants := japanese.Variant()
		if got := subtypeString(t, japaneseVariants, "size"); got != "Japanese" {
			t.Errorf("japanese.Variant()[size] = %q, want Japanese", got)
		}
		if got := subtypeString(t, japaneseVariants, "items_per_case"); got != "60" {
			t.Errorf("japanese.Variant()[items_per_case] = %q, want 60", got)
		}
		if got := subtypeString(t, japaneseVariants, "color"); got != "Black" {
			t.Errorf("japanese.Variant()[color] = %q, want Black", got)
		}
	})

	t.Run("falls back to the visible SKU and marks clear sleeves", func(t *testing.T) {
		doc := parseDocument(t, `
<!doctype html>
<html>
<body>
  <h1 class="ProductDetails__title">Clear - Matte Sleeves</h1>

  <small
    class="js-product-swatches-current-swatch-title"
    data-original-swatch-title="Clear"
    data-current-swatch-title="Clear"
  >Clear</small>

  <div class="js-product-gallery-image">
    <img src="//www.dragonshield.com/cdn/shop/files/clear.png?v=1">
  </div>

  <div class="ProductSpecifications__list">
    <div class="ProductSpecifications_item">
      <h3 class="ProductSpecifications_item-title">Texture</h3>
      <div class="ProductSpecifications_item-description --is-active">Matte</div>
    </div>
  </div>

  <span data-sku>AT-13002</span>
</body>
</html>`)

		items := parseProductPage(doc.Selection, "https://www.dragonshield.com/products/clear-matte-sleeves")
		if len(items) != 1 {
			t.Fatalf("parseProductPage() returned %d items, want 1", len(items))
		}

		item := items[0]
		if item.Code != "AT-13002" {
			t.Errorf("item.Code = %q, want AT-13002", item.Code)
		}
		if item.Name != "Clear - Matte Sleeves" {
			t.Errorf("item.Name = %q", item.Name)
		}
		variant := item.Variant()
		if clear, _ := variant["clear"].(bool); !clear {
			t.Errorf("item.Variant()[clear] = %v, want true", variant["clear"])
		}
		if got := subtypeString(t, variant, "texture"); got != "Matte" {
			t.Errorf("item.Variant()[texture] = %q, want Matte", got)
		}
		if got := subtypeString(t, variant, "color"); got != "Clear" {
			t.Errorf("item.Variant()[color] = %q, want Clear", got)
		}
		if !strings.HasSuffix(item.Image.URLSmall, "width=480") {
			t.Errorf("item.Image.URLSmall = %q, want width=480", item.Image.URLSmall)
		}
	})

	t.Run("extracts deduplicated product links and pagination", func(t *testing.T) {
		doc := parseDocument(t, `
<!doctype html>
<html>
<body>
  <div class="js-collection-grid">
    <a href="/products/black-matte-sleeves">Black</a>
    <a href="/products/black-matte-sleeves">Black duplicate</a>
    <a href="/products/crimson-silver-matte-dual-sleeves?utm_source=grid#gallery">Crimson</a>
    <a href="https://www.dragonshield.com/products/pink-sapphire-matte-sleeves">Pink</a>
    <a href="/collections/matte-sleeves">Not a product</a>
  </div>

  <a rel="next" href="/collections/card-sleeves?page=2">Next</a>
</body>
</html>`)

		pageURL, err := url.Parse("https://www.dragonshield.com/collections/card-sleeves")
		if err != nil {
			t.Fatalf("parse page URL: %v", err)
		}

		links := productLinks(doc.Selection, pageURL)
		wantLinks := []string{
			"https://www.dragonshield.com/products/black-matte-sleeves",
			"https://www.dragonshield.com/products/crimson-silver-matte-dual-sleeves",
			"https://www.dragonshield.com/products/pink-sapphire-matte-sleeves",
		}
		if len(links) != len(wantLinks) {
			t.Fatalf("productLinks() = %v, want %v", links, wantLinks)
		}
		for index, want := range wantLinks {
			if links[index] != want {
				t.Errorf("productLinks()[%d] = %q, want %q", index, links[index], want)
			}
		}

		next := nextPageURL(doc.Selection, pageURL)
		wantNext := "https://www.dragonshield.com/collections/card-sleeves?page=2"
		if next != wantNext {
			t.Errorf("nextPageURL() = %q, want %q", next, wantNext)
		}
	})

	t.Run("builds image variants", func(t *testing.T) {
		cases := []struct {
			name  string
			raw   string
			width int
			want  string
		}{
			{
				name:  "protocol relative and existing width",
				raw:   "//www.dragonshield.com/cdn/shop/files/a.png?width=200&v=1",
				width: 480,
				want:  "https://www.dragonshield.com/cdn/shop/files/a.png?v=1&width=480",
			},
			{
				name:  "absolute https",
				raw:   "https://cdn.example.com/a.png",
				width: 800,
				want:  "https://cdn.example.com/a.png?width=800",
			},
			{
				name:  "empty",
				raw:   "",
				width: 480,
				want:  "",
			},
		}

		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				if got := imageURL(testCase.raw, testCase.width); got != testCase.want {
					t.Errorf("imageURL(%q, %d) = %q, want %q", testCase.raw, testCase.width, got, testCase.want)
				}
			})
		}
	})
}

func TestGetSleevesLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live provider test in short mode")
	}
	if os.Getenv("DRAGONSHIELD_LIVE") == "" {
		t.Skip("set DRAGONSHIELD_LIVE=1 to run the live provider test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	items, err := New().GetSleeves(ctx)
	if err != nil {
		t.Fatalf("GetSleeves: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("GetSleeves returned no items")
	}

	t.Logf("collected %d sleeves in %s", len(items), time.Since(start).Round(time.Second))
}

func parseDocument(t *testing.T, html string) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse HTML: %v", err)
	}

	return doc
}

func subtypeString(t *testing.T, subtypes map[string]any, key string) string {
	t.Helper()

	value, exists := subtypes[key]
	if !exists {
		t.Fatalf("subtype %q is missing", key)
	}

	text, ok := value.(string)
	if !ok {
		t.Fatalf("subtype %q = %v (%T), want string", key, value, value)
	}

	return text
}
