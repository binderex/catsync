package catalog

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// catalogIdentityColumns are the columns that make up the composite unique
// index idx_catalog_identity. They are the conflict target for upserts.
var catalogIdentityColumns = []clause.Column{
	{Name: "external_id"},
	{Name: "brand"},
	{Name: "type"},
	{Name: "lang"},
	{Name: "code"},
	{Name: "rarity"},
	{Name: "edition"},
	{Name: "extended_artwork"},
	{Name: "set_name"},
}

// catalogMutableColumns are the columns updated when a row already exists.
// The identity columns, the primary key and created_at are intentionally
// excluded.
var catalogMutableColumns = []string{
	"name",
	"description",
	"archetype",
	"card_info",
	"card_images",
	"serial_code",
	"rarity_code",
	"guaranteed_copies",
	"print_image",
	"set_type",
	"set_code",
	"set_regional_code",
	"quantity_per_set",
	"quantity_per_box",
	"set_image",
	"set_box_image",
	"released_at",
	"updated_at",
}

// catalogIdentity is the set of fields that identify a catalog item. It
// mirrors idx_catalog_identity and is used to detect duplicates in memory.
type catalogIdentity struct {
	ExternalID      string
	Brand           Brand
	Type            CatalogType
	Lang            Lang
	Code            string
	Rarity          string
	Edition         string
	ExtendedArtwork bool
	SetName         string
}

// identity returns the composite key of the item.
func (i CatalogItem) identity() catalogIdentity {
	return catalogIdentity{
		ExternalID:      i.ExternalID,
		Brand:           i.Brand,
		Type:            i.Type,
		Lang:            i.Lang,
		Code:            i.Code,
		Rarity:          i.Rarity,
		Edition:         i.Edition,
		ExtendedArtwork: i.ExtendedArtwork,
		SetName:         i.SetName,
	}
}

// Repository persists catalog items.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a catalog repository backed by db.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetByName returns every catalog item whose name matches exactly.
func (r *Repository) GetByName(ctx context.Context, name string) ([]CatalogItem, error) {
	var items []CatalogItem

	err := r.db.WithContext(ctx).
		Where("name = ?", name).
		Order("id").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("get catalog items by name %q: %w", name, err)
	}

	return items, nil
}

// UpsertBatch inserts items that do not exist yet and updates the mutable
// fields of the ones that already exist, using idx_catalog_identity as the
// conflict target.
//
// The batch must not contain two items with the same identity: PostgreSQL
// rejects an ON CONFLICT statement that affects the same row twice. The
// service layer is responsible for removing those duplicates and for keeping
// the batch small enough.
func (r *Repository) UpsertBatch(ctx context.Context, items []CatalogItem) error {
	if len(items) == 0 {
		return nil
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   catalogIdentityColumns,
			DoUpdates: clause.AssignmentColumns(catalogMutableColumns),
		}).
		Create(&items).Error
	if err != nil {
		return fmt.Errorf("upsert catalog items: %w", err)
	}

	return nil
}
