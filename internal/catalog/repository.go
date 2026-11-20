package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const upsertBatchSize = 50

var (
	// ErrItemInfoFilterRequired is returned when GetItemInfo is called without
	// source, brand or kind.
	ErrItemInfoFilterRequired = errors.New("item info lookup requires at least one filter")
	// ErrItemInfoSourceRequired is returned when an item info without source is
	// sent to the upsert.
	ErrItemInfoSourceRequired = errors.New("item info source is required")
)

// itemInfoConflictColumns matches idx_item_infos_source. Source is unique, so
// conflicting on it also satisfies idx_item_infos_identity.
var itemInfoConflictColumns = []clause.Column{
	{Name: "source"},
}

// itemInfoMutableColumns are updated when the source already exists.
var itemInfoMutableColumns = []string{
	"images",
	"verified_source",
	"set_type",
	"set_code",
	"archetype",
	"card_types",
	"properties",
	"updated_at",
}

// itemConflictColumns matches idx_items_identity. It is the conflict target for
// CardPrinted, Set and plain Item rows. Subtypes are part of the identity so
// product variants that only differ by subtype (for example sleeve sizes) do
// not overwrite each other.
var itemConflictColumns = []clause.Column{
	{Name: "kind"},
	{Name: "brand"},
	{Name: "lang"},
	{Name: "code"},
	{Name: "item_info_id"},
	{Name: "rarity"},
	{Name: "edition"},
	{Name: "extended_art"},
	{Name: "set_id"},
	{Name: "subtypes"},
}

// itemMutableColumns are updated when the item identity already exists.
var itemMutableColumns = []string{
	"name",
	"description",
	"image",
	"release_at",
	"serial_code",
	"rarity_code",
	"guaranteed_copies",
	"regional_code",
	"quantity_cards_per_pack",
	"quantity_packs_per_set",
	"updated_at",
}

// Repository persists catalog items and item infos.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a catalog repository backed by db.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetItemInfosByBrand returns every item info for brand. An empty brand
// returns the whole item_infos table.
func (r *Repository) GetItemInfosByBrand(ctx context.Context, brand Brand) ([]ItemInfo, error) {
	query := r.db.WithContext(ctx).Model(&ItemInfo{})
	if brand != "" {
		query = query.Where("brand = ?", brand)
	}

	var infos []ItemInfo
	if err := query.Find(&infos).Error; err != nil {
		return nil, fmt.Errorf("get item infos by brand: %w", err)
	}

	return infos, nil
}

// GetItemInfo returns the first item info matching the provided non-empty
// filters. At least one filter must be provided.
func (r *Repository) GetItemInfo(ctx context.Context, source string, brand Brand, kind Kind) (*ItemInfo, error) {
	query := r.db.WithContext(ctx).Model(&ItemInfo{})
	hasFilter := false

	if source != "" {
		query = query.Where("source = ?", source)
		hasFilter = true
	}
	if brand != "" {
		query = query.Where("brand = ?", brand)
		hasFilter = true
	}
	if kind != "" {
		query = query.Where("kind = ?", kind)
		hasFilter = true
	}

	if !hasFilter {
		return nil, ErrItemInfoFilterRequired
	}

	var info ItemInfo
	if err := query.First(&info).Error; err != nil {
		return nil, fmt.Errorf("get item info: %w", err)
	}

	return &info, nil
}

// UpsertItemInfo inserts or updates one item info by source.
func (r *Repository) UpsertItemInfo(ctx context.Context, info *ItemInfo) error {
	if info == nil {
		return nil
	}
	if info.Source == "" {
		return ErrItemInfoSourceRequired
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   itemInfoConflictColumns,
			DoUpdates: clause.AssignmentColumns(itemInfoMutableColumns),
		}).
		Create(info).Error
	if err != nil {
		return fmt.Errorf("upsert item info: %w", err)
	}

	return nil
}

// UpsertItemInfos inserts or updates a batch of item infos by source.
func (r *Repository) UpsertItemInfos(ctx context.Context, infos []ItemInfo) error {
	if len(infos) == 0 {
		return nil
	}

	for _, info := range infos {
		if info.Source == "" {
			return ErrItemInfoSourceRequired
		}
	}

	infos = dedupeItemInfos(infos)

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   itemInfoConflictColumns,
			DoUpdates: clause.AssignmentColumns(itemInfoMutableColumns),
		}).
		CreateInBatches(&infos, upsertBatchSize).Error
	if err != nil {
		return fmt.Errorf("upsert item infos: %w", err)
	}

	return nil
}

// UpsertItems inserts or updates plain Item rows, such as playmats and sleeves.
func (r *Repository) UpsertItems(ctx context.Context, items []Item) error {
	return upsertItemRows(r.db.WithContext(ctx), items)
}

// UpsertCardPrinted inserts or updates card print rows.
func (r *Repository) UpsertCardPrinted(ctx context.Context, prints []CardPrinted) error {
	return upsertItemRows(r.db.WithContext(ctx), prints)
}

// UpsertSets inserts or updates set rows. The CardsPrinted association is not
// persisted here.
func (r *Repository) UpsertSets(ctx context.Context, sets []Set) error {
	return upsertItemRows(r.db.WithContext(ctx), sets)
}

