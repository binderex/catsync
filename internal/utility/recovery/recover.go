// Package recovery rebuilds the YGO provider snapshot from the catalog stored
// in PostgreSQL. It is a maintenance utility, not part of the normal sync.
package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/binderex/catsync/internal/domain"
	jsonstore "github.com/binderex/catsync/internal/infrastructure/persistence/json"
)

const snapshotVersion = 2

// ItemSource loads catalog items for the snapshot.
type ItemSource interface {
	Load(ctx context.Context, kinds ...domain.Kind) ([]domain.Item, error)
}

// Stats summarizes a recovered snapshot.
type Stats struct {
	Items   int
	Sets    int
	Cards   int
	Orphans int
	Bytes   int
	Path    string
}

type savedSet struct {
	Set    domain.Item   `json:"set"`
	Prints []domain.Item `json:"prints"`
}

type checkpoint struct {
	Version  int        `json:"version"`
	SavedAt  time.Time  `json:"saved_at"`
	Complete bool       `json:"complete"`
	Sets     []savedSet `json:"sets"`
}

// BuildSnapshot groups catalog items into a complete YGO checkpoint. Cards
// whose set is missing are counted as orphans and left out.
func BuildSnapshot(items []domain.Item) (json.RawMessage, Stats, error) {
	sets := make(map[string]*savedSet)
	order := make([]string, 0)

	for _, item := range items {
		if item.Kind != domain.KindSet {
			continue
		}
		sets[item.ID] = &savedSet{Set: item}
		order = append(order, item.ID)
	}

	orphans := 0
	for _, item := range items {
		if item.Kind != domain.KindCard {
			continue
		}
		if item.SetID == nil {
			orphans++
			continue
		}
		set, ok := sets[*item.SetID]
		if !ok {
			orphans++
			continue
		}
		set.Prints = append(set.Prints, item)
	}

	snapshot := checkpoint{
		Version:  snapshotVersion,
		SavedAt:  time.Now().UTC(),
		Complete: true,
		Sets:     make([]savedSet, 0, len(order)),
	}
	for _, id := range order {
		snapshot.Sets = append(snapshot.Sets, *sets[id])
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("encode ygo snapshot: %w", err)
	}

	return data, Stats{
		Items:   len(items),
		Sets:    len(order),
		Cards:   len(items) - len(order),
		Orphans: orphans,
		Bytes:   len(data),
	}, nil
}

// Run loads the YGO catalog from source and writes the snapshot into path.
func Run(ctx context.Context, source ItemSource, path string) (Stats, error) {
	items, err := source.Load(ctx, domain.KindSet, domain.KindCard)
	if err != nil {
		return Stats{}, fmt.Errorf("load catalog items: %w", err)
	}

	data, stats, err := BuildSnapshot(items)
	if err != nil {
		return Stats{}, err
	}

	now := time.Now().UTC()
	run := domain.RetrierRun{
		Version:   2,
		ID:        domain.RetrierRunID("ygo-all-recovered-" + now.Format("20060102T150405Z")),
		Provider:  "ygo",
		Mode:      "all",
		State:     domain.RetrierRunInterrupted,
		StartedAt: now,
		UpdatedAt: now,
		Progress: domain.Progress{
			Total:     stats.Items,
			Done:      stats.Items,
			Completed: stats.Items,
		},
		ProviderData: data,
	}

	repo := jsonstore.NewFile(path)
	if err := repo.Replace(ctx, []domain.RetrierRun{run}); err != nil {
		return Stats{}, fmt.Errorf("save retrier document: %w", err)
	}

	stats.Path = repo.Path()

	return stats, nil
}
