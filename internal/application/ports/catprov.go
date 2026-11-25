package ports

import (
	"context"

	"github.com/binderex/catsync/internal/domain"
)

// CatalogProvider fetches catalog items from an external provider. Dragon
// Shield and Yu-Gi-Oh! implement it with the same two operations.
type CatalogProvider interface {
	// Name identifies the provider in retrier persistence.
	Name() string
	GetNewReleases(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error)
	GetAllReleases(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error)
}
