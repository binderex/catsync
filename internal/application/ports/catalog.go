package ports

import (
	"context"

	"github.com/binderex/catsync/internal/domain"
)

// CatalogRepository persists catalog items. The catalog use case only needs to
// upsert the denormalized item rows and remove whole sets that no longer have
// regular cards.
type CatalogRepository interface {
	Upsert(ctx context.Context, items []domain.Item) error
	Delete(ctx context.Context, ids []string) error
}
