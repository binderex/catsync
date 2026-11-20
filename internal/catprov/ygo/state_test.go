package ygo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/retrier"
)

func TestGetAllReleasesResumeComplete(t *testing.T) {
	previousPath := releasesStatePath
	releasesStatePath = filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() {
		releasesStatePath = previousPath
	})

	provider := NewYGOCatalogProvider()
	scrape := newEmptyReleaseScrape(provider, []Release{{
		Name:      "Saved Set",
		URL:       "https://yugipedia.com/wiki/Saved_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      catalog.LangEN,
		Brand:     catalog.BrandYGO,
	}})

	setInfo := catalog.NewSetInfo(catalog.BrandYGO, nil, "https://yugipedia.com/wiki/Saved_Set", "Booster pack", "SAV")
	setItem := catalog.NewItem(&setInfo.ID, catalog.KindSet, catalog.LangEN, "SAV", "Saved Set", "", catalog.Image{}, time.Time{})
	setItem.Brand = catalog.BrandYGO
	set := catalog.NewSet(*setItem, "SAV-EN", 9, 24)
	set.ItemInfo = setInfo
	set.ItemInfoID = &setInfo.ID

	cardInfo := catalog.NewCardInfo(catalog.BrandYGO, nil, "https://yugipedia.com/wiki/Saved_Card", "", "Monster", nil)
	cardItem := catalog.NewItem(&cardInfo.ID, catalog.KindCard, catalog.LangEN, "SAV-EN001", "Saved Card", "", catalog.Image{}, time.Time{})
	cardItem.Brand = catalog.BrandYGO
	print := catalog.NewCardPrinted(*cardItem, &set.ID, "001", "Common", "C", "", false, 1)
	print.ItemInfo = cardInfo

	scrape.states = []*setState{{Set: set, Prints: []*catalog.CardPrinted{print}}}
	if err := saveReleasesCheckpoint(releasesStatePath, scrape.snapshot(true)); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	sets, err := provider.GetAllReleases(context.Background(), true)
	if err != nil {
		t.Fatalf("get all releases: %v", err)
	}
	if len(sets) != 1 || len(sets[0].CardsPrinted) != 1 {
		t.Fatalf("sets = %d", len(sets))
	}
	if sets[0].CardsPrinted[0].Code != "SAV-EN001" || sets[0].CardsPrinted[0].ItemInfo == nil {
		t.Errorf("print = %+v", sets[0].CardsPrinted[0])
	}
}

