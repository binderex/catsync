package ygo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/application/usecases"
	"github.com/binderex/catsync/internal/domain"
	jsonstore "github.com/binderex/catsync/internal/infrastructure/persistence/json"
)

func TestMergeCheckpointsUpsertsBySetIdentity(t *testing.T) {
	baseSet := testSetItem("LOB", "Legend of Blue Eyes", "old set")
	basePrint := testPrintItem("LOB-001", &baseSet.ID, nil)
	basePrint.Name = "Old print"
	basePrint.Description = "old description"

	absentPrint := testPrintItem("LOB-002", &baseSet.ID, nil)
	absentPrint.Name = "Kept print"

	unrelatedSet := testSetItem("MRD", "Metal Raiders", "")
	unrelatedPrint := testPrintItem("MRD-001", &unrelatedSet.ID, nil)

	baseURL := baseSet.Source
	unrelatedURL := unrelatedSet.Source
	newURL := "https://yugipedia.com/wiki/New_Set"

	base := releasesCheckpoint{
		Version:  releasesStateVersion,
		Complete: true,
		Releases: []Release{
			{
				Name:      "Legend of Blue Eyes",
				URL:       baseURL,
				ReleaseAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
				Lang:      domain.LangEN,
				Brand:     domain.BrandYGO,
			},
			{
				Name:      "Metal Raiders",
				URL:       unrelatedURL,
				ReleaseAt: time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC),
				Lang:      domain.LangEN,
				Brand:     domain.BrandYGO,
			},
		},
		Sets: []savedSet{
			{Set: *baseSet, Prints: []domain.Item{*basePrint, *absentPrint}},
			{Set: *unrelatedSet, Prints: []domain.Item{*unrelatedPrint}},
		},
	}

	scrapedSet := *baseSet
	scrapedSet.ID = uuid.NewString()
	scrapedSet.Name = "Updated set"
	scrapedSet.Description = "updated description"

	scrapedPrint := *basePrint
	scrapedPrint.ID = uuid.NewString()
	scrapedPrint.SetID = &scrapedSet.ID
	scrapedPrint.Name = "Updated print"
	scrapedPrint.Description = "updated description"

	newPrint := *testPrintItem("LOB-003", &scrapedSet.ID, nil)
	newPrint.Name = "New print"

	jpSet := *baseSet
	jpSet.ID = uuid.NewString()
	jpSet.Lang = domain.LangJP
	jpSet.Code = "LOB-JP"
	jpSet.Name = "Legend of Blue Eyes JP"

	jpPrint := *testPrintItem("LOB-JP001", &jpSet.ID, nil)
	jpPrint.Lang = domain.LangJP

	scraped := releasesCheckpoint{
		Version:  releasesStateVersion,
		Complete: true,
		Releases: []Release{
			{
				Name:      "Legend of Blue Eyes (updated)",
				URL:       baseURL,
				ReleaseAt: time.Date(2020, time.February, 2, 0, 0, 0, 0, time.UTC),
				Lang:      domain.LangEN,
				Brand:     domain.BrandYGO,
			},
			{
				Name:      "New Set",
				URL:       newURL,
				ReleaseAt: time.Date(2021, time.March, 3, 0, 0, 0, 0, time.UTC),
				Lang:      domain.LangEN,
				Brand:     domain.BrandYGO,
			},
		},
		Sets: []savedSet{
			{Set: scrapedSet, Prints: []domain.Item{scrapedPrint, newPrint}},
			{Set: jpSet, Prints: []domain.Item{jpPrint}},
		},
	}

	merged := mergeCheckpoints(base, scraped)

	if !merged.Complete {
		t.Error("merged snapshot should be complete")
	}
	if len(merged.Sets) != 3 {
		t.Fatalf("sets = %d, want 3", len(merged.Sets))
	}

	en := findSavedSet(t, merged.Sets, baseSet.IdentityKey())
	if en.Set.ID != baseSet.ID {
		t.Errorf("existing set id = %q, want %q", en.Set.ID, baseSet.ID)
	}
	if en.Set.Name != "Updated set" || en.Set.Description != "updated description" {
		t.Errorf("existing set was not updated: %+v", en.Set)
	}
	if len(en.Prints) != 3 {
		t.Fatalf("existing set prints = %d, want 3", len(en.Prints))
	}

	updatedPrint := findPrintByCode(t, en.Prints, "LOB-001")
	if updatedPrint.ID != basePrint.ID {
		t.Errorf("updated print id = %q, want %q", updatedPrint.ID, basePrint.ID)
	}
	if updatedPrint.Name != "Updated print" || updatedPrint.Description != "updated description" {
		t.Errorf("updated print was not refreshed: %+v", updatedPrint)
	}
	if updatedPrint.SetID == nil || *updatedPrint.SetID != baseSet.ID {
		t.Errorf("updated print set id = %v, want %q", updatedPrint.SetID, baseSet.ID)
	}

	keptPrint := findPrintByCode(t, en.Prints, "LOB-002")
	if keptPrint.ID != absentPrint.ID || keptPrint.Name != "Kept print" {
		t.Errorf("absent print was not preserved: %+v", keptPrint)
	}

	addedPrint := findPrintByCode(t, en.Prints, "LOB-003")
	if addedPrint.SetID == nil || *addedPrint.SetID != baseSet.ID {
		t.Errorf("new print set id = %v, want %q", addedPrint.SetID, baseSet.ID)
	}

	jp := findSavedSet(t, merged.Sets, jpSet.IdentityKey())
	if jp.Set.ID != jpSet.ID {
		t.Errorf("new language set id = %q, want %q", jp.Set.ID, jpSet.ID)
	}
	if len(jp.Prints) != 1 || jp.Prints[0].Code != "LOB-JP001" {
		t.Errorf("new language prints = %+v", jp.Prints)
	}

	unrelated := findSavedSet(t, merged.Sets, unrelatedSet.IdentityKey())
	if len(unrelated.Prints) != 1 || unrelated.Prints[0].Code != "MRD-001" {
		t.Errorf("unrelated set was not preserved: %+v", unrelated)
	}

	if len(merged.Releases) != 3 {
		t.Fatalf("releases = %d, want 3", len(merged.Releases))
	}
	if release := findRelease(t, merged.Releases, baseURL); release.Name != "Legend of Blue Eyes (updated)" {
		t.Errorf("existing release was not updated: %+v", release)
	}
	if release := findRelease(t, merged.Releases, unrelatedURL); release.Name != "Metal Raiders" {
		t.Errorf("unrelated release was not preserved: %+v", release)
	}
	if release := findRelease(t, merged.Releases, newURL); release.Name != "New Set" {
		t.Errorf("new release was not added: %+v", release)
	}
}

