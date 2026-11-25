package domain

import (
	"testing"

	"github.com/google/uuid"
)

func baseItem() Item {
	return Item{
		Kind:    KindCard,
		Brand:   BrandYGO,
		Lang:    LangEN,
		Code:    "LOB-001",
		Rarity:  "Ultra Rare",
		Edition: "1st Edition",
		Source:  "https://example.com/card",
		SetID:   nil,
		Properties: map[string]any{
			"variant": map[string]any{"finish": "foil"},
		},
	}
}

func TestIdentityIncludesSourceAndVariant(t *testing.T) {
	item := baseItem()
	base := item.IdentityKey()

	withSource := item
	withSource.Source = "https://example.com/other"
	if withSource.IdentityKey() == base {
		t.Error("source did not change the identity")
	}

	withVariant := item
	withVariant.Properties = map[string]any{
		"variant": map[string]any{"finish": "matte"},
	}
	if withVariant.IdentityKey() == base {
		t.Error("variant did not change the identity")
	}
}

func TestIdentityIgnoresVerifiedSource(t *testing.T) {
	item := baseItem()
	verified := item
	verified.VerifiedSource = true

	if verified.IdentityKey() != item.IdentityKey() {
		t.Error("verified source changed the identity")
	}
}

func TestIdentityIgnoresMutableFields(t *testing.T) {
	item := baseItem()
	base := item.IdentityKey()

	mutable := item
	mutable.Name = "Other name"
	mutable.Description = "Other description"
	mutable.Image = Image{URLSmall: "small", URLLarge: "large"}
	mutable.Properties = map[string]any{
		"variant":   map[string]any{"finish": "foil"},
		"set_type":  "Booster",
		"card_type": "Monster",
	}

	if mutable.IdentityKey() != base {
		t.Error("mutable fields changed the identity")
	}
}

func TestDeduplicateItemsRemovesDuplicates(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	items := []Item{
		{
			ID:     firstID,
			Kind:   KindCard,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB-001",
			Name:   "First",
			Source: "https://example.com/card",
		},
		{
			ID:     secondID,
			Kind:   KindCard,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB-001",
			Name:   "Second",
			Source: "https://example.com/card",
		},
	}

	unique := DeduplicateItems(items)
	if len(unique) != 1 {
		t.Fatalf("unique = %d", len(unique))
	}
	if unique[0].ID != firstID {
		t.Errorf("id = %v, want %v", unique[0].ID, firstID)
	}
	if unique[0].Name != "Second" {
		t.Errorf("name = %q, want Second", unique[0].Name)
	}

}

func TestDeduplicateItemsDedupesAfterSetRemap(t *testing.T) {
	keptSetID := uuid.New()
	removedSetID := uuid.New()
	items := []Item{
		{
			ID:     keptSetID,
			Kind:   KindSet,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB",
			Source: "set",
		},
		{
			ID:     removedSetID,
			Kind:   KindSet,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB",
			Source: "set",
		},
		{
			ID:     uuid.New(),
			Kind:   KindCard,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB-001",
			Source: "card",
			SetID:  &removedSetID,
			Name:   "First",
		},
		{
			ID:     uuid.New(),
			Kind:   KindCard,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB-001",
			Source: "card",
			SetID:  &keptSetID,
			Name:   "Second",
		},
	}

	unique := DeduplicateItems(items)
	if len(unique) != 2 {
		t.Fatalf("unique = %d, want 1 set and 1 print", len(unique))
	}

	var cards int
	for _, item := range unique {
		if item.Kind != KindCard {
			continue
		}
		cards++
		if item.SetID == nil || *item.SetID != keptSetID {
			t.Errorf("print set id = %v, want %v", item.SetID, keptSetID)
		}
		if item.Name != "Second" {
			t.Errorf("print name = %q, want Second", item.Name)
		}
	}
	if cards != 1 {
		t.Errorf("cards = %d, want 1", cards)
	}
}

func TestDeduplicateItemsRemapsSetReferences(t *testing.T) {
	keptSetID := uuid.New()
	removedSetID := uuid.New()
	print := Item{
		ID:     uuid.New(),
		Kind:   KindCard,
		Brand:  BrandYGO,
		Lang:   LangEN,
		Code:   "LOB-001",
		SetID:  &removedSetID,
		Source: "card",
	}
	items := []Item{
		{
			ID:     keptSetID,
			Kind:   KindSet,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB",
			Source: "set",
		},
		{
			ID:     removedSetID,
			Kind:   KindSet,
			Brand:  BrandYGO,
			Lang:   LangEN,
			Code:   "LOB",
			Source: "set",
		},
		print,
	}

	unique := DeduplicateItems(items)
	if len(unique) != 2 {
		t.Fatalf("unique = %d", len(unique))
	}

	for _, item := range unique {
		if item.Kind != KindCard {
			continue
		}
		if item.SetID == nil || *item.SetID != keptSetID {
			t.Errorf("print set id = %v, want %v", item.SetID, keptSetID)
		}
	}
}

func TestNewCardPrintedUsesIdentityEdition(t *testing.T) {
	setID := uuid.New()
	item := NewCardPrinted(
		baseItem(),
		&setID,
		"LOB-001",
		"Ultra Rare",
		"UR",
		"1st Edition",
		true,
		2,
	)

	if item.Kind != KindCard {
		t.Errorf("kind = %q", item.Kind)
	}
	if item.Edition != "1st Edition" {
		t.Errorf("edition = %q", item.Edition)
	}
	if item.SetID == nil || *item.SetID != setID {
		t.Errorf("set id = %v", item.SetID)
	}
	if item.Properties["serial_code"] != "LOB-001" {
		t.Errorf("serial code = %v", item.Properties["serial_code"])
	}
}
