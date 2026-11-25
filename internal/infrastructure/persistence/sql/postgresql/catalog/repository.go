package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/binderex/catsync/internal/domain"
)

const (
	upsertBatchSize    = 250
	upsertBatchTimeout = 2 * time.Minute
)

var itemIdentityColumns = []clause.Column{
	{Name: "kind"},
	{Name: "brand"},
	{Name: "lang"},
	{Name: "code"},
	{Name: "set_id"},
	{Name: "rarity"},
	{Name: "edition"},
	{Name: "extended_art"},
	{Name: "source"},
	{Name: "variant_key"},
}

var itemMutableColumns = []string{
	"name",
	"description",
	"image",
	"release_at",
	"properties",
	"verified_source",
	"updated_at",
}

// Repository persists the denormalized catalog items in PostgreSQL.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a catalog repository backed by db.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Upsert inserts or updates items by their unique identity. The use case must
// normalize and deduplicate the items first; sets are written before prints so
// their persisted IDs can be attached to the card rows.
func (r *Repository) Upsert(ctx context.Context, items []domain.Item) error {
	if len(items) == 0 {
		return nil
	}

	sets, others := splitSets(items)
	brands := setBrands(sets)

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(sets) > 0 {
			if err := upsertBatches(ctx, tx, sets); err != nil {
				return fmt.Errorf("upsert sets: %w", err)
			}

			persisted, err := loadSetIDs(ctx, tx, brands)
			if err != nil {
				return err
			}

			idMap := make(map[uuid.UUID]uuid.UUID, len(sets))
			for _, set := range sets {
				id, ok := persisted[set.IdentityKey()]
				if !ok {
					return fmt.Errorf("persisted set %q was not found", set.Name)
				}
				idMap[set.ID] = id
			}

			for i := range others {
				if others[i].SetID == nil {
					continue
				}
				if id, ok := idMap[*others[i].SetID]; ok {
					setID := id
					others[i].SetID = &setID
				}
			}
		}

		if len(others) > 0 {
			if err := upsertBatches(ctx, tx, others); err != nil {
				return fmt.Errorf("upsert items: %w", err)
			}
		}

		return propagateSourceVerification(ctx, tx, items)
	})
}

// Delete removes the given sets and any rows that still reference them.
func (r *Repository) Delete(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("set_id IN ?", ids).Delete(&domain.Item{}).Error; err != nil {
			return fmt.Errorf("delete rows for sets: %w", err)
		}
		if err := tx.Where("id IN ?", ids).Delete(&domain.Item{}).Error; err != nil {
			return fmt.Errorf("delete sets: %w", err)
		}
		return nil
	})
}

func splitSets(items []domain.Item) (sets, others []domain.Item) {
	for _, item := range items {
		if item.Kind == domain.KindSet {
			sets = append(sets, item)
			continue
		}
		others = append(others, item)
	}
	return sets, others
}

func setBrands(sets []domain.Item) []domain.Brand {
	brands := make([]domain.Brand, 0, len(sets))
	seen := make(map[domain.Brand]bool, len(sets))
	for _, set := range sets {
		if seen[set.Brand] {
			continue
		}
		seen[set.Brand] = true
		brands = append(brands, set.Brand)
	}
	return brands
}

func loadSetIDs(
	ctx context.Context,
	db *gorm.DB,
	brands []domain.Brand,
) (map[string]uuid.UUID, error) {
	var rows []domain.Item
	err := db.WithContext(ctx).
		Model(&domain.Item{}).
		Select(
			"id", "kind", "brand", "lang", "code", "set_id",
			"rarity", "edition", "extended_art", "source", "verified_source",
			"properties",
		).
		Where("kind = ?", domain.KindSet).
		Where("brand IN ?", brands).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load persisted sets: %w", err)
	}

	ids := make(map[string]uuid.UUID, len(rows))
	for _, row := range rows {
		ids[row.IdentityKey()] = row.ID
	}
	return ids, nil
}

func upsertBatches(ctx context.Context, db *gorm.DB, items []domain.Item) error {
	for start := 0; start < len(items); start += upsertBatchSize {
		end := min(start+upsertBatchSize, len(items))

		batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
		batch := items[start:end]
		err := db.WithContext(batchCtx).
			Omit(clause.Associations).
			Clauses(clause.OnConflict{
				Columns:   itemIdentityColumns,
				DoUpdates: clause.AssignmentColumns(itemMutableColumns),
			}).
			Create(&batch).Error
		cancel()

		if err != nil {
			return fmt.Errorf("batch rows %d-%d: %w", start, end, err)
		}
	}

	return nil
}

// sourceVerificationValues returns the last verification value seen for each
// non-empty source.
func sourceVerificationValues(items []domain.Item) map[string]bool {
	values := make(map[string]bool)
	for _, item := range items {
		if item.Source == "" {
			continue
		}
		values[item.Source] = item.VerifiedSource
	}
	return values
}

// propagateSourceVerification updates every row that shares a source with the
// incoming items. Source remains part of the identity; verification is mutable
// so changing it updates the whole source instead of creating new rows.
func propagateSourceVerification(
	ctx context.Context,
	db *gorm.DB,
	items []domain.Item,
) error {
	values := sourceVerificationValues(items)
	if len(values) == 0 {
		return nil
	}

	sources := make([]string, 0, len(values))
	for source := range values {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	for start := 0; start < len(sources); start += upsertBatchSize {
		end := min(start+upsertBatchSize, len(sources))
		batch := sources[start:end]

		var builder strings.Builder
		builder.WriteString(`UPDATE items AS i SET verified_source = v.verified FROM (VALUES `)
		args := make([]any, 0, len(batch)*2)
		for index, source := range batch {
			if index > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString("(?::text, ?::boolean)")
			args = append(args, source, values[source])
		}
		builder.WriteString(`) AS v(source, verified) WHERE i.source = v.source`)

		batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
		err := db.WithContext(batchCtx).Exec(builder.String(), args...).Error
		cancel()
		if err != nil {
			return fmt.Errorf("propagate source verification: %w", err)
		}
	}

	return nil
}
