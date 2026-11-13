package catalog

import (
	"context"
	"fmt"
)

// upsertBatchSize keeps each INSERT well below PostgreSQL's 65535 bind
// parameter limit (CatalogItem has fewer than 30 columns).
const upsertBatchSize = 1000

// Service contains the catalog use cases.
type Service struct {
	repo *Repository
}

// NewService creates a catalog service backed by repo.
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetByName returns every catalog item whose name matches exactly.
func (s *Service) GetByName(ctx context.Context, name string) ([]CatalogItem, error) {
	return s.repo.GetByName(ctx, name)
}

// Upsert inserts new catalog items and updates the mutable fields of the ones
// that already exist.
//
// Before writing, it removes duplicated items using the same composite key as
// idx_catalog_identity (the last occurrence wins) and then sends the items to
// the repository in batches, so a large input does not exceed the database
// parameter limit.
func (s *Service) Upsert(ctx context.Context, items []CatalogItem) error {
	if len(items) == 0 {
		return nil
	}

	unique := dedupeByIdentity(items)

	for start := 0; start < len(unique); start += upsertBatchSize {
		end := min(start+upsertBatchSize, len(unique))

		if err := s.repo.UpsertBatch(ctx, unique[start:end]); err != nil {
			return fmt.Errorf("upsert catalog items [%d:%d]: %w", start, end, err)
		}
	}

	return nil
}

// dedupeByIdentity keeps a single item per composite identity. When the same
// identity appears more than once, the last occurrence wins.
func dedupeByIdentity(items []CatalogItem) []CatalogItem {
	positions := make(map[catalogIdentity]int, len(items))
	unique := make([]CatalogItem, 0, len(items))

	for _, item := range items {
		key := item.identity()

		if pos, ok := positions[key]; ok {
			unique[pos] = item
			continue
		}

		positions[key] = len(unique)
		unique = append(unique, item)
	}

	return unique
}
