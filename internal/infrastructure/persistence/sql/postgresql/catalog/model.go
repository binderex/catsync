package catalog

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/domain"
)

// itemModel is the GORM model for the items table. It owns the persistence
// types and tags; domain.Item stays free of GORM concerns. The JSON tags used
// by the JSONB columns live in domain.Image and the Properties values.
type itemModel struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	Kind        string     `gorm:"type:text;not null;index:idx_items_kind"`
	Brand       string     `gorm:"type:text;not null;index:idx_items_brand"`
	Lang        string     `gorm:"type:char(2);not null;index:idx_items_lang"`
	Code        string     `gorm:"type:varchar(50);not null;default:'';index:idx_items_code"`
	SetID       *uuid.UUID `gorm:"type:uuid;index:idx_items_set_id"`
	Rarity      string     `gorm:"type:varchar(50);not null;default:''"`
	Edition     string     `gorm:"type:text;not null;default:''"`
	ExtendedArt bool       `gorm:"not null;default:false"`

	Source         string `gorm:"type:text;not null;default:''"`
	VerifiedSource bool   `gorm:"not null;default:false"`

	Name        string       `gorm:"type:text;not null;default:'';index:idx_items_name"`
	Description string       `gorm:"type:text;not null;default:''"`
	Image       domain.Image `gorm:"type:jsonb;serializer:json;not null;default:'{}'"`
	ReleaseAt   time.Time

	Properties map[string]any `gorm:"type:jsonb;serializer:json;not null;default:'{}'"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName maps the model to the existing denormalized items table.
func (itemModel) TableName() string {
	return "items"
}

// itemToModel converts a domain item into its persistence representation.
func itemToModel(item domain.Item) (itemModel, error) {
	id, err := parseID(item.ID)
	if err != nil {
		return itemModel{}, fmt.Errorf("item %s %q id: %w", item.Kind, item.Code, err)
	}

	var setID *uuid.UUID
	if item.SetID != nil {
		parsed, err := parseID(*item.SetID)
		if err != nil {
			return itemModel{}, fmt.Errorf("item %s %q set id: %w", item.Kind, item.Code, err)
		}
		setID = &parsed
	}

	return itemModel{
		ID:             id,
		Kind:           string(item.Kind),
		Brand:          string(item.Brand),
		Lang:           string(item.Lang),
		Code:           item.Code,
		SetID:          setID,
		Rarity:         item.Rarity,
		Edition:        item.Edition,
		ExtendedArt:    item.ExtendedArt,
		Source:         item.Source,
		VerifiedSource: item.VerifiedSource,
		Name:           item.Name,
		Description:    item.Description,
		Image:          item.Image,
		ReleaseAt:      item.ReleaseAt,
		Properties:     item.Properties,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}, nil
}

// itemsToModels converts a batch of domain items into persistence rows.
func itemsToModels(items []domain.Item) ([]itemModel, error) {
	models := make([]itemModel, len(items))
	for index, item := range items {
		model, err := itemToModel(item)
		if err != nil {
			return nil, err
		}
		models[index] = model
	}
	return models, nil
}

// toDomain converts a persistence row back into the domain entity.
func (m itemModel) toDomain() domain.Item {
	item := domain.Item{
		ID:             m.ID.String(),
		Kind:           domain.Kind(m.Kind),
		Brand:          domain.Brand(m.Brand),
		Lang:           domain.Lang(m.Lang),
		Code:           m.Code,
		Rarity:         m.Rarity,
		Edition:        m.Edition,
		ExtendedArt:    m.ExtendedArt,
		Source:         m.Source,
		VerifiedSource: m.VerifiedSource,
		Name:           m.Name,
		Description:    m.Description,
		Image:          m.Image,
		ReleaseAt:      m.ReleaseAt,
		Properties:     m.Properties,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
	if m.SetID != nil {
		setID := m.SetID.String()
		item.SetID = &setID
	}
	return item
}

func parseID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse uuid %q: %w", value, err)
	}
	return id, nil
}

func parseIDs(ids []string) ([]uuid.UUID, error) {
	parsed := make([]uuid.UUID, len(ids))
	for index, id := range ids {
		value, err := parseID(id)
		if err != nil {
			return nil, err
		}
		parsed[index] = value
	}
	return parsed, nil
}
