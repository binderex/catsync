package usecases

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
)

// CatalogUseCase persists denormalized catalog items. It normalizes the input,
// removes duplicate identities and drops non-standard rows before the
// repository upsert.
type CatalogUseCase struct {
	repo ports.CatalogRepository
}

// NewCatalogUseCase creates a catalog use case backed by repo.
func NewCatalogUseCase(repo ports.CatalogRepository) *CatalogUseCase {
	return &CatalogUseCase{repo: repo}
}

// Upsert removes duplicate items and writes the unique rows in one operation.
// Sets that had cards but none of them are regular cards are deleted. It
// returns the number of rows sent to the repository.
func (uc *CatalogUseCase) Upsert(ctx context.Context, items []domain.Item) (int, error) {
	if uc.repo == nil {
		return 0, fmt.Errorf("catalog repository is not configured")
	}
	if len(items) == 0 {
		return 0, nil
	}

	setsWithCards, setsWithStandardCards := setCardState(items)
	prunedSets := prunedSetIDs(setsWithCards, setsWithStandardCards)

	items = excludeUnknown(items)
	items = excludeNonStandardCards(items)
	items = excludeSets(items, prunedSets)
	items = excludeOrphanCards(items)

	count := 0
	if len(items) > 0 {
		normalized := domain.DeduplicateItems(prepareItems(items))
		for _, item := range normalized {
			if strings.TrimSpace(item.Source) == "" {
				return 0, fmt.Errorf("catalog item %s %q has an empty source", item.Kind, item.Code)
			}
		}

		if err := uc.repo.Upsert(ctx, normalized); err != nil {
			return 0, err
		}
		count = len(normalized)
	}

	if len(prunedSets) > 0 {
		if err := uc.repo.Delete(ctx, prunedSets); err != nil {
			return count, err
		}
	}

	return count, nil
}

// prepareItems fills the persistence defaults: a stable ID, a Properties map
// and the update timestamp. Identity and duplicate handling stay in domain.
func prepareItems(items []domain.Item) []domain.Item {
	now := time.Now().UTC()

	for index := range items {
		if items[index].ID == uuid.Nil {
			items[index].ID = uuid.New()
		}
		if items[index].Properties == nil {
			items[index].Properties = make(map[string]any)
		}
		if items[index].UpdatedAt.IsZero() {
			items[index].UpdatedAt = now
		}
	}

	return items
}

// setCardState reports which sets have at least one card and which have at
// least one regular card. Sets with no cards at all are not pruned because the
// card list may simply be missing from the crawl.
func setCardState(items []domain.Item) (map[uuid.UUID]bool, map[uuid.UUID]bool) {
	withCards := make(map[uuid.UUID]bool)
	withStandardCards := make(map[uuid.UUID]bool)

	for _, item := range items {
		if item.Kind != domain.KindCard || item.SetID == nil {
			continue
		}

		id := *item.SetID
		withCards[id] = true
		if !isUnknownName(item.Name) && isStandardCard(item) {
			withStandardCards[id] = true
		}
	}

	return withCards, withStandardCards
}

func prunedSetIDs(withCards, withStandardCards map[uuid.UUID]bool) []uuid.UUID {
	pruned := make([]uuid.UUID, 0)
	for id := range withCards {
		if !withStandardCards[id] {
			pruned = append(pruned, id)
		}
	}
	return pruned
}

func excludeSets(items []domain.Item, pruned []uuid.UUID) []domain.Item {
	if len(pruned) == 0 {
		return items
	}

	removed := make(map[uuid.UUID]bool, len(pruned))
	for _, id := range pruned {
		removed[id] = true
	}

	filtered := make([]domain.Item, 0, len(items))
	for _, item := range items {
		if item.Kind == domain.KindSet && removed[item.ID] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// excludeOrphanCards drops cards whose set is not present in the batch. The
// set may have been filtered out as an unsupported brand.
func excludeOrphanCards(items []domain.Item) []domain.Item {
	sets := make(map[uuid.UUID]bool)
	for _, item := range items {
		if item.Kind == domain.KindSet {
			sets[item.ID] = true
		}
	}

	filtered := make([]domain.Item, 0, len(items))
	for _, item := range items {
		if item.Kind == domain.KindCard && item.SetID != nil && !sets[*item.SetID] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// excludeUnknown drops rows whose name is the "unknown" placeholder. They are
// not real catalog entries and must not be added to the catalog.
func excludeUnknown(items []domain.Item) []domain.Item {
	filtered := make([]domain.Item, 0, len(items))
	for _, item := range items {
		if isUnknownName(item.Name) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func isUnknownName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "unknown")
}

// excludeNonStandardCards keeps only regular Yu-Gi-Oh! cards: monsters, spells
// and traps. Tokens, strategy/tip/checklist cards, skills and any other card
// type are not part of the catalog.
func excludeNonStandardCards(items []domain.Item) []domain.Item {
	filtered := make([]domain.Item, 0, len(items))
	for _, item := range items {
		if !isStandardCard(item) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func isStandardCard(item domain.Item) bool {
	if item.Kind != domain.KindCard {
		return true
	}

	types := strings.ToLower(strings.TrimSpace(item.PropertyString(domain.PropertyCardTypes)))
	if types == "" {
		return false
	}

	for _, excluded := range []string{"token", "strategy", "skill", "tip", "checklist", "non-game"} {
		if strings.Contains(types, excluded) {
			return false
		}
	}

	return strings.Contains(types, "monster") ||
		strings.Contains(types, "spell") ||
		strings.Contains(types, "trap")
}