// UpsertSetWithPrints inserts or updates a set and its card prints in one
// transaction. The prints are attached to set.ID, overwriting any different
// SetID they had. Prints that are no longer present are not deleted.
func (r *Repository) UpsertSetWithPrints(ctx context.Context, set *Set, prints []CardPrinted) error {
	if set == nil {
		return nil
	}
	if set.ID == uuid.Nil {
		set.ID = uuid.New()
	}
	set.Kind = KindSet

	rows := make([]CardPrinted, len(prints))
	copy(rows, prints)
	for i := range rows {
		rows[i].Kind = KindCard
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewRepository(tx)

		if err := repo.UpsertSets(ctx, []Set{*set}); err != nil {
			return err
		}

		// The upsert preserves the existing row ID on conflict. Resolve it
		// before attaching the prints so they never point to a discarded ID.
		persistedID, err := repo.findSetID(ctx, *set)
		if err != nil {
			return err
		}
		set.ID = persistedID
		for i := range rows {
			rows[i].SetID = &set.ID
		}

		return repo.UpsertCardPrinted(ctx, rows)
	})
	if err != nil {
		return fmt.Errorf("upsert set with prints: %w", err)
	}

	return nil
}

func (r *Repository) findSetID(ctx context.Context, set Set) (uuid.UUID, error) {
	query := r.db.WithContext(ctx).
		Model(&Item{}).
		Where(
			"kind = ? AND brand = ? AND lang = ? AND code = ? AND rarity = '' AND edition = '' AND extended_art = false AND set_id IS NULL",
			KindSet,
			set.Brand,
			set.Lang,
			set.Code,
		)

	if set.ItemInfoID != nil {
		query = query.Where("item_info_id = ?", *set.ItemInfoID)
	} else {
		query = query.Where("item_info_id IS NULL")
	}

	var candidates []Item
	if err := query.Find(&candidates).Error; err != nil {
		return uuid.Nil, fmt.Errorf("find persisted set: %w", err)
	}

	wantSubtypes := subtypesKey(set.Subtypes)
	for _, candidate := range candidates {
		if subtypesKey(candidate.Subtypes) == wantSubtypes {
			return candidate.ID, nil
		}
	}

	return uuid.Nil, fmt.Errorf("find persisted set id: %w", gorm.ErrRecordNotFound)
}

func upsertItemRows[T itemIdentityer](db *gorm.DB, rows []T) error {
	rows = dedupeItemRows(rows)
	if len(rows) == 0 {
		return nil
	}

	err := db.
		Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns:   itemConflictColumns,
			DoUpdates: clause.AssignmentColumns(itemMutableColumns),
		}).
		CreateInBatches(&rows, upsertBatchSize).Error
	if err != nil {
		return fmt.Errorf("upsert items: %w", err)
	}

	return nil
}

type itemIdentity struct {
	Kind        Kind
	Brand       Brand
	Lang        Lang
	Code        string
	ItemInfoID  string
	Rarity      string
	Edition     string
	ExtendedArt bool
	SetID       string
	Subtypes    string
}

type itemIdentityer interface {
	itemIdentity() itemIdentity
}

func (i Item) itemIdentity() itemIdentity {
	return itemIdentity{
		Kind:       i.Kind,
		Brand:      i.Brand,
		Lang:       i.Lang,
		Code:       i.Code,
		ItemInfoID: uuidString(i.ItemInfoID),
		Subtypes:   subtypesKey(i.Subtypes),
	}
}

// subtypesKey returns a canonical representation of the subtypes map for the
// in-memory dedupe. encoding/json sorts map keys, so the result is stable.
func subtypesKey(subtypes map[string]any) string {
	if subtypes == nil {
		return ""
	}

	data, err := json.Marshal(subtypes)
	if err != nil {
		return fmt.Sprintf("%v", subtypes)
	}

	return string(data)
}

func (c CardPrinted) itemIdentity() itemIdentity {
	identity := c.Item.itemIdentity()
	identity.Rarity = c.Rarity
	identity.Edition = c.Edition
	identity.ExtendedArt = c.ExtendedArt
	identity.SetID = uuidString(c.SetID)
	return identity
}

func (s Set) itemIdentity() itemIdentity {
	return s.Item.itemIdentity()
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func dedupeItemRows[T itemIdentityer](rows []T) []T {
	positions := make(map[itemIdentity]int, len(rows))
	unique := make([]T, 0, len(rows))

	for _, row := range rows {
		identity := row.itemIdentity()
		if position, ok := positions[identity]; ok {
			unique[position] = row
			continue
		}

		positions[identity] = len(unique)
		unique = append(unique, row)
	}

	return unique
}

func dedupeItemInfos(infos []ItemInfo) []ItemInfo {
	positions := make(map[string]int, len(infos))
	unique := make([]ItemInfo, 0, len(infos))

	for _, info := range infos {
		if position, ok := positions[info.Source]; ok {
			unique[position] = info
			continue
		}

		positions[info.Source] = len(unique)
		unique = append(unique, info)
	}

	return unique
}
