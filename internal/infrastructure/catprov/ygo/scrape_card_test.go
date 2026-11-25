package ygo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

const ojamaYellowFixture = `
<html><body>
<h1 id="firstHeading"><span class="mw-page-title-main">Ojama Yellow</span></h1>
<div class="card-table effect-card">
  <div class="heading"><div>Ojama Yellow</div></div>
  <div class="card-table-columns">
    <div class="imagecolumn">
      <div class="cardtable-main_image-wrapper"><img src="https://img.yugipedia.com/thumb/1/1a/OjamaYellow.png/300px-OjamaYellow.png"></div>
    </div>
    <div class="infocolumn">
      <table class="innertable">
        <tr><th>Card type</th><td><a href="/wiki/Monster_Card">Monster</a></td></tr>
        <tr><th>Attribute</th><td><a href="/wiki/EARTH">EARTH</a></td></tr>
        <tr><th>Types</th><td><a href="/wiki/Beast">Beast</a></td></tr>
        <tr><th>Level</th><td><a href="/wiki/Level_2_Monster_Cards">2</a></td></tr>
        <tr><th>ATK / DEF</th><td><a href="/wiki/700_ATK_Monster_Cards">700</a> / <a href="/wiki/1000_DEF_Monster_Cards">1000</a></td></tr>
        <tr><th>Password</th><td><a href="/wiki/42941100">42941100</a></td></tr>
        <tr><td colspan="2"><div class="lore">He's not the brightest, but he's a good friend.</div></td></tr>
      </table>
    </div>
  </div>
</div>
<a href="/wiki/Card_Gallery:Ojama_Yellow">Gallery</a>
<table id="cts--EN" class="wikitable card-list cts">
  <tr>
    <th class="cts__header--release">Release</th>
    <th class="cts__header--number">Number</th>
    <th class="cts__header--set">Set</th>
    <th class="cts__header--rarity">Rarity</th>
  </tr>
  <tr>
    <td>2006-02-08</td>
    <td><a href="/wiki/DP2-EN003">DP2-EN003</a></td>
    <td><a href="/wiki/Duelist_Pack:_Chazz_Princeton">Duelist Pack: Chazz Princeton</a></td>
    <td><a href="/wiki/Common">Common</a></td>
  </tr>
</table>
</body></html>`

const strategyCardFixture = `
<html><body>
<h1 id="firstHeading"><span class="mw-page-title-main">Ojama clogging 1</span></h1>
<div class="card-table strategy-card">
  <div class="heading"><div>Ojama clogging 1</div></div>
  <div class="card-table-columns">
    <div class="imagecolumn">
      <div class="cardtable-main_image-wrapper"><img src="https://img.yugipedia.com/thumb/7/75/StrategyCard1-DP2-EN.jpg/300px-StrategyCard1-DP2-EN.jpg"></div>
    </div>
    <div class="infocolumn">
      <table class="innertable">
        <tr><th>Card type</th><td><a href="/wiki/Strategy_Card">Strategy</a></td></tr>
        <tr><th>Status</th><td>Illegal (OCG) Illegal (TCG)</td></tr>
        <tr><td colspan="2"><div class="lore">"Ojama Trio" and "Ojama King" both clog up 3 spaces in your opponent's Monster Card Zone.</div></td></tr>
      </table>
    </div>
  </div>
</div>
</body></html>`

