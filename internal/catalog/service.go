package catalog

import (
	"context"
	"time"
)

// upsertBatchTimeout prevents a stalled database connection from blocking the
// whole synchronization forever.
const upsertBatchTimeout = 2 * time.Minute

// Service contains the catalog use cases.
type Service struct {
	repo *Repository
}

// NewService creates a catalog service backed by repo.
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetItemInfo returns the first item info matching the provided non-empty
// filters. At least one filter must be provided.
func (s *Service) GetItemInfo(ctx context.Context, source string, brand Brand, kind Kind) (*ItemInfo, error) {
	return s.repo.GetItemInfo(ctx, source, brand, kind)
}

// UpsertItemInfo inserts or updates one item info by source.
func (s *Service) UpsertItemInfo(ctx context.Context, info *ItemInfo) error {
	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	return s.repo.UpsertItemInfo(batchCtx, info)
}

// UpsertItemInfos inserts or updates a batch of item infos by source.
func (s *Service) UpsertItemInfos(ctx context.Context, infos []ItemInfo) error {
	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	return s.repo.UpsertItemInfos(batchCtx, infos)
}

// UpsertItems inserts or updates plain Item rows, such as playmats and sleeves.
func (s *Service) UpsertItems(ctx context.Context, items []Item) error {
	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	return s.repo.UpsertItems(batchCtx, items)
}

// UpsertCardPrinted inserts or updates card print rows.
func (s *Service) UpsertCardPrinted(ctx context.Context, prints []CardPrinted) error {
	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	return s.repo.UpsertCardPrinted(batchCtx, prints)
}

// UpsertSets inserts or updates set rows.
func (s *Service) UpsertSets(ctx context.Context, sets []Set) error {
	batchCtx, cancel := context.WithTimeout(ctx, upsertBatchTimeout)
	defer cancel()

	return s.repo.UpsertSets(batchCtx, sets)
}
