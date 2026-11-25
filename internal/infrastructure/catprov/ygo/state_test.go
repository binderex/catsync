package ygo

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

func testSetItem(code, name, description string) *domain.Item {
	set := &domain.Item{
		Kind:           domain.KindSet,
		Brand:          domain.BrandYGO,
		Lang:           domain.LangEN,
		Code:           code,
		Name:           name,
		Description:    description,
		Source:         "https://yugipedia.com/wiki/" + name,
		VerifiedSource: true,
		Properties:     make(map[string]any),
	}
	set.ID = uuid.NewString()
	set.SetProperty(domain.PropertyRegionalCode, code+"-EN")
	set.SetProperty(domain.PropertyQuantityCardsPerPack, 9)
	set.SetProperty(domain.PropertyQuantityPacksPerSet, 24)
	return set
}

func testPrintItem(code string, setID *string, info *domain.Item) *domain.Item {
	print := &domain.Item{
		Kind:        domain.KindCard,
		Brand:       domain.BrandYGO,
		Lang:        domain.LangEN,
		Code:        code,
		Name:        code,
		Description: "",
		SetID:       setID,
		Rarity:      "Common",
		Edition:     "1st Edition",
		Properties:  make(map[string]any),
	}
	print.ID = uuid.NewString()
	print.SetProperty(domain.PropertySerialCode, "001")
	print.SetProperty(domain.PropertyRarityCode, "C")
	print.SetProperty(domain.PropertyGuaranteedCopies, 1)
	if info != nil {
		applyCardInfo(print, info)
	}
	return print
}

func TestGetAllReleasesResumeComplete(t *testing.T) {
	provider := NewYGOCatalogProvider()
	scrape := newEmptyReleaseScrape(provider, []Release{{
		Name:      "Saved Set",
		URL:       "https://yugipedia.com/wiki/Saved_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}})

	set := testSetItem("SAV", "Saved Set", "")
	cardInfo := newCardItem(domain.BrandYGO, nil, "https://yugipedia.com/wiki/Saved_Card", "", "Monster", nil)
	print := testPrintItem("SAV-EN001", &set.ID, cardInfo)

	scrape.states = []*setState{{Set: set, Prints: []*domain.Item{print}}}
	data, err := encodeReleasesCheckpoint(scrape.snapshot(true))
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}

	items, err := provider.GetAllReleases(context.Background(), domain.ProviderOptions{
		Resume:      true,
		ResumeState: &domain.RetrierRun{ProviderData: data},
	})
	if err != nil {
		t.Fatalf("get all releases: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Kind != domain.KindSet || items[1].Kind != domain.KindCard {
		t.Errorf("items = %+v", items)
	}
	if items[1].Code != "SAV-EN001" || items[1].Source != "https://yugipedia.com/wiki/Saved_Card" {
		t.Errorf("print = %+v", items[1])
	}
}

func TestReleasesCheckpointRoundTrip(t *testing.T) {
	provider := NewYGOCatalogProvider()
	scrape := newEmptyReleaseScrape(provider, []Release{{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Medium:    MediumTCG,
		Brand:     domain.BrandYGO,
	}})

	set := testSetItem("TST", "Test Set", "set description")
	cardInfo := newCardItem(domain.BrandYGO, nil, "https://yugipedia.com/wiki/Test_Card", "", "Monster Normal", nil)
	print := testPrintItem("TST-EN001", &set.ID, cardInfo)

	state := &setState{Set: set, Prints: []*domain.Item{print}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[print] = state
	scrape.printsByCard["https://yugipedia.com/wiki/Test_Card"] = []*domain.Item{print}
	scrape.galleryRefs["https://yugipedia.com/wiki/Card_Gallery:Test_Card"] = []*domain.Item{print}
	scrape.cardResults["https://yugipedia.com/wiki/Test_Card"] = cardScrapeResult{
		Info:         cardInfo,
		Translations: map[domain.Lang]cardLangData{domain.LangEN: {Name: "Test Card"}},
		GalleryURL:   "https://yugipedia.com/wiki/Card_Gallery:Test_Card",
		Kind:         cardKindRushDuel,
		OK:           true,
	}
	scrape.galleryResults["https://yugipedia.com/wiki/Card_Gallery:Test_Card"] = []gallerySection{{
		Name:  "Worldwide English",
		Brand: domain.BrandYGO,
		Lang:  domain.LangEN,
		Prints: []galleryPrint{{
			Code:       "TST-EN001",
			Rarity:     "Common",
			RarityCode: "C",
			Edition:    "1st Edition",
			Image:      domain.Image{URLSmall: "small", URLLarge: "large"},
		}},
	}}
	scrape.processed["https://yugipedia.com/wiki/Test_Set"] = true
	scrape.pages["https://yugipedia.com/wiki/Test_Set"] = domain.URLState{
		URL:       "https://yugipedia.com/wiki/Test_Set",
		Visited:   true,
		Completed: true,
	}

	checkpoint := scrape.snapshot(false)
	data, err := encodeReleasesCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}

	loaded, found, err := decodeReleasesCheckpoint(data)
	if err != nil {
		t.Fatalf("decode checkpoint: %v", err)
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
	if restored.states[0].Set.Description != "set description" {
		t.Errorf("set description = %q", restored.states[0].Set.Description)
	}
	if restored.states[0].Set.PropertyUint(domain.PropertyQuantityCardsPerPack) != 9 ||
		restored.states[0].Set.PropertyUint(domain.PropertyQuantityPacksPerSet) != 24 {
		t.Errorf("quantities = %d/%d",
			restored.states[0].Set.PropertyUint(domain.PropertyQuantityCardsPerPack),
			restored.states[0].Set.PropertyUint(domain.PropertyQuantityPacksPerSet))
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

	items := restored.items()
	if len(items) != 2 {
		t.Fatalf("materialized items = %d", len(items))
	}
	if items[1].Source != "https://yugipedia.com/wiki/Test_Card" {
		t.Error("print source was not preserved")
	}
}
