package catprov

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/catalog"
)

func newTestProvider(t *testing.T) *YGOCatProv {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	return NewYGOCatProv()
}

func TestGetCardsDateFiltering(t *testing.T) {
	prov := newTestProvider(t)

	// With a cutoff in the future every released Ojama card should be returned.
	all, err := prov.GetCards("ojama", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(ojama): %v", err)
	}
	if len(all) == 0 {
		t.Fatal("GetCards(ojama): expected at least one card")
	}

	for _, item := range all {
		if item.Name == "" {
			t.Errorf("GetCards(ojama): item %s has an empty name", item.ExternalID)
		}
		if item.Brand != catalog.BrandYGO {
			t.Errorf("GetCards(ojama): item %s has brand %q, want %q", item.Name, item.Brand, catalog.BrandYGO)
		}
		if item.Type != catalog.CatalogTypeCard {
			t.Errorf("GetCards(ojama): item %s has type %q, want %q", item.Name, item.Type, catalog.CatalogTypeCard)
		}
	}

	// With a cutoff in the past, cards released after it must be filtered out.
	past, err := prov.GetCards("ojama", time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
	if err != nil && !errors.Is(err, ErrCardsNotFound) {
		t.Fatalf("GetCards(ojama) with past cutoff: %v", err)
	}
	if len(past) >= len(all) {
		t.Fatalf("GetCards(ojama): past cutoff returned %d cards, want fewer than %d", len(past), len(all))
	}
}

func TestGetCardPrints(t *testing.T) {
	prov := newTestProvider(t)

	card := catalog.CatalogItem{
		Name:       "Dark Magician",
		ExternalID: "46986414",
	}

	if _, err := prov.GetCardPrints(card); err != nil {
		t.Fatalf("GetCardPrints(%q): %v", card.Name, err)
	}
}

func TestGetCardPrintsFields(t *testing.T) {
	prov := newTestProvider(t)

	cards, err := prov.GetCards("Ojamassimilation", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(Ojamassimilation): %v", err)
	}

	var card catalog.CatalogItem
	for _, item := range cards {
		if item.Name == "Ojamassimilation" {
			card = item
			break
		}
	}
	if card.Name == "" {
		t.Fatal("card Ojamassimilation not found")
	}

	prints, err := prov.GetCardPrints(card)
	if err != nil {
		t.Fatalf("GetCardPrints(%q): %v", card.Name, err)
	}
	if len(prints) == 0 {
		t.Fatal("expected at least one print")
	}
	t.Logf("prints for %s: %d", card.Name, len(prints))

	for _, print := range prints {
		if print.ExternalID == "" {
			t.Error("print has empty ExternalID")
		}
		if print.Name == "" {
			t.Error("print has empty Name")
		}
		if print.Brand != catalog.BrandYGO {
			t.Errorf("print %s has brand %q", print.Code, print.Brand)
		}
		if print.Type != catalog.CatalogTypeCard {
			t.Errorf("print %s has type %q", print.Code, print.Type)
		}
		if print.Lang == "" {
			t.Errorf("print %s has empty Lang", print.Code)
		}
		if print.Code == "" {
			t.Errorf("print %s has empty Code", print.Name)
		}
		if print.SetName == "" {
			t.Errorf("print %s has empty SetName", print.Code)
		}
		if print.Rarity == "" {
			t.Errorf("print %s has empty Rarity", print.Code)
		}
		if print.ReleasedAt.IsZero() {
			t.Errorf("print %s has empty ReleasedAt", print.Code)
		}
	}
}

func TestGetCardPrintsEditions(t *testing.T) {
	prov := newTestProvider(t)

	cards, err := prov.GetCards("Exodia the Forbidden One", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(Exodia): %v", err)
	}

	var card catalog.CatalogItem
	for _, item := range cards {
		if item.Name == "Exodia the Forbidden One" {
			card = item
			break
		}
	}
	if card.Name == "" {
		t.Fatal("card Exodia the Forbidden One not found")
	}

	prints, err := prov.GetCardPrints(card)
	if err != nil {
		t.Fatalf("GetCardPrints(Exodia): %v", err)
	}

	codeEditions := make(map[string]map[string]bool)
	for _, print := range prints {
		if print.Code == "" || print.Edition == "" {
			continue
		}
		if codeEditions[print.Code] == nil {
			codeEditions[print.Code] = make(map[string]bool)
		}
		codeEditions[print.Code][print.Edition] = true
		if len(codeEditions[print.Code]) >= 2 {
			t.Logf("found code %q with editions %v", print.Code, codeEditions[print.Code])
			return
		}
	}

	t.Fatal("expected the same code with different editions")
}

func TestGetCardPrintsSkipsPlaceholderCodes(t *testing.T) {
	prov := newTestProvider(t)
	prov.now = time.Date(2028, time.January, 1, 0, 0, 0, 0, time.UTC)

	cards, err := prov.GetCards("Contrast HERO Chaos", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(Contrast HERO Chaos): %v", err)
	}

	var card catalog.CatalogItem
	for _, item := range cards {
		if item.Name == "Contrast HERO Chaos" {
			card = item
			break
		}
	}
	if card.Name == "" {
		t.Fatal("card Contrast HERO Chaos not found")
	}

	prints, err := prov.GetCardPrints(card)
	if err != nil {
		t.Fatalf("GetCardPrints(Contrast HERO Chaos): %v", err)
	}
	if len(prints) == 0 {
		t.Fatal("expected at least one print")
	}

	for _, print := range prints {
		if strings.Contains(print.Code, "?") {
			t.Errorf("placeholder code leaked into prints: %s", print.Code)
		}
	}
}

func TestGetCardPrintsExtendedArtwork(t *testing.T) {
	prov := newTestProvider(t)

	cards, err := prov.GetCards("Red Nova Dragon - Burning Soul", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(Red Nova Dragon - Burning Soul): %v", err)
	}

	var card catalog.CatalogItem
	for _, item := range cards {
		if item.Name == "Red Nova Dragon - Burning Soul" {
			card = item
			break
		}
	}
	if card.Name == "" {
		t.Fatal("card Red Nova Dragon - Burning Soul not found")
	}

	prints, err := prov.GetCardPrints(card)
	if err != nil {
		t.Fatalf("GetCardPrints(Red Nova Dragon - Burning Soul): %v", err)
	}

	variants := make(map[string]map[bool]bool)
	for _, print := range prints {
		key := print.Code + "|" + print.Rarity + "|" + print.Edition
		if variants[key] == nil {
			variants[key] = make(map[bool]bool)
		}
		variants[key][print.ExtendedArtwork] = true
	}

	for key, flags := range variants {
		if flags[true] && flags[false] {
			t.Logf("found extended artwork variant for %s", key)
			return
		}
	}

	t.Fatal("expected a print with and without Extended artwork for the same code, rarity and edition")
}

func TestGetCardsAndCardPrints(t *testing.T) {
	prov := newTestProvider(t)

	cards, err := prov.GetCards("ojama", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(ojama): %v", err)
	}

	if len(cards) > 5 {
		cards = cards[:5]
	}
	if len(cards) == 0 {
		t.Fatal("GetCards(ojama): expected at least one card")
	}

	for _, card := range cards {
		if _, err := prov.GetCardPrints(card); err != nil {
			t.Fatalf("GetCardPrints(%q): %v", card.Name, err)
		}
	}
}

func TestGetSets(t *testing.T) {
	prov := newTestProvider(t)

	cards, err := prov.GetCards("ojama", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetCards(ojama): %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("GetCards(ojama): expected at least one card")
	}

	var prints []catalog.CatalogItem
	for _, card := range cards {
		got, err := prov.GetCardPrints(card)
		if err != nil {
			// Some cards (e.g. Ojamandala) do not have a prints gallery.
			if errors.Is(err, ErrCardPrintsNotFound) {
				continue
			}
			t.Fatalf("GetCardPrints(%q): %v", card.Name, err)
		}
		prints = append(prints, got...)
	}
	if len(prints) == 0 {
		t.Fatal("expected at least one card print")
	}

	items, err := prov.GetSets(prints)
	if err != nil {
		t.Fatalf("GetSets(): %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one set or updated card")
	}

	var sets, setCards int
	var hasReleasedSet bool
	for _, item := range items {
		switch item.Type {
		case catalog.CatalogTypeSet:
			sets++
			if !item.ReleasedAt.IsZero() {
				hasReleasedSet = true
			}
			if item.SetName == "" {
				t.Errorf("set %s has empty SetName", item.ExternalID)
			}
		case catalog.CatalogTypeCard:
			setCards++
		}
	}

	if sets == 0 {
		t.Error("expected at least one set")
	}
	if setCards == 0 {
		t.Error("expected at least one updated card")
	}
	if !hasReleasedSet {
		t.Error("expected at least one set with ReleasedAt")
	}

}

func TestGetSetsGuaranteedCopies(t *testing.T) {
	prov := newTestProvider(t)

	const setName = "THE CHRONICLES DECK: Spirit Charmers (All-Foil Edition)"

	cards := []catalog.CatalogItem{
		{
			SetName: setName,
			Lang:    catalog.LangEN,
			Brand:   catalog.BrandYGO,
			Type:    catalog.CatalogTypeCard,
			Code:    "CH02-EN001",
			Rarity:  "Secret Rare",
		},
		{
			SetName: setName,
			Lang:    catalog.LangEN,
			Brand:   catalog.BrandYGO,
			Type:    catalog.CatalogTypeCard,
			Code:    "CH02-EN001",
			Rarity:  "Ultra Rare",
		},
		{
			SetName: "Structure Deck: Fire Kings",
			Lang:    catalog.LangEN,
			Brand:   catalog.BrandYGO,
			Type:    catalog.CatalogTypeCard,
			Code:    "SR14-EN045",
			Rarity:  "Super Rare",
		},
	}

	items, err := prov.GetSets(cards)
	if err != nil {
		t.Fatalf("GetSets(%q): %v", setName, err)
	}

	var secretRare, ultraRare, fireKing *catalog.CatalogItem
	for i := range items {
		switch {
		case items[i].Code == "CH02-EN001" && items[i].Rarity == "Secret Rare":
			secretRare = &items[i]
		case items[i].Code == "CH02-EN001" && items[i].Rarity == "Ultra Rare":
			ultraRare = &items[i]
		case items[i].Code == "SR14-EN045":
			fireKing = &items[i]
		}
	}

	if secretRare == nil {
		t.Fatal("Secret Rare CH02-EN001 not returned")
	}
	if ultraRare == nil {
		t.Fatal("Ultra Rare CH02-EN001 not returned")
	}
	if fireKing == nil {
		t.Fatal("SR14-EN045 not returned")
	}
	if secretRare.GuaranteedCopies != 0 {
		t.Errorf("Secret Rare GuaranteedCopies = %d, want 0", secretRare.GuaranteedCopies)
	}
	if ultraRare.GuaranteedCopies != 1 {
		t.Errorf("Ultra Rare GuaranteedCopies = %d, want 1", ultraRare.GuaranteedCopies)
	}
	if fireKing.GuaranteedCopies != 1 {
		t.Errorf("SR14-EN045 GuaranteedCopies = %d, want 1", fireKing.GuaranteedCopies)
	}
}

func TestGetSetsImmortalPhoenix(t *testing.T) {
	prov := newTestProvider(t)
	prov.now = time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)

	const setName = "Immortal Phoenix"
	cards := []catalog.CatalogItem{
		{SetName: setName, Lang: catalog.LangEN, Brand: catalog.BrandYGO, Type: catalog.CatalogTypeCard, Code: "IMPH-EN001", Rarity: "Secret Rare"},
		{SetName: setName, Lang: catalog.LangJP, Brand: catalog.BrandYGO, Type: catalog.CatalogTypeCard, Code: "IMPH-JP001", Rarity: "Secret Rare"},
		{SetName: setName, Lang: catalog.LangSC, Brand: catalog.BrandYGO, Type: catalog.CatalogTypeCard, Code: "IMPH-SC001", Rarity: "Secret Rare"},
		{SetName: setName, Lang: catalog.LangJP, Brand: catalog.BrandYGO, Type: catalog.CatalogTypeCard, Code: "IMPH-JP0??", Rarity: "Secret Rare"},
	}

	items, err := prov.GetSets(cards)
	if err != nil {
		t.Fatalf("GetSets(%q): %v", setName, err)
	}

	var sawJPSet, sawJPCard bool
	for _, item := range items {
		if item.Lang != catalog.LangJP {
			t.Errorf("expected only JP products, got %s (%s)", item.Lang, item.Name)
		}
		if strings.Contains(item.Code, "?") {
			t.Errorf("placeholder code leaked into results: %s", item.Code)
		}

		switch item.Type {
		case catalog.CatalogTypeSet:
			sawJPSet = true
			if item.ReleasedAt.IsZero() {
				t.Error("JP set is missing ReleasedAt")
			}
		case catalog.CatalogTypeCard:
			if item.Code == "IMPH-JP001" {
				sawJPCard = true
			}
		}
	}

	if !sawJPSet {
		t.Error("expected the JP set to be returned")
	}
	if !sawJPCard {
		t.Error("expected the JP card IMPH-JP001 to be returned")
	}
}
