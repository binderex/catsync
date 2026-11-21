package catalog

import (
	"testing"

	"github.com/google/uuid"
)

func TestDedupeSetsMergesPrintsAndKeepsLastMetadata(t *testing.T) {
	code := "LOB"
	firstPrint := CardPrinted{ID: uuid.New(), Code: "LOB-001", Name: "First print"}
	secondPrint := CardPrinted{ID: uuid.New(), Code: "LOB-002", Name: "Second print"}

	sets := []Set{
		{
			ID:           uuid.New(),
			Kind:         KindSet,
			Brand:        BrandYGO,
			Lang:         LangEN,
			Code:         code,
			Name:         "First set",
			CardsPrinted: []CardPrinted{firstPrint},
		},
		{
			ID:           uuid.New(),
			Kind:         KindSet,
			Brand:        BrandYGO,
			Lang:         LangEN,
			Code:         code,
			Name:         "Second set",
			CardsPrinted: []CardPrinted{secondPrint},
		},
	}

	unique := dedupeSets(sets)

	if len(unique) != 1 {
		t.Fatalf("expected 1 set, got %d", len(unique))
	}
	if unique[0].Name != "Second set" {
		t.Fatalf("expected last metadata to win, got %q", unique[0].Name)
	}
	if len(unique[0].CardsPrinted) != 2 {
		t.Fatalf("expected both prints, got %d", len(unique[0].CardsPrinted))
	}
	if unique[0].CardsPrinted[0].Name != "First print" ||
		unique[0].CardsPrinted[1].Name != "Second print" {
		t.Fatalf("unexpected prints: %#v", unique[0].CardsPrinted)
	}
}

func TestDedupeSetsNormalizesKind(t *testing.T) {
	sets := []Set{
		{Brand: BrandYGO, Lang: LangEN, Code: "LOB"},
		{Kind: KindSet, Brand: BrandYGO, Lang: LangEN, Code: "LOB"},
	}

	unique := dedupeSets(sets)

	if len(unique) != 1 {
		t.Fatalf("expected 1 set, got %d", len(unique))
	}
	if unique[0].Kind != KindSet {
		t.Fatalf("expected kind %q, got %q", KindSet, unique[0].Kind)
	}
}

func TestDedupeItemRowsKeepsLastRow(t *testing.T) {
	first := CardPrinted{Item: Item{
		ID:    uuid.New(),
		Kind:  KindCard,
		Brand: BrandYGO,
		Lang:  LangEN,
		Code:  "LOB-001",
		Name:  "First",
	}}
	second := first
	second.ID = uuid.New()
	second.Name = "Second"

	unique := dedupeItemRows([]CardPrinted{first, second})

	if len(unique) != 1 {
		t.Fatalf("expected 1 row, got %d", len(unique))
	}
	if unique[0].Name != "Second" {
		t.Fatalf("expected last row to win, got %q", unique[0].Name)
	}
}

func TestSubtypesKeyIsStable(t *testing.T) {
	left := subtypesKey(map[string]any{"finish": "matte", "size": "standard"})
	right := subtypesKey(map[string]any{"size": "standard", "finish": "matte"})

	if left != right {
		t.Fatalf("expected stable key, got %q and %q", left, right)
	}
	if subtypesKey(nil) != "" {
		t.Fatal("nil subtypes must produce an empty key")
	}
}
