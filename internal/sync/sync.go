package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/catprov"
)

// CatalogWriter persists catalog items.
type CatalogWriter interface {
	Upsert(ctx context.Context, items []catalog.CatalogItem) error
}

// Synchronizer coordinates a catalog provider and the catalog service.
type Synchronizer struct {
	provider catprov.CatalogProvider
	catalog  CatalogWriter
}

// New creates a catalog synchronizer.
func New(provider catprov.CatalogProvider, catalog CatalogWriter) *Synchronizer {
	return &Synchronizer{
		provider: provider,
		catalog:  catalog,
	}
}

// SyncCatalog fetches every card, print and set matching name and upserts the
// result into the catalog.
func (s *Synchronizer) SyncCatalog(ctx context.Context, name string) error {
	max := time.Now().Add(24 * time.Hour)

	cards, err := s.provider.GetCards(name, max)
	if err != nil {
		return fmt.Errorf("get cards %q: %w", name, err)
	}

	var prints []catalog.CatalogItem
	for _, card := range cards {
		cardPrints, err := s.provider.GetCardPrints(card)
		if err != nil {
			// Some cards (e.g. Ojamandala) do not have a prints gallery.
			if errors.Is(err, catprov.ErrCardPrintsNotFound) {
				continue
			}
			return fmt.Errorf("get card prints for %q: %w", card.Name, err)
		}
		prints = append(prints, cardPrints...)
	}

	if len(prints) == 0 {
		return fmt.Errorf("sync catalog %q: %w", name, catprov.ErrCardPrintsNotFound)
	}

	setItems, err := s.provider.GetSets(prints)
	if err != nil && !errors.Is(err, catprov.ErrSetNotFound) {
		return fmt.Errorf("get sets for %q: %w", name, err)
	}

	items := make([]catalog.CatalogItem, 0, len(prints)+len(setItems))
	items = append(items, prints...)
	items = append(items, setItems...)

	if err := s.catalog.Upsert(ctx, items); err != nil {
		return fmt.Errorf("upsert catalog %q: %w", name, err)
	}

	return nil
}
