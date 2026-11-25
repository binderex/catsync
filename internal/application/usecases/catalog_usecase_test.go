package usecases

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

type fakeCatalogRepository struct {
	items   []domain.Item
	deleted []uuid.UUID
	calls   int
}

func (r *fakeCatalogRepository) Upsert(_ context.Context, items []domain.Item) error {
	r.calls++
	r.items = append([]domain.Item(nil), items...)
	return nil
}

func (r *fakeCatalogRepository) Delete(_ context.Context, ids []uuid.UUID) error {
	r.deleted = append(r.deleted, ids...)
	return nil
}

func TestCatalogUpsertNormalizesItems(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	items := []domain.Item{
		{
			Kind:   domain.KindSleeve,
			Brand:  domain.BrandDS,
			Lang:   domain.LangEN,
			Code:   "SKU-1",
			Source: "https://example.com/sleeve",
		},
		{
			ID:     uuid.Nil,
			Kind:   domain.KindSet,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB",
			Source: "https://example.com/set",
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if repo.calls != 1 {
		t.Fatalf("calls = %d", repo.calls)
	}
	if len(repo.items) != 2 {
		t.Fatalf("items = %d", len(repo.items))
	}
	if repo.items[0].ID == uuid.Nil {
		t.Error("sleeve id was not assigned")
	}
	if repo.items[1].ID == uuid.Nil {
		t.Error("set id was not assigned")
	}
	if repo.items[0].Properties == nil || repo.items[1].Properties == nil {
		t.Error("properties was not initialized")
	}
}

func TestCatalogUpsertRemovesDuplicatesBeforeUpsert(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	firstID := uuid.New()
	secondID := uuid.New()
	items := []domain.Item{
		{
			ID:     firstID,
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB-001",
			Name:   "First",
			Source: "https://example.com/card",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Effect",
			},
		},
		{
			ID:     secondID,
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB-001",
			Name:   "Second",
			Source: "https://example.com/card",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Effect",
			},
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if len(repo.items) != 1 {
		t.Fatalf("items = %d, want 1", len(repo.items))
	}
	if repo.items[0].ID != firstID {
		t.Errorf("id = %v, want %v", repo.items[0].ID, firstID)
	}
	if repo.items[0].Name != "Second" {
		t.Errorf("name = %q, want Second", repo.items[0].Name)
	}
}

func TestCatalogUpsertRejectsEmptySource(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	_, err := uc.Upsert(context.Background(), []domain.Item{{
		Kind:  domain.KindCard,
		Brand: domain.BrandYGO,
		Lang:  domain.LangEN,
		Code:  "LOB-001",
		Properties: map[string]any{
			domain.PropertyCardTypes: "Monster Effect",
		},
	}})
	if err == nil {
		t.Fatal("expected empty source error")
	}
	if repo.calls != 0 {
		t.Fatal("repository must not be called for an empty source")
	}
}

func TestCatalogUpsertSkipsUnknownNames(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	items := []domain.Item{
		{
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "MF03-EN008",
			Name:   "unknown",
			Source: "https://yugipedia.com/wiki/MF03-EN008",
		},
		{
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "MF03-EN001",
			Name:   "Elemental HERO Avian",
			Source: "https://yugipedia.com/wiki/Elemental_HERO_Avian",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Normal",
			},
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if len(repo.items) != 1 || repo.items[0].Name != "Elemental HERO Avian" {
		t.Fatalf("items = %+v", repo.items)
	}
}

func TestCatalogUpsertSkipsNonStandardCards(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	card := func(code, name, cardTypes string) domain.Item {
		return domain.Item{
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   code,
			Name:   name,
			Source: "https://yugipedia.com/wiki/" + code,
			Properties: map[string]any{
				domain.PropertyCardTypes: cardTypes,
			},
		}
	}

	items := []domain.Item{
		card("STD-MON", "Monster", "Monster Effect"),
		card("STD-SPL", "Spell", "Spell Normal"),
		card("STD-TRP", "Trap", "Trap Normal"),
		card("TOKEN", "Token", "Token Normal"),
		card("STRATEGY", "Strategy", "Strategy"),
		card("SKILL", "Skill", "Yugi - Skill"),
		card("TIP", "Tip", "Tip"),
		card("CHECK", "Checklist", "Checklist"),
		card("NON", "Non-game", "Non-game"),
		card("EMPTY", "No types", ""),
		{
			Kind:   domain.KindSet,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "STD",
			Name:   "Standard Set",
			Source: "https://yugipedia.com/wiki/Standard_Set",
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}

	kept := make(map[string]bool, len(repo.items))
	for _, item := range repo.items {
		kept[item.Code] = true
	}
	for _, code := range []string{"STD-MON", "STD-SPL", "STD-TRP", "STD"} {
		if !kept[code] {
			t.Errorf("standard item %s was dropped", code)
		}
	}
	for _, code := range []string{"TOKEN", "STRATEGY", "SKILL", "TIP", "CHECK", "NON", "EMPTY"} {
		if kept[code] {
			t.Errorf("non-standard item %s was kept", code)
		}
	}
}

func TestCatalogUpsertPrunesTokenOnlySets(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	tokenSetID := uuid.New()
	normalSetID := uuid.New()

	items := []domain.Item{
		{
			ID:     tokenSetID,
			Kind:   domain.KindSet,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangJP,
			Code:   "SSTK",
			Name:   "Satellite Shop Limited Event",
			Source: "https://yugipedia.com/wiki/Satellite_Shop_Limited_Event_participation_cards",
		},
		{
			ID:     uuid.New(),
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangJP,
			Code:   "SSTK-JP001",
			Name:   "Token",
			SetID:  &tokenSetID,
			Source: "https://yugipedia.com/wiki/SSTK-JP001",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Token Normal",
			},
		},
		{
			ID:     normalSetID,
			Kind:   domain.KindSet,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB",
			Name:   "Legend of Blue Eyes",
			Source: "https://yugipedia.com/wiki/Legend_of_Blue_Eyes",
		},
		{
			ID:     uuid.New(),
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB-001",
			Name:   "Blue-Eyes White Dragon",
			SetID:  &normalSetID,
			Source: "https://yugipedia.com/wiki/Blue-Eyes_White_Dragon",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Normal",
			},
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != tokenSetID {
		t.Fatalf("deleted = %v, want [%s]", repo.deleted, tokenSetID)
	}

	for _, item := range repo.items {
		if item.ID == tokenSetID {
			t.Errorf("token-only set was upserted")
		}
	}
	if len(repo.items) != 2 {
		t.Errorf("upserted = %+v", repo.items)
	}
}

func TestCatalogUpsertDropsOrphanCards(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	setID := uuid.New()
	missingSetID := uuid.New()

	items := []domain.Item{
		{
			ID:     setID,
			Kind:   domain.KindSet,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB",
			Name:   "Legend of Blue Eyes",
			Source: "https://yugipedia.com/wiki/Legend_of_Blue_Eyes",
		},
		{
			ID:     uuid.New(),
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "LOB-001",
			Name:   "Blue-Eyes White Dragon",
			SetID:  &setID,
			Source: "https://yugipedia.com/wiki/Blue-Eyes_White_Dragon",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Normal",
			},
		},
		{
			ID:     uuid.New(),
			Kind:   domain.KindCard,
			Brand:  domain.BrandYGO,
			Lang:   domain.LangEN,
			Code:   "RD/ORP1-JP001",
			Name:   "Rush card",
			SetID:  &missingSetID,
			Source: "https://yugipedia.com/wiki/Rush_card",
			Properties: map[string]any{
				domain.PropertyCardTypes: "Monster Normal",
			},
		},
	}

	count, err := uc.Upsert(context.Background(), items)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	for _, item := range repo.items {
		if item.Code == "RD/ORP1-JP001" {
			t.Errorf("orphan card was upserted")
		}
	}
}

func TestCatalogUpsertEmpty(t *testing.T) {
	repo := &fakeCatalogRepository{}
	uc := NewCatalogUseCase(repo)

	count, err := uc.Upsert(context.Background(), nil)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if repo.calls != 0 {
		t.Fatalf("calls = %d", repo.calls)
	}
}
