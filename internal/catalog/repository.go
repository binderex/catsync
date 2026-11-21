package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// upsertBatchSize is the number of rows sent in each INSERT. Large crawls
	// contain hundreds of thousands of prints, so small batches spend most of
	// their time on network round trips.
	upsertBatchSize = 250

	// upsertBatchTimeout bounds a single batch. It is deliberately applied per
	// batch instead of per high-level call: a full catalog has too many rows
	// for one fixed deadline, but a stalled statement should still fail.
	upsertBatchTimeout = 2 * time.Minute
)

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

	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	err := r.db.WithContext(batchCtx).
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

	err := createInBatches(ctx, r.db, infos, clause.OnConflict{
		Columns:   itemInfoConflictColumns,
		DoUpdates: clause.AssignmentColumns(itemInfoMutableColumns),
	})
	if err != nil {
		return fmt.Errorf("upsert item infos: %w", err)
	}

	return nil
}

// UpsertItems inserts or updates plain Item rows, such as playmats and sleeves.
func (r *Repository) UpsertItems(ctx context.Context, items []Item) error {
	return upsertItemRows(ctx, r.db, items)
}

// UpsertCardPrinted inserts or updates card print rows.
func (r *Repository) UpsertCardPrinted(ctx context.Context, prints []CardPrinted) error {
	return upsertItemRows(ctx, r.db, prints)
}

// UpsertSets inserts or updates set rows. The CardsPrinted association is not
// persisted here.
func (r *Repository) UpsertSets(ctx context.Context, sets []Set) error {
	return upsertItemRows(ctx, r.db, sets)
}

// UpsertSetsWithPrints is the bulk variant of UpsertSetWithPrints. It upserts
// every set, resolves the persisted IDs in one query, attaches those IDs to the
// prints, and then upserts every print. Sets and prints are written in batches
// instead of one transaction per set; re-running the operation is safe.
func (r *Repository) UpsertSetsWithPrints(ctx context.Context, sets []Set) error {
	sets = dedupeSets(sets)
	if len(sets) == 0 {
		return nil
	}

	printCount := 0
	for i := range sets {
		set := &sets[i]
		set.Kind = KindSet
		if set.ID == uuid.Nil {
			set.ID = uuid.New()
		}
		printCount += len(set.CardsPrinted)
	}

	if err := upsertItemRows(ctx, r.db, sets); err != nil {
		return fmt.Errorf("upsert sets: %w", err)
	}

	// The upsert preserves the existing ID on conflict. Resolve the actual IDs
	// after writing so the prints never point to a discarded set.
	persistedIDs, err := r.findSetIDs(ctx, sets)
	if err != nil {
		return err
	}

	for i := range sets {
		set := &sets[i]
		id, ok := persistedIDs[set.itemIdentity()]
		if !ok {
			return fmt.Errorf("persisted set %q was not found", set.Name)
		}
		set.ID = id

		for j := range set.CardsPrinted {
			print := &set.CardsPrinted[j]
			print.Kind = KindCard
			print.SetID = &set.ID
		}
	}

	log.Printf(
		"catalog: upserted %d sets; upserting %d prints",
		len(sets),
		printCount,
	)

	prints := make([]CardPrinted, 0, printCount)
	for i := range sets {
		prints = append(prints, sets[i].CardsPrinted...)
	}
	if err := upsertItemRows(ctx, r.db, prints); err != nil {
		return fmt.Errorf("upsert card prints: %w", err)
	}

	return nil
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

// findSetIDs resolves every persisted set in sets in one query. The returned
// map is keyed by the same identity used by the item upsert.
func (r *Repository) findSetIDs(ctx context.Context, sets []Set) (map[itemIdentity]uuid.UUID, error) {
	brands := make([]Brand, 0, len(sets))
	seen := make(map[Brand]bool, len(sets))
	for _, set := range sets {
		if seen[set.Brand] {
			continue
		}
		seen[set.Brand] = true
		brands = append(brands, set.Brand)
	}
	if len(brands) == 0 {
		return map[itemIdentity]uuid.UUID{}, nil
	}

	var rows []Set
	err := r.db.WithContext(ctx).
		Model(&Set{}).
		Select("id", "brand", "lang", "code", "item_info_id", "subtypes").
		Where("kind = ?", KindSet).
		Where("brand IN ?", brands).
		Where("rarity = '' AND edition = '' AND extended_art = false AND set_id IS NULL").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("find persisted sets: %w", err)
	}

	ids := make(map[itemIdentity]uuid.UUID, len(rows))
	for _, row := range rows {
		row.Kind = KindSet
		ids[row.itemIdentity()] = row.ID
	}

	return ids, nil
}

func upsertItemRows[T itemIdentityer](ctx context.Context, db *gorm.DB, rows []T) error {
	rows = dedupeItemRows(rows)
	if len(rows) == 0 {
		return nil
	}

	err := createInBatches(
		ctx,
		db.Omit(clause.Associations),
		rows,
		clause.OnConflict{
			Columns:   itemConflictColumns,
			DoUpdates: clause.AssignmentColumns(itemMutableColumns),
		},
	)
	if err != nil {
		return fmt.Errorf("upsert items: %w", err)
	}

	return nil
}

// createInBatches writes rows in batches and applies a fresh timeout to each
// batch. GORM's CreateInBatches uses one context for the whole slice, which
// makes a fixed timeout unusable for full catalog crawls.
func createInBatches[T any](
	ctx context.Context,
	db *gorm.DB,
	rows []T,
	clauses ...clause.Expression,
) error {
	for start := 0; start < len(rows); start += upsertBatchSize {
		end := min(start+upsertBatchSize, len(rows))

		batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
		batch := rows[start:end]
		err := db.WithContext(batchCtx).
			Clauses(clauses...).
			Create(&batch).Error
		cancel()

		if err != nil {
			return fmt.Errorf("batch rows %d-%d: %w", start, end, err)
		}
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

// dedupeSets keeps one row per set identity. The last set wins the metadata,
// but prints from every duplicate are retained.
func dedupeSets(sets []Set) []Set {
	positions := make(map[itemIdentity]int, len(sets))
	unique := make([]Set, 0, len(sets))

	for _, set := range sets {
		set.Kind = KindSet
		identity := set.itemIdentity()
		if position, ok := positions[identity]; ok {
			set.CardsPrinted = append(
				unique[position].CardsPrinted,
				set.CardsPrinted...,
			)
			unique[position] = set
			continue
		}

		positions[identity] = len(unique)
		unique = append(unique, set)
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