const rushDuelFixture = `
<html><body>
<div role="note" class="hatnote navigation-not-searchable">The <b>English</b> name given is not official.</div>
<h1 id="firstHeading"><span class="mw-page-title-main">Mad Rare Aquila</span></h1>
<div class="card-table effect-card">
  <div class="heading"><div>Mad Rare Aquila</div></div>
  <div class="card-table-columns">
    <div class="imagecolumn">
      <div class="cardtable-main_image-wrapper"><img src="https://img.yugipedia.com/thumb/7/70/MadRareAquila.png/300px-MadRareAquila.png"></div>
      <div class="status-badge"><a href="/wiki/Yu-Gi-Oh!_Rush_Duel">Rush Duel</a></div>
    </div>
    <div class="infocolumn">
      <table class="innertable">
        <tr><th>Card type</th><td><a href="/wiki/Monster_Card">Monster</a></td></tr>
        <tr><th>Attribute</th><td><a href="/wiki/DARK">DARK</a></td></tr>
        <tr><th>Types</th><td><a href="/wiki/Winged_Beast">Winged Beast</a> / <a href="/wiki/Effect_Monster">Effect</a></td></tr>
        <tr><th>Level</th><td><a href="/wiki/Level_4_Monster_Cards">4</a></td></tr>
        <tr><th>ATK / DEF</th><td><a href="/wiki/1300_ATK_Monster_Cards">1300</a> / <a href="/wiki/1100_DEF_Monster_Cards">1100</a></td></tr>
        <tr><td colspan="2"><div class="lore">This card can attack twice.</div></td></tr>
      </table>
    </div>
  </div>
</div>
</body></html>`

const gallerySectionsFixture = `
<html><body>
<div class="card-gallery" id="card-gallery--EN">
  <div class="mw-heading mw-heading2"><h2>Worldwide English</h2></div>
  <ul class="gallery">
    <li class="gallerybox">
      <div class="thumb"><img src="https://img.yugipedia.com/thumb/a/b/normal-en.png/300px-normal-en.png"></div>
      <div class="gallerytext"><p><a href="/wiki/LOB-EN001">LOB-EN001</a> <a href="/wiki/Ultra_Rare" title="Ultra Rare">UR</a> <a href="/wiki/1st_Edition" title="1st Edition">1st Edition</a></p></div>
    </li>
  </ul>
</div>
<div class="card-gallery" id="card-gallery--JP">
  <h2>Japanese</h2>
  <ul class="gallery">
    <li class="gallerybox">
      <div class="thumb"><img src="https://img.yugipedia.com/thumb/a/b/normal-jp.png/300px-normal-jp.png"></div>
      <div class="gallerytext"><p><a href="/wiki/LOB-JP001">LOB-JP001</a> <a href="/wiki/Ultra_Rare" title="Ultra Rare">UR</a> <a href="/wiki/1st_Edition" title="1st Edition">1st Edition</a></p></div>
    </li>
  </ul>
</div>
<div class="card-gallery" id="card-gallery--JP">
  <h2>Rush Duel - Japanese</h2>
  <ul class="gallery">
    <li class="gallerybox">
      <div class="thumb"><img src="https://img.yugipedia.com/thumb/a/b/rush-jp.png/300px-rush-jp.png"></div>
      <div class="gallerytext"><p><a href="/wiki/RD/VJMP-JP001">RD/VJMP-JP001</a> <a href="/wiki/Rush_Rare" title="Rush Rare">RR</a></p></div>
    </li>
  </ul>
</div>
<div class="card-gallery" id="card-gallery--KR">
  <h2>Rush Duel - Korean</h2>
  <ul class="gallery">
    <li class="gallerybox">
      <div class="thumb"><img src="https://img.yugipedia.com/thumb/a/b/rush-kr.png/300px-rush-kr.png"></div>
      <div class="gallerytext"><p><a href="/wiki/RD/VJMP-KR001">RD/VJMP-KR001</a> <a href="/wiki/Rush_Rare" title="Rush Rare">RR</a></p></div>
    </li>
  </ul>
</div>
<div class="card-gallery" id="card-gallery--EN">
  <h2>Anime - Worldwide English</h2>
  <ul class="gallery">
    <li class="gallerybox">
      <div class="thumb"><img src="https://img.yugipedia.com/thumb/a/b/anime-en.png/300px-anime-en.png"></div>
      <div class="gallerytext"><p><a href="/wiki/ANIME-EN001">ANIME-EN001</a> <a href="/wiki/Common" title="Common">C</a></p></div>
    </li>
  </ul>
</div>
</body></html>`