func TestUpdateCompleteAllSnapshotMergesIntoPersistedAllRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo := jsonstore.NewFile(filepath.Join(dir, "retrier.json"))
	persistence := usecases.NewRetrierPersistenceUseCase(repo)

	baseSet := testSetItem("LOB", "Legend of Blue Eyes", "old set")
	basePrint := testPrintItem("LOB-001", &baseSet.ID, nil)

	base := releasesCheckpoint{
		Version:  releasesStateVersion,
		Complete: true,
		Releases: []Release{{
			Name:      "Legend of Blue Eyes",
			URL:       baseSet.Source,
			ReleaseAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			Lang:      domain.LangEN,
			Brand:     domain.BrandYGO,
		}},
		Sets: []savedSet{
			{Set: *baseSet, Prints: []domain.Item{*basePrint}},
		},
	}

	data, err := encodeReleasesCheckpoint(base)
	if err != nil {
		t.Fatalf("encode base checkpoint: %v", err)
	}

	run := domain.RetrierRun{
		Version:      2,
		ID:           domain.RetrierRunID("all-1"),
		Provider:     "ygo",
		Mode:         "all",
		State:        domain.RetrierRunInterrupted,
		StartedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		ProviderData: data,
	}
	if err := persistence.Save(ctx, run); err != nil {
		t.Fatalf("save base run: %v", err)
	}

	provider := NewYGOCatalogProvider(persistence)

	scrapedSet := *baseSet
	scrapedSet.ID = uuid.NewString()
	scrapedSet.Name = "Updated set"

	newPrint := *testPrintItem("LOB-002", &scrapedSet.ID, nil)
	scrape := newEmptyReleaseScrape(provider, []Release{{
		Name:      "Legend of Blue Eyes",
		URL:       baseSet.Source,
		ReleaseAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}})
	scrape.states = []*setState{{
		Set:    &scrapedSet,
		Prints: []*domain.Item{&newPrint},
	}}
	scrape.processed[canonicalPageURL(baseSet.Source)] = true

	if err := provider.updateCompleteAllSnapshot(ctx, scrape); err != nil {
		t.Fatalf("update all snapshot: %v", err)
	}

	loaded, err := persistence.Load(ctx, run.ID)
	if err != nil {
		t.Fatalf("load updated run: %v", err)
	}

	merged, found, err := decodeReleasesCheckpoint(loaded.ProviderData)
	if err != nil {
		t.Fatalf("decode updated run: %v", err)
	}
	if !found || !merged.Complete {
		t.Fatalf("updated run checkpoint found=%v complete=%v", found, merged.Complete)
	}
	if len(merged.Sets) != 1 {
		t.Fatalf("sets = %d, want 1", len(merged.Sets))
	}
	if merged.Sets[0].Set.ID != baseSet.ID {
		t.Errorf("set id = %q, want %q", merged.Sets[0].Set.ID, baseSet.ID)
	}
	if len(merged.Sets[0].Prints) != 2 {
		t.Fatalf("prints = %d, want 2", len(merged.Sets[0].Prints))
	}
}