func TestReleasesCheckpointRoundTrip(t *testing.T) {
	provider := NewYGOCatalogProvider()
	scrape := newEmptyReleaseScrape(provider, []Release{{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      catalog.LangEN,
		Medium:    MediumTCG,
		Brand:     catalog.BrandYGO,
	}})

	setInfo := catalog.NewSetInfo(catalog.BrandYGO, nil, "https://yugipedia.com/wiki/Test_Set", "Booster pack", "TST")
	setItem := catalog.NewItem(&setInfo.ID, catalog.KindSet, catalog.LangEN, "TST", "Test Set", "set description", catalog.Image{}, time.Time{})
	setItem.Brand = catalog.BrandYGO
	set := catalog.NewSet(*setItem, "TST-EN", 9, 24)
	set.ID = uuid.New()
	set.ItemInfo = setInfo
	set.ItemInfoID = &setInfo.ID

	cardInfo := catalog.NewCardInfo(catalog.BrandYGO, nil, "https://yugipedia.com/wiki/Test_Card", "", "Monster Normal", nil)
	cardItem := catalog.NewItem(&cardInfo.ID, catalog.KindCard, catalog.LangEN, "TST-EN001", "Test Card", "", catalog.Image{}, time.Time{})
	cardItem.Brand = catalog.BrandYGO
	print := catalog.NewCardPrinted(*cardItem, &set.ID, "001", "Common", "C", "1st Edition", false, 1)
	print.ItemInfo = cardInfo

	state := &setState{Set: set, Prints: []*catalog.CardPrinted{print}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[print] = state
	scrape.printsByCard["https://yugipedia.com/wiki/Test_Card"] = []*catalog.CardPrinted{print}
	scrape.galleryRefs["https://yugipedia.com/wiki/Card_Gallery:Test_Card"] = []*catalog.CardPrinted{print}
	scrape.cardResults["https://yugipedia.com/wiki/Test_Card"] = cardScrapeResult{
		Info:         cardInfo,
		Translations: map[catalog.Lang]cardLangData{catalog.LangEN: {Name: "Test Card"}},
		GalleryURL:   "https://yugipedia.com/wiki/Card_Gallery:Test_Card",
		Kind:         cardKindRushDuel,
		OK:           true,
	}
	scrape.galleryResults["https://yugipedia.com/wiki/Card_Gallery:Test_Card"] = []gallerySection{{
		Name:  "Worldwide English",
		Brand: catalog.BrandYGO,
		Lang:  catalog.LangEN,
		Prints: []galleryPrint{{
			Code:       "TST-EN001",
			Rarity:     "Common",
			RarityCode: "C",
			Edition:    "1st Edition",
			Image:      catalog.Image{URLSmall: "small", URLLarge: "large"},
		}},
	}}
	scrape.processed["https://yugipedia.com/wiki/Test_Set"] = true
	scrape.pages["https://yugipedia.com/wiki/Test_Set"] = retrier.URLState{
		URL:       "https://yugipedia.com/wiki/Test_Set",
		Visited:   true,
		Completed: true,
	}

	path := filepath.Join(t.TempDir(), "state.json")
	checkpoint := scrape.snapshot(false)
	if err := saveReleasesCheckpoint(path, checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	loaded, found, err := loadReleasesCheckpoint(path)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !found {
		t.Fatal("checkpoint was not found")
	}
	if loaded.Complete {
		t.Error("checkpoint should be incomplete")
	}

	restored, err := newReleaseScrapeFromCheckpoint(provider, loaded)
	if err != nil {
		t.Fatalf("restore checkpoint: %v", err)
	}
	if len(restored.states) != 1 || len(restored.states[0].Prints) != 1 {
		t.Fatalf("restored states = %d", len(restored.states))
	}
	if restored.states[0].Set.CardsPrinted != nil {
		t.Error("Set.CardsPrinted should only be materialized on demand")
	}
	if restored.states[0].Set.Description != "set description" {
		t.Errorf("set description = %q", restored.states[0].Set.Description)
	}
	if restored.states[0].Set.QuantityCardsPerPack != 9 || restored.states[0].Set.QuantityPacksPerSet != 24 {
		t.Errorf("quantities = %d/%d", restored.states[0].Set.QuantityCardsPerPack, restored.states[0].Set.QuantityPacksPerSet)
	}

	cardKey := "https://yugipedia.com/wiki/Test_Card"
	galleryKey := "https://yugipedia.com/wiki/Card_Gallery:Test_Card"
	if refs := restored.printsByCard[cardKey]; len(refs) != 1 || refs[0] != restored.states[0].Prints[0] {
		t.Error("card refs were not restored")
	}
	if refs := restored.galleryRefs[galleryKey]; len(refs) != 1 || refs[0] != restored.states[0].Prints[0] {
		t.Error("gallery refs were not restored")
	}
	if result, ok := restored.cardResults[cardKey]; !ok {
		t.Error("card result was not restored")
	} else if result.Kind != cardKindRushDuel || !result.OK {
		t.Errorf("card result = %+v", result)
	}
	if _, ok := restored.galleryResults[galleryKey]; !ok {
		t.Error("gallery result was not restored")
	}
	if !restored.processed["https://yugipedia.com/wiki/Test_Set"] {
		t.Error("processed state was not restored")
	}
	if !restored.pages["https://yugipedia.com/wiki/Test_Set"].Completed {
		t.Error("retrier state was not restored")
	}

	sets := restored.sets()
	if len(sets) != 1 || len(sets[0].CardsPrinted) != 1 {
		t.Fatalf("materialized sets = %d", len(sets))
	}
	if sets[0].CardsPrinted[0].ItemInfo == nil || sets[0].CardsPrinted[0].ItemInfo.Source != cardInfo.Source {
		t.Error("print ItemInfo was not preserved")
	}
}
