package ygo

import (
	"strings"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

const setFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Magnificent Maestros</th></tr>
  <tr><td class="infobox-image"><img src="https://img.yugipedia.com/thumb/9/9a/MAMS-BoxEN.png/300px-MAMS-BoxEN.png"></td></tr>
  <tr><th class="infobox-header">Names</th></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data"><span lang="en">Magnificent Maestros</span></td></tr>
  <tr><th class="infobox-label">French</th><td class="infobox-data"><span lang="fr">Les Maîtres Fantastiques</span></td></tr>
  <tr><th class="infobox-label">Spanish</th><td class="infobox-data"><span lang="es">Maestros Magníficos</span></td></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Collector's Set</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul>
      <li><abbr title="en">MAMS-EN</abbr></li>
      <li><abbr title="fr">MAMS-FR</abbr></li>
      <li><abbr title="sp">MAMS-SP</abbr></li>
    </ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">English (na)</th><td class="infobox-data">November 13, 2026</td></tr>
  <tr><th class="infobox-label">English (eu)</th><td class="infobox-data">November 12, 2026</td></tr>
  <tr><th class="infobox-label">French, Spanish</th><td class="infobox-data">November 12, 2026</td></tr>
</table>

<div class="set-list"><table>
  <tr><th>Card number</th><th>Name</th><th>Rarity</th><th>Category</th><th>Print</th></tr>
  <tr>
    <td><a href="/wiki/MAMS-EN001">MAMS-EN001</a></td>
    <td>"<a href="/wiki/Blue-Eyes_White_Dragon,_the_White_Phantom_Beast">Blue-Eyes White Dragon, the White Phantom Beast</a>"</td>
    <td><a href="/wiki/Ultra_Rare">Ultra Rare</a><br><a href="/wiki/Starlight_Rare">Starlight Rare</a></td>
    <td>Effect Monster</td><td>New</td>
  </tr>
  <tr>
    <td><a href="/wiki/MAMS-EN001">MAMS-EN001</a></td>
    <td>"<a href="/wiki/Blue-Eyes_White_Dragon,_the_White_Phantom_Beast">Blue-Eyes White Dragon, the White Phantom Beast</a>" (extended art)</td>
    <td><a href="/wiki/Ultra_Rare">Ultra Rare</a></td>
    <td>Effect Monster</td><td>New</td>
  </tr>
</table></div>

<div class="set-list"><table>
  <tr><th>Card number</th><th>English name</th><th>French name</th><th>Rarity</th><th>Category</th><th>Print</th></tr>
  <tr>
    <td><a href="/wiki/MAMS-FR001">MAMS-FR001</a></td>
    <td>"<a href="/wiki/Blue-Eyes_White_Dragon,_the_White_Phantom_Beast">Blue-Eyes White Dragon, the White Phantom Beast</a>"</td>
    <td>"Dragon Blanc aux Yeux Bleus, la Bête Fantôme Blanche"</td>
    <td><a href="/wiki/Ultra_Rare">Ultra Rare</a></td>
    <td>Effect Monster</td><td>New</td>
  </tr>
</table></div>

<div class="set-list"><table>
  <tr><th>Card number</th><th>English name</th><th>Spanish name</th><th>Rarity</th><th>Category</th><th>Print</th></tr>
  <tr>
    <td><a href="/wiki/MAMS-SP001">MAMS-SP001</a></td>
    <td>"<a href="/wiki/Blue-Eyes_White_Dragon,_the_White_Phantom_Beast">Blue-Eyes White Dragon, the White Phantom Beast</a>"</td>
    <td>"Dragón Blanco de Ojos Azules, la Bestia Fantasma Blanca"</td>
    <td><a href="/wiki/Ultra_Rare">Ultra Rare</a></td>
    <td>Effect Monster</td><td>New</td>
  </tr>
</table></div>
</body></html>`

const academySetFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Academy Duel Disk Limited Edition</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Medium</th><td class="infobox-data"><a href="/wiki/Yu-Gi-Oh!_Official_Card_Game">OCG</a></td></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Promotional card</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul><li>ADD-JP (<abbr title="Japanese">jp</abbr>)</li></ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">Japanese</th><td class="infobox-data">April 27, 2006</td></tr>
</table>
<div class="set-list-tab">
  <div class="set-list"><table>
    <tr><th>Card number</th><th>English name</th><th>Japanese name</th><th>Rarity</th><th>Category</th></tr>
    <tr>
      <td><a href="/wiki/ADD-JP001">ADD-JP001</a></td>
      <td>"<a href="/wiki/Elemental_HERO_Avian">Elemental HERO Avian</a>"</td>
      <td><span lang="ja">Ｅ・ＨＥＲＯ フェザーマン</span></td>
      <td><a href="/wiki/Common">Common</a></td>
      <td>Normal Monster</td>
    </tr>
  </table></div>
</div>
</body></html>`

const boosterSetFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Booster Set</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Booster Pack</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data"><ul><li>BST-EN (<abbr title="en">en</abbr>)</li></ul></td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data">January 1, 2006</td></tr>
</table>
<div class="set-list"><table>
  <tr><th>Card number</th><th>Name</th><th>Rarity</th></tr>
  <tr><td><a href="/wiki/BST-EN001">BST-EN001</a></td><td><a href="/wiki/Card_A">Card A</a></td><td><a href="/wiki/Common">Common</a></td></tr>
</table></div>
</body></html>`

func TestScrapeSet(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	states, cardURLs, _, err := scrapeSet(
		"https://yugipedia.com/wiki/Magnificent_Maestros",
		[]byte(setFixture),
		Release{},
	)
	if err != nil {
		t.Fatalf("scrape set: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(states))
	}

	enSet := findSetState(states, domain.LangEN)
	if enSet == nil {
		t.Fatal("missing English set")
	}
	if enSet.Set.Name != "Magnificent Maestros" {
		t.Errorf("English set name = %q", enSet.Set.Name)
	}
	if enSet.Set.Code != "MAMS" || enSet.Set.PropertyString(domain.PropertyRegionalCode) != "MAMS-EN" {
		t.Errorf("English set codes = %q / %q", enSet.Set.Code, enSet.Set.PropertyString(domain.PropertyRegionalCode))
	}
	if !enSet.Set.ReleaseAt.Equal(time.Date(2026, time.November, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("English set release = %v", enSet.Set.ReleaseAt)
	}
	if enSet.Set.Source != "https://yugipedia.com/wiki/Magnificent_Maestros" {
		t.Errorf("set source = %q", enSet.Set.Source)
	}
	if enSet.Set.PropertyString(domain.PropertySetType) != "Collector's Set" || enSet.Set.PropertyString(domain.PropertySetCode) != "MAMS" {
		t.Errorf("set properties = %+v", enSet.Set.Properties)
	}
	if enSet.Set.ID == "" {
		t.Error("set ID is empty")
	}

	if len(enSet.Prints) != 3 {
		t.Fatalf("expected 3 English prints, got %d", len(enSet.Prints))
	}

	enUltra := findPrint(enSet.Prints, domain.LangEN, "Ultra Rare", false)
	if enUltra == nil {
		t.Fatal("missing English Ultra Rare")
	}
	if enUltra.Name != "Blue-Eyes White Dragon, the White Phantom Beast" {
		t.Errorf("English card name = %q", enUltra.Name)
	}
	if enUltra.Code != "MAMS-EN001" || enUltra.PropertyString(domain.PropertySerialCode) != "001" {
		t.Errorf("English card code = %q / %q", enUltra.Code, enUltra.PropertyString(domain.PropertySerialCode))
	}
	if enUltra.SetID == nil || *enUltra.SetID != enSet.Set.ID {
		t.Errorf("English card SetID = %v, want %v", enUltra.SetID, enSet.Set.ID)
	}
	if enUltra.Source == "" {
		t.Error("base print should carry its card source")
	}
	if enUltra.PropertyUint(domain.PropertyGuaranteedCopies) != 2 {
		t.Errorf("English Ultra Rare GuaranteedCopies = %d, want 2", enUltra.PropertyUint(domain.PropertyGuaranteedCopies))
	}

	enExtended := findPrint(enSet.Prints, domain.LangEN, "Ultra Rare", true)
	if enExtended == nil {
		t.Fatal("missing English extended art print")
	}

	frSet := findSetState(states, domain.LangFR)
	if frSet == nil {
		t.Fatal("missing French set")
	}
	if frSet.Set.Name != "Les Maîtres Fantastiques" {
		t.Errorf("French set name = %q", frSet.Set.Name)
	}
	frCard := findPrint(frSet.Prints, domain.LangFR, "Ultra Rare", false)
	if frCard == nil {
		t.Fatal("missing French print")
	}
	if frCard.Name != "Dragon Blanc aux Yeux Bleus, la Bête Fantôme Blanche" {
		t.Errorf("French card name = %q", frCard.Name)
	}

	esSet := findSetState(states, domain.LangES)
	if esSet == nil {
		t.Fatal("missing Spanish set")
	}
	if esSet.Set.Name != "Maestros Magníficos" || esSet.Set.PropertyString(domain.PropertyRegionalCode) != "MAMS-SP" {
		t.Errorf("Spanish set = %q / %q", esSet.Set.Name, esSet.Set.PropertyString(domain.PropertyRegionalCode))
	}

	const cardURL = "https://yugipedia.com/wiki/Blue-Eyes_White_Dragon,_the_White_Phantom_Beast"
	if len(cardURLs[cardURL]) != 5 {
		t.Errorf("card URL refs = %d, want 5", len(cardURLs[cardURL]))
	}
}

func TestScrapeSetGuaranteedCopies(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	t.Run("non booster has guaranteed copies", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/Academy_Duel_Disk_Limited_Edition", []byte(academySetFixture), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}
		if len(states) != 1 || len(states[0].Prints) != 1 {
			t.Fatalf("expected one set and one print, got %d / %d", len(states), len(states[0].Prints))
		}
		print := states[0].Prints[0]
		if print.PropertyUint(domain.PropertyGuaranteedCopies) != 1 {
			t.Errorf("GuaranteedCopies = %d, want 1", print.PropertyUint(domain.PropertyGuaranteedCopies))
		}
		if print.Code != "ADD-JP001" || print.Rarity != "Common" {
			t.Errorf("print = %s / %s", print.Code, print.Rarity)
		}
	})

	t.Run("booster is random", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/Booster_Set", []byte(boosterSetFixture), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}
		if len(states) != 1 || len(states[0].Prints) != 1 {
			t.Fatalf("expected one set and one print, got %d / %d", len(states), len(states[0].Prints))
		}
		if copies := states[0].Prints[0].PropertyUint(domain.PropertyGuaranteedCopies); copies != 0 {
			t.Errorf("booster GuaranteedCopies = %d, want 0", copies)
		}
	})
}

const lazySetFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Lazy Set</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Collector's Set</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul>
      <li>LZY-EN (<abbr title="English">en</abbr>)</li>
      <li>LZY-FR (<abbr title="French">fr</abbr>)</li>
    </ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data">January 1, 2006</td></tr>
  <tr><th class="infobox-label">French</th><td class="infobox-data">January 2, 2006</td></tr>
</table>
<div class="set-list-tab" data-page="Set Card Lists:Lazy Set (TCG-EN)"></div>
<div class="set-list-tab" data-page="Set Card Lists:Lazy Set (TCG-FR)"></div>
</body></html>`

const frenchListFixture = `
<html><body>
<div class="set-list"><table>
  <tr><th>Card number</th><th>Name</th><th>French name</th><th>Rarity</th></tr>
  <tr>
    <td><a href="/wiki/LZY-FR001">LZY-FR001</a></td>
    <td>"<a href="/wiki/Lazy_Card">Lazy Card</a>"</td>
    <td>"Carte Paresseuse"</td>
    <td><a href="/wiki/Common">Common</a></td>
  </tr>
</table></div>
</body></html>`

func TestScrapeSetListPagesOnlyValidLanguages(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	fixture := strings.Replace(
		lazySetFixture,
		`<div class="set-list-tab" data-page="Set Card Lists:Lazy Set (TCG-FR)"></div>`,
		`<div class="set-list-tab" data-page="Set Card Lists:Lazy Set (TCG-FR)"></div>
<div class="set-list-tab" data-page="Set Card Lists:Lazy Set (TCG-DE)"></div>`,
		1,
	)
	states, _, pages, err := scrapeSet("https://yugipedia.com/wiki/Lazy_Set", []byte(fixture), Release{})
	if err != nil {
		t.Fatalf("scrape set: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 released sets, got %d", len(states))
	}
	if len(pages) != 2 {
		t.Fatalf("expected only the EN/FR list pages, got %d", len(pages))
	}
	for _, page := range pages {
		if strings.Contains(page.URL, "TCG-DE") {
			t.Errorf("unreleased language page was queued: %s", page.URL)
		}
	}
}

func TestScrapeSetListPagesSkipRedlinks(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	nav := `<div class="set-navigation">
<a href="/wiki/Set_Card_Lists:Lazy_Set_(TCG-EN)">English</a>
<a class="new" href="/wiki/Set_Card_Lists:Lazy_Set_(TCG-FR)?action=edit&amp;redlink=1">French</a>
</div>`
	fixture := strings.Replace(lazySetFixture, `<div class="set-list-tab"`, nav+`<div class="set-list-tab"`, 1)

	states, _, pages, err := scrapeSet("https://yugipedia.com/wiki/Lazy_Set", []byte(fixture), Release{})
	if err != nil {
		t.Fatalf("scrape set: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 released sets, got %d", len(states))
	}
	if len(pages) != 1 {
		t.Fatalf("expected only the existing EN list page, got %d", len(pages))
	}
	if !strings.Contains(pages[0].URL, "TCG-EN") {
		t.Errorf("unexpected list page: %s", pages[0].URL)
	}
}

func TestScrapeSetListPage(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	states, _, pages, err := scrapeSet("https://yugipedia.com/wiki/Lazy_Set", []byte(lazySetFixture), Release{})
	if err != nil {
		t.Fatalf("scrape set: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(states))
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 list pages, got %d", len(pages))
	}

	frSet := findSetState(states, domain.LangFR)
	if frSet == nil {
		t.Fatal("missing French set")
	}
	if len(frSet.Prints) != 0 {
		t.Fatalf("expected no inline French prints, got %d", len(frSet.Prints))
	}

	var frPage *setListPage
	for _, page := range pages {
		if strings.Contains(page.URL, "TCG-FR") {
			frPage = page
			break
		}
	}
	if frPage == nil {
		t.Fatal("missing French list page")
	}

	refs, err := scrapeSetListPage(frPage.URL, []byte(frenchListFixture), frPage)
	if err != nil {
		t.Fatalf("scrape list page: %v", err)
	}
	if len(frSet.Prints) != 1 {
		t.Fatalf("expected 1 French print, got %d", len(frSet.Prints))
	}
	print := frSet.Prints[0]
	if print.Code != "LZY-FR001" || print.Name != "Carte Paresseuse" {
		t.Errorf("French print = %q / %q", print.Code, print.Name)
	}
	if print.PropertyUint(domain.PropertyGuaranteedCopies) != 1 {
		t.Errorf("GuaranteedCopies = %d, want 1", print.PropertyUint(domain.PropertyGuaranteedCopies))
	}
	if len(refs) != 1 {
		t.Errorf("expected 1 card URL grouping, got %d", len(refs))
	}
}

const breakdownSetPage = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Breakdown Set</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Booster Pack</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul>
      <li>BDN-EN (<abbr title="English">en</abbr>)</li>
      <li>BDN-JP (<abbr title="Japanese">jp</abbr>)</li>
      <li>BDN-AE (<abbr title="Asian-English">ae</abbr>)</li>
      <li>BDN-KR (<abbr title="Korean">kr</abbr>)</li>
      <li>BDN-SC (<abbr title="Simplified Chinese">sc</abbr>)</li>
    </ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Japanese</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Asian-English</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Korean</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Simplified Chinese</th><td class="infobox-data">January 1, 2026</td></tr>
</table>

<div class="mw-heading mw-heading2"><h2 id="Breakdown">Breakdown</h2></div>
<div class="mw-heading mw-heading3"><h3 id="Japanese">Japanese</h3></div>
<ul><li>There are 5 cards per pack and 30 packs per box. The set contains 80 cards.</li></ul>
<div class="mw-heading mw-heading3"><h3 id="Simplified_Chinese">Simplified Chinese</h3></div>
<ul><li>There are 5 cards per pack and 30 packs per box. The set contains 100 cards.</li></ul>
<div class="mw-heading mw-heading3"><h3 id="TCG">TCG</h3></div>
<ul><li>There are 9 cards per pack and 24 packs per box. The set contains 100 cards.</li></ul>
<div class="mw-heading mw-heading2"><h2 id="Galleries">Galleries</h2></div>
</body></html>`

const breakdownMagnificentFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Magnificent Maestros</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Collector's Set</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul><li>MAMS-EN (<abbr title="English">en</abbr>)</li></ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data">January 1, 2026</td></tr>
</table>
<div class="mw-heading mw-heading2"><h2 id="Breakdown">Breakdown</h2></div>
<p>Each box contains:</p>
<ul>
  <li>3 Regular packs with 5 cards each.
    <ul><li>Pool A: Includes the 18 extended art cards.</li></ul>
  </li>
  <li>1 Bonus pack of 3 cards.
    <ul><li>Pool 1: Includes 19 cards.</li></ul>
  </li>
</ul>
<div class="mw-heading mw-heading2"><h2 id="Galleries">Galleries</h2></div>
</body></html>`

const breakdownImmortalFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Immortal Phoenix</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Booster Pack</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul>
      <li>IMPH-JP (<abbr title="Japanese">jp</abbr>)</li>
      <li>IMPH-SC (<abbr title="Simplified Chinese">sc</abbr>)</li>
      <li>IMPH-EN (<abbr title="English">en</abbr>)</li>
    </ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">Japanese</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Simplified Chinese</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">English</th><td class="infobox-data">January 1, 2026</td></tr>
</table>
<div class="mw-heading mw-heading2"><h2 id="Breakdown">Breakdown</h2></div>
<div class="mw-heading mw-heading3"><h3 id="Japanese">Japanese</h3></div>
<ul><li>There are 30 packs per box, and each pack contains 5 different cards from a set of 80 cards.</li></ul>
<table class="wikitable"><caption>Probability Table</caption><tr><td>76.0%</td></tr></table>
<div class="mw-heading mw-heading3"><h3 id="Simplified_Chinese">Simplified Chinese</h3></div>
<ul><li>There are 30 packs per box, and each pack contains 5 different cards from a set of 100 cards.</li></ul>
<table class="wikitable"><caption>Probability Table</caption><tr><td>76.0%</td></tr></table>
<div class="mw-heading mw-heading3"><h3 id="TCG">TCG</h3></div>
<ul><li>There are 9 cards per pack and 24 packs per box. The set contains 100 cards.</li></ul>
<div class="mw-heading mw-heading2"><h2 id="Galleries">Galleries</h2></div>
</body></html>`

const breakdownGoldRushFixture = `
<html><body>
<table class="infobox">
  <tr><th class="infobox-above">Gold Rush Pack</th></tr>
  <tr><th class="infobox-header">Set information</th></tr>
  <tr><th class="infobox-label">Medium</th><td class="infobox-data">Rush Duel</td></tr>
  <tr><th class="infobox-label">Type</th><td class="infobox-data">Booster Pack</td></tr>
  <tr><th class="infobox-label">Prefix</th><td class="infobox-data">
    <ul>
      <li>RD/GRP1-JP (<abbr title="Japanese">jp</abbr>)</li>
      <li>RD/GRP1-KR (<abbr title="Korean">kr</abbr>)</li>
    </ul>
  </td></tr>
  <tr><th class="infobox-header">Release dates</th></tr>
  <tr><th class="infobox-label">Japanese</th><td class="infobox-data">January 1, 2026</td></tr>
  <tr><th class="infobox-label">Korean</th><td class="infobox-data">January 1, 2026</td></tr>
</table>
<div class="mw-heading mw-heading2"><h2 id="Breakdown">Breakdown</h2></div>
<ul><li>There are 4 cards per pack and 15 packs per box. The set contains 55 cards.</li></ul>
<div class="mw-heading mw-heading2"><h2 id="Galleries">Galleries</h2></div>
</body></html>`

func TestScrapeSetBreakdown(t *testing.T) {
	previousMax := max
	max = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		max = previousMax
	})

	t.Run("language sections", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/BDN", []byte(breakdownSetPage), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}

		jp := findSetState(states, domain.LangJP)
		ae := findSetState(states, domain.LangAE)
		kr := findSetState(states, domain.LangKR)
		sc := findSetState(states, domain.LangSC)
		en := findSetState(states, domain.LangEN)

		if jp == nil || ae == nil || kr == nil || sc == nil || en == nil {
			t.Fatal("missing language sets")
		}
		if !strings.Contains(jp.Set.Description, "80 cards") {
			t.Errorf("JP description = %q", jp.Set.Description)
		}
		if !strings.Contains(ae.Set.Description, "80 cards") || !strings.Contains(kr.Set.Description, "80 cards") {
			t.Error("Japanese breakdown should apply to AE and KR")
		}
		if !strings.Contains(sc.Set.Description, "100 cards") {
			t.Errorf("SC description = %q", sc.Set.Description)
		}
		if !strings.Contains(en.Set.Description, "100 cards") {
			t.Errorf("EN description = %q", en.Set.Description)
		}

		if jp.Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 5 || jp.Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 30 {
			t.Errorf("JP quantities = %d/%d", jp.Set.PropertyUint(domain.PropertyQuantityCardsPerPack), jp.Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
		}
		if en.Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 9 || en.Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 24 {
			t.Errorf("EN quantities = %d/%d", en.Set.PropertyUint(domain.PropertyQuantityCardsPerPack), en.Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
		}
	})

	t.Run("magnificent maestros regular packs", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/MAMS", []byte(breakdownMagnificentFixture), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}
		if len(states) != 1 {
			t.Fatalf("expected 1 set, got %d", len(states))
		}
		set := states[0].Set
		if !strings.Contains(set.Description, "3 Regular packs") || !strings.Contains(set.Description, "Pool A") {
			t.Errorf("description = %q", set.Description)
		}
		if strings.Contains(set.Description, "Bonus pack") == false {
			t.Error("description should keep the bonus pack text")
		}
		if set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 5 || set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 3 {
			t.Errorf("quantities = %d/%d, want 5/3", set.PropertyUint(domain.PropertyQuantityCardsPerPack), set.PropertyUint(domain.PropertyQuantityPacksPerSet))
		}
	})

	t.Run("tables are omitted", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/IMPH", []byte(breakdownImmortalFixture), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}
		for _, state := range states {
			if strings.Contains(state.Set.Description, "Probability Table") || strings.Contains(state.Set.Description, "76.0%") {
				t.Errorf("%s description contains table content: %q", state.Set.Lang, state.Set.Description)
			}
		}
		jp := findSetState(states, domain.LangJP)
		en := findSetState(states, domain.LangEN)
		if jp == nil || en == nil {
			t.Fatal("missing sets")
		}
		if jp.Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 5 || jp.Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 30 {
			t.Errorf("JP quantities = %d/%d", jp.Set.PropertyUint(domain.PropertyQuantityCardsPerPack), jp.Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
		}
		if en.Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 9 || en.Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 24 {
			t.Errorf("EN quantities = %d/%d", en.Set.PropertyUint(domain.PropertyQuantityCardsPerPack), en.Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
		}
	})

	t.Run("generic rush duel breakdown", func(t *testing.T) {
		states, _, _, err := scrapeSet("https://yugipedia.com/wiki/RDGRP1", []byte(breakdownGoldRushFixture), Release{})
		if err != nil {
			t.Fatalf("scrape set: %v", err)
		}
		if len(states) != 2 {
			t.Fatalf("expected 2 sets, got %d", len(states))
		}
		for _, state := range states {
			if !strings.Contains(state.Set.Description, "4 cards per pack") {
				t.Errorf("%s description = %q", state.Set.Lang, state.Set.Description)
			}
			if state.Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 4 || state.Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 15 {
				t.Errorf("%s quantities = %d/%d", state.Set.Lang, state.Set.PropertyUint(domain.PropertyQuantityCardsPerPack), state.Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
			}
		}
	})
}

func findSetState(states []*setState, lang domain.Lang) *setState {
	for _, state := range states {
		if state != nil && state.Set != nil && state.Set.Lang == lang {
			return state
		}
	}
	return nil
}

func findPrint(prints []*domain.Item, lang domain.Lang, rarity string, extended bool) *domain.Item {
	for _, print := range prints {
		if print == nil {
			continue
		}
		if print.Lang == lang && strings.EqualFold(print.Rarity, rarity) && print.ExtendedArt == extended {
			return print
		}
	}
	return nil
}