func TestGetAllReleasesUsesCompleteAllSnapshotWithoutCrawl(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo := jsonstore.NewFile(filepath.Join(dir, "retrier.json"))
	persistence := usecases.NewRetrierPersistenceUseCase(repo)

	set := testSetItem("LOB", "Legend of Blue Eyes", "")
	print := testPrintItem("LOB-001", &set.ID, nil)

	checkpoint := releasesCheckpoint{
		Version:  releasesStateVersion,
		Complete: true,
		Releases: []Release{{
			Name:  "Legend of Blue Eyes",
			URL:   set.Source,
			Lang:  domain.LangEN,
			Brand: domain.BrandYGO,
		}},
		Sets: []savedSet{
			{Set: *set, Prints: []domain.Item{*print}},
		},
	}
	data, err := encodeReleasesCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}

	run := domain.RetrierRun{
		Version:      2,
		ID:           domain.RetrierRunID("all-complete"),
		Provider:     "ygo",
		Mode:         "all",
		State:        domain.RetrierRunSuccess,
		StartedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		ProviderData: data,
	}
	if err := persistence.Save(ctx, run); err != nil {
		t.Fatalf("save complete run: %v", err)
	}

	provider := NewYGOCatalogProvider(persistence)
	items, err := provider.GetAllReleases(ctx, domain.ProviderOptions{Resume: true})
	if err != nil {
		t.Fatalf("GetAllReleases: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Kind != domain.KindSet || items[1].Kind != domain.KindCard {
		t.Errorf("items = %+v", items)
	}
}

func findSavedSet(t *testing.T, sets []savedSet, key string) savedSet {
	t.Helper()
	for _, set := range sets {
		if set.Set.IdentityKey() == key {
			return set
		}
	}
	t.Fatalf("set %q not found", key)
	return savedSet{}
}

func findPrintByCode(t *testing.T, prints []domain.Item, code string) domain.Item {
	t.Helper()
	for _, print := range prints {
		if print.Code == code {
			return print
		}
	}
	t.Fatalf("print %q not found", code)
	return domain.Item{}
}

func findRelease(t *testing.T, releases []Release, rawURL string) Release {
	t.Helper()
	key := canonicalPageURL(rawURL)
	for _, release := range releases {
		if canonicalPageURL(release.URL) == key {
			return release
		}
	}
	t.Fatalf("release %q not found", rawURL)
	return Release{}
}
