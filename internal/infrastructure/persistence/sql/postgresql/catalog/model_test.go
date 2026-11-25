package catalog

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

func TestItemModelRoundTrip(t *testing.T) {
	setID := uuid.NewString()
	item := domain.Item{
		ID:             uuid.NewString(),
		Kind:           domain.KindCard,
		Brand:          domain.BrandYGO,
		Lang:           domain.LangEN,
		Code:           "LOB-001",
		SetID:          &setID,
		Rarity:         "Ultra Rare",
		Edition:        "1st Edition",
		ExtendedArt:    true,
		Source:         "https://example.com/card",
		VerifiedSource: true,
		Name:           "Blue-Eyes White Dragon",
		Description:    "A legendary dragon.",
		Image:          domain.Image{URLSmall: "small", URLLarge: "large"},
		ReleaseAt:      time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		Properties: map[string]any{
			domain.PropertyImages: []domain.Image{
				{URLSmall: "small", URLLarge: "large"},
			},
			domain.PropertyVariant: map[string]any{"finish": "foil"},
		},
		CreatedAt: time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC),
	}

	model, err := itemToModel(item)
	if err != nil {
		t.Fatalf("itemToModel: %v", err)
	}
	if model.ID.String() != item.ID {
		t.Errorf("model id = %q, want %q", model.ID, item.ID)
	}
	if model.SetID == nil || model.SetID.String() != setID {
		t.Errorf("model set id = %v, want %q", model.SetID, setID)
	}
	if model.Kind != string(item.Kind) || model.Brand != string(item.Brand) || model.Lang != string(item.Lang) {
		t.Errorf("model identity = %q/%q/%q", model.Kind, model.Brand, model.Lang)
	}

	restored := model.toDomain()
	if !reflect.DeepEqual(restored, item) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", restored, item)
	}
}

func TestItemModelRejectsInvalidID(t *testing.T) {
	if _, err := itemToModel(domain.Item{ID: "not-a-uuid"}); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestItemModelRejectsInvalidSetID(t *testing.T) {
	setID := "not-a-uuid"
	if _, err := itemToModel(domain.Item{ID: uuid.NewString(), SetID: &setID}); err == nil {
		t.Fatal("expected invalid set id error")
	}
}

func TestItemModelTableName(t *testing.T) {
	if got := (itemModel{}).TableName(); got != "items" {
		t.Fatalf("TableName = %q, want items", got)
	}
}
