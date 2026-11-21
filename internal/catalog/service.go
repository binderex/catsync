package catalog

import "context"

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

// GetItemInfosByBrand returns every item info for brand. An empty brand
// returns all item infos.
func (s *Service) GetItemInfosByBrand(ctx context.Context, brand Brand) ([]ItemInfo, error) {
	return s.repo.GetItemInfosByBrand(ctx, brand)
}

// UpsertItemInfo inserts or updates one item info by source.
func (s *Service) UpsertItemInfo(ctx context.Context, info *ItemInfo) error {
	return s.repo.UpsertItemInfo(ctx, info)
}

// UpsertItemInfos inserts or updates a batch of item infos by source.
func (s *Service) UpsertItemInfos(ctx context.Context, infos []ItemInfo) error {
	return s.repo.UpsertItemInfos(ctx, infos)
}

// UpsertItems inserts or updates plain Item rows, such as playmats and sleeves.
func (s *Service) UpsertItems(ctx context.Context, items []Item) error {
	return s.repo.UpsertItems(ctx, items)
}

// UpsertCardPrinted inserts or updates card print rows.
func (s *Service) UpsertCardPrinted(ctx context.Context, prints []CardPrinted) error {
	return s.repo.UpsertCardPrinted(ctx, prints)
}

// UpsertSets inserts or updates set rows.
func (s *Service) UpsertSets(ctx context.Context, sets []Set) error {
	return s.repo.UpsertSets(ctx, sets)
}

// UpsertSetWithPrints inserts or updates a set and its card prints atomically.
func (s *Service) UpsertSetWithPrints(ctx context.Context, set *Set, prints []CardPrinted) error {
	return s.repo.UpsertSetWithPrints(ctx, set, prints)
}

// UpsertSetsWithPrints inserts or updates many sets and their prints in bulk.
// It is intended for full catalog crawls; re-running it is safe.
func (s *Service) UpsertSetsWithPrints(ctx context.Context, sets []Set) error {
	return s.repo.UpsertSetsWithPrints(ctx, sets)
}
