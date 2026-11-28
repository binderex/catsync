package ygo

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

// updateCompleteAllSnapshot merges the sets, prints and releases produced by a
// completed "new" scrape into the latest complete "all" provider snapshot.
//
// The merge is an upsert by identity: existing rows keep their IDs, their
// mutable fields are refreshed, new rows are appended and rows that do not
// appear in this scrape are preserved. The snapshot accumulates data like a
// backup of what has been inserted.
func (p *Provider) updateCompleteAllSnapshot(ctx context.Context, scrape *releaseScrape) error {
	if p.persistence == nil {
		return nil
	}
	if !scrape.isComplete() {
		log.Printf("ygo: new scrape is incomplete; skipping all snapshot merge")
		return nil
	}

	log.Printf("ygo: building new scrape snapshot")
	scraped := scrape.snapshot(true)
	if len(scraped.Sets) == 0 && len(scraped.Releases) == 0 {
		log.Printf("ygo: no scraped sets to merge into all snapshot")
		return nil
	}
	log.Printf("ygo: new scrape snapshot has %d sets", len(scraped.Sets))

	log.Printf("ygo: loading complete all snapshot for merge")
	run, base, found, err := p.loadLatestCompleteAllSnapshot(ctx)
	if err != nil {
		return err
	}
	if !found {
		log.Printf("ygo: no complete all snapshot to update; skipping snapshot merge")
		return nil
	}
	log.Printf("ygo: loaded all snapshot with %d sets", len(base.Sets))

	merged := mergeCheckpoints(base, scraped)
	log.Printf("ygo: merged snapshot has %d sets; saving", len(merged.Sets))

	data, err := encodeReleasesCheckpoint(merged)
	if err != nil {
		return err
	}

	run.ProviderData = data
	run.UpdatedAt = time.Now().UTC()
	if err := p.persistence.Save(ctx, run); err != nil {
		return fmt.Errorf("save updated all snapshot: %w", err)
	}

	log.Printf(
		"ygo: updated all snapshot with %d scraped sets (%d total sets)",
		len(scraped.Sets),
		len(merged.Sets),
	)
	return nil
}

// loadLatestCompleteAllSnapshot returns the newest "all" run that contains a
// complete provider snapshot.
func (p *Provider) loadLatestCompleteAllSnapshot(
	ctx context.Context,
) (domain.RetrierRun, releasesCheckpoint, bool, error) {
	summaries, err := p.persistence.List(ctx, domain.RetrierRunFilter{
		Provider: p.Name(),
		Mode:     "all",
	})
	if err != nil {
		return domain.RetrierRun{}, releasesCheckpoint{}, false,
			fmt.Errorf("list all runs: %w", err)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	for _, summary := range summaries {
		run, err := p.persistence.Load(ctx, summary.ID)
		if err != nil {
			log.Printf("ygo: load all run %s: %v", summary.ID, err)
			continue
		}
		if len(run.ProviderData) == 0 {
			continue
		}

		checkpoint, found, err := decodeReleasesCheckpoint(run.ProviderData)
		if err != nil {
			log.Printf("ygo: decode all run %s: %v", summary.ID, err)
			continue
		}
		if !found || !checkpoint.completeSnapshot() {
			continue
		}

		return run, checkpoint, true, nil
	}

	return domain.RetrierRun{}, releasesCheckpoint{}, false, nil
}

// mergeCheckpoints upserts scraped sets/prints/releases into base.
//
// Existing sets and prints keep their IDs so references stay consistent;
// mutable fields come from the scraped data. Rows absent from the scrape are
// kept because the snapshot is an accumulated backup, not a full replacement.
func mergeCheckpoints(base, scraped releasesCheckpoint) releasesCheckpoint {
	merged := base

	setIndex := make(map[string]int, len(merged.Sets))
	printIndex := make(map[int]map[string]int, len(merged.Sets))
	for index := range merged.Sets {
		setIndex[merged.Sets[index].Set.IdentityKey()] = index

		prints := make(map[string]int, len(merged.Sets[index].Prints))
		for position := range merged.Sets[index].Prints {
			prints[merged.Sets[index].Prints[position].IdentityKey()] = position
		}
		printIndex[index] = prints
	}

	for _, scrapedSet := range scraped.Sets {
		key := scrapedSet.Set.IdentityKey()

		if index, ok := setIndex[key]; ok {
			existing := &merged.Sets[index]
			setID := existing.Set.ID

			updatedSet := scrapedSet.Set
			updatedSet.ID = setID
			updatedSet.CreatedAt = existing.Set.CreatedAt
			existing.Set = updatedSet

			for _, scrapedPrint := range scrapedSet.Prints {
				updatedPrint := scrapedPrint
				printSetID := setID
				updatedPrint.SetID = &printSetID

				printKey := updatedPrint.IdentityKey()
				if position, ok := printIndex[index][printKey]; ok {
					updatedPrint.ID = existing.Prints[position].ID
					updatedPrint.CreatedAt = existing.Prints[position].CreatedAt
					existing.Prints[position] = updatedPrint
					continue
				}

				existing.Prints = append(existing.Prints, updatedPrint)
				printIndex[index][printKey] = len(existing.Prints) - 1
			}
			continue
		}

		merged.Sets = append(merged.Sets, scrapedSet)
		index := len(merged.Sets) - 1
		setIndex[key] = index

		prints := make(map[string]int, len(scrapedSet.Prints))
		for position := range scrapedSet.Prints {
			prints[scrapedSet.Prints[position].IdentityKey()] = position
		}
		printIndex[index] = prints
	}

	merged.Releases = mergeReleases(merged.Releases, scraped.Releases)
	merged.Complete = true
	return merged
}

func mergeReleases(base, scraped []Release) []Release {
	merged := make([]Release, len(base))
	copy(merged, base)

	index := make(map[string]int, len(merged))
	for position := range merged {
		index[canonicalPageURL(merged[position].URL)] = position
	}

	for _, release := range scraped {
		key := canonicalPageURL(release.URL)
		if position, ok := index[key]; ok {
			merged[position] = release
			continue
		}
		index[key] = len(merged)
		merged = append(merged, release)
	}

	return merged
}