func TestScrapeGallerySections(t *testing.T) {
	sections := scrapeGallery([]byte(gallerySectionsFixture))
	if len(sections) != 4 {
		t.Fatalf("expected 4 gallery sections, got %d", len(sections))
	}
	for _, section := range sections {
		if strings.HasPrefix(strings.ToLower(section.Name), "anime") {
			t.Errorf("anime section was not ignored: %q", section.Name)
		}
	}

	normalEN := galleryTestPrint("LOB-EN001", domain.LangEN, domain.BrandYGO, "Ultra Rare")
	normalJP := galleryTestPrint("LOB-JP001", domain.LangJP, domain.BrandYGO, "Ultra Rare")
	rushJP := galleryTestPrint("RD/VJMP-JP001", domain.LangJP, domain.BrandYGORD, "Rush Rare")
	rushKR := galleryTestPrint("RD/VJMP-KR001", domain.LangKR, domain.BrandYGORD, "Rush Rare")

	applyGalleryPrint(&normalEN, sections)
	applyGalleryPrint(&normalJP, sections)
	applyGalleryPrint(&rushJP, sections)
	applyGalleryPrint(&rushKR, sections)

	if !strings.Contains(normalEN.Image.URLLarge, "normal-en.png") {
		t.Errorf("normal EN image = %q", normalEN.Image.URLLarge)
	}
	if !strings.Contains(normalJP.Image.URLLarge, "normal-jp.png") {
		t.Errorf("normal JP image = %q", normalJP.Image.URLLarge)
	}
	if !strings.Contains(rushJP.Image.URLLarge, "rush-jp.png") {
		t.Errorf("Rush JP image = %q", rushJP.Image.URLLarge)
	}
	if !strings.Contains(rushKR.Image.URLLarge, "rush-kr.png") {
		t.Errorf("Rush KR image = %q", rushKR.Image.URLLarge)
	}
	if normalEN.Edition != "1st Edition" || normalEN.PropertyString(domain.PropertyRarityCode) != "UR" {
		t.Errorf("normal EN gallery metadata = %q / %q", normalEN.Edition, normalEN.PropertyString(domain.PropertyRarityCode))
	}
}

func galleryTestPrint(code string, lang domain.Lang, brand domain.Brand, rarity string) domain.Item {
	print := domain.Item{
		Kind:        domain.KindCard,
		Brand:       brand,
		Lang:        lang,
		Code:        code,
		Name:        code,
		Description: "",
		Rarity:      rarity,
		Edition:     "",
		Properties:  make(map[string]any),
	}
	return print
}

func TestScrapeCardCarriesItemInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := NewYGOCatalogProvider()
	const pageURL = "https://yugipedia.com/wiki/Ojama_Yellow"

	cards, err := provider.fetchYGOPRODECKCards(ctx, "ojama yellow")
	if err != nil {
		t.Fatalf("fetch ygoprodeck: %v", err)
	}

	card, err := findYGOPROCard("Ojama Yellow", cards)
	if err != nil {
		t.Fatalf("find ygopro card: %v", err)
	}

	info := card.toItem(pageURL)
	result, err := provider.scrapeCard(ctx, pageURL, []byte(ojamaYellowFixture), info)
	if err != nil {
		t.Fatalf("scrape card: %v", err)
	}

	if result.Info != info {
		t.Fatal("scrapeCard did not keep the provided card info")
	}
	if len(result.Prints) != 1 {
		t.Fatalf("expected 1 card print, got %d", len(result.Prints))
	}

	print := result.Prints[0]
	if print.Source != pageURL {
		t.Fatalf("print does not carry the card source: %q", print.Source)
	}
	if print.Code != "DP2-EN003" || print.Rarity != "Common" {
		t.Errorf("print = %q / %q", print.Code, print.Rarity)
	}
	if result.Info.Source != pageURL {
		t.Errorf("card info source = %q, want %q", result.Info.Source, pageURL)
	}
	if result.GalleryURL != "https://yugipedia.com/wiki/Card_Gallery:Ojama_Yellow" {
		t.Errorf("gallery URL = %q", result.GalleryURL)
	}
}

func TestScrapeCardResolvesItemInfoWhenEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := NewYGOCatalogProvider()
	const pageURL = "https://yugipedia.com/wiki/Ojama_Yellow"

	result, err := provider.scrapeCard(ctx, pageURL, []byte(ojamaYellowFixture), nil)
	if err != nil {
		t.Fatalf("scrape card: %v", err)
	}
	if result.Info == nil {
		t.Fatal("expected a resolved card info")
	}
	if result.Info.Source != pageURL {
		t.Errorf("card info source = %q, want %q", result.Info.Source, pageURL)
	}
	if !result.Info.VerifiedSource {
		t.Error("Ojama Yellow should have a verified source")
	}
	if len(result.Prints) != 1 {
		t.Fatalf("expected 1 print, got %d", len(result.Prints))
	}
	if result.Prints[0].Source != result.Info.Source {
		t.Error("print does not carry the resolved card source")
	}
}

func TestScrapeStrategyCardFallsBackToPage(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	provider := NewYGOCatalogProvider()
	const pageURL = "https://yugipedia.com/wiki/Ojama_clogging_1"

	result, err := provider.scrapeCard(context.Background(), pageURL, []byte(strategyCardFixture), nil)
	if err != nil {
		t.Fatalf("scrape strategy card: %v", err)
	}
	if result.Info == nil {
		t.Fatal("expected a page-sourced ItemInfo")
	}
	if result.Info.Source != pageURL {
		t.Errorf("source = %q, want %q", result.Info.Source, pageURL)
	}
	if result.Info.Brand != domain.BrandYGO {
		t.Errorf("brand = %q, want %q", result.Info.Brand, domain.BrandYGO)
	}
	if result.Info.Properties["card_type"] != "Strategy" {
		t.Errorf("card_type = %v", result.Info.Properties["card_type"])
	}
	if !strings.Contains(result.Info.Properties["status"].(string), "Illegal") {
		t.Errorf("status = %v", result.Info.Properties["status"])
	}
	if !result.Info.VerifiedSource {
		t.Error("strategy card page should be verified")
	}
}

func TestScrapeCardUnofficialNameOverridesProvidedInfo(t *testing.T) {
	provider := NewYGOCatalogProvider()
	const pageURL = "https://yugipedia.com/wiki/Mad_Rare_Aquila"

	info := newCardItem(domain.BrandYGORD, nil, pageURL, "", "Monster", nil)
	info.VerifiedSource = true

	result, err := provider.scrapeCard(context.Background(), pageURL, []byte(rushDuelFixture), info)
	if err != nil {
		t.Fatalf("scrape rush duel card: %v", err)
	}
	if result.Info != info {
		t.Fatal("provided card info was not kept")
	}
	if result.Info.VerifiedSource {
		t.Error("the unofficial English name hatnote must override VerifiedSource to false")
	}
}

func TestScrapeRushDuelMarksUnofficialSource(t *testing.T) {
	provider := NewYGOCatalogProvider()
	const pageURL = "https://yugipedia.com/wiki/Mad_Rare_Aquila"

	result, err := provider.scrapeCard(context.Background(), pageURL, []byte(rushDuelFixture), nil)
	if err != nil {
		t.Fatalf("scrape rush duel card: %v", err)
	}
	if result.Info == nil {
		t.Fatal("expected a page-sourced ItemInfo")
	}
	if result.Info.Brand != domain.BrandYGORD {
		t.Errorf("brand = %q, want %q", result.Info.Brand, domain.BrandYGORD)
	}
	if result.Info.VerifiedSource {
		t.Error("the unofficial English name hatnote must set VerifiedSource=false")
	}
	if result.Info.Properties["card_type"] != "Monster" {
		t.Errorf("card_type = %v", result.Info.Properties["card_type"])
	}
}
