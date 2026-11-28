package ygo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

type recordingPersistence struct {
	providerCalls int
	mode          string
	data          json.RawMessage
}

func (p *recordingPersistence) RunStarted(context.Context, domain.RetrierRun) (domain.RetrierRunID, error) {
	return "", nil
}

func (p *recordingPersistence) RunStateChanged(context.Context, domain.RetrierRunID, domain.URLState) error {
	return nil
}

func (p *recordingPersistence) RunFinished(context.Context, domain.RetrierRun) error {
	return nil
}

func (p *recordingPersistence) Save(context.Context, domain.RetrierRun) error {
	return nil
}

func (p *recordingPersistence) Load(context.Context, domain.RetrierRunID) (domain.RetrierRun, error) {
	return domain.RetrierRun{}, domain.ErrRunNotFound
}

func (p *recordingPersistence) LoadLatest(context.Context, string, string) (domain.RetrierRun, bool, error) {
	return domain.RetrierRun{}, false, nil
}

func (p *recordingPersistence) List(context.Context, domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error) {
	return nil, nil
}

func (p *recordingPersistence) Delete(context.Context, domain.RetrierRunID) error {
	return nil
}

func (p *recordingPersistence) SaveProviderData(
	_ context.Context,
	_ string,
	mode string,
	data json.RawMessage,
) error {
	p.providerCalls++
	p.mode = mode
	p.data = data
	return nil
}

func TestSaveScrapeCheckpointStoresCompleteSnapshot(t *testing.T) {
	persistence := &recordingPersistence{}
	provider := NewYGOCatalogProvider(persistence)
	release := Release{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}
	scrape := newEmptyReleaseScrape(provider, []Release{release})

	set := testSetItem("TST", "Test Set", "")
	print := testPrintItem("TST-EN001", &set.ID, nil)
	state := &setState{Set: set, Prints: []*domain.Item{print}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[print] = state
	scrape.processed[canonicalPageURL(release.URL)] = true

	provider.saveScrapeCheckpoint(context.Background(), "new", scrape, true)

	if persistence.providerCalls != 1 || persistence.mode != "new" {
		t.Fatalf("provider calls = %d mode = %q", persistence.providerCalls, persistence.mode)
	}
	checkpoint, found, err := decodeReleasesCheckpoint(persistence.data)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if !found || !checkpoint.completeSnapshot() || len(checkpoint.Sets) != 1 {
		t.Fatalf("snapshot = %+v", checkpoint)
	}
}

func TestSaveScrapeCheckpointSkipsEmptyRun(t *testing.T) {
	persistence := &recordingPersistence{}
	provider := NewYGOCatalogProvider(persistence)
	scrape := newEmptyReleaseScrape(provider, nil)

	provider.saveScrapeCheckpoint(context.Background(), "new", scrape, true)

	if persistence.providerCalls != 0 {
		t.Fatalf("empty run created a provider snapshot")
	}
}

func TestSnapshotSeparatesPendingPrints(t *testing.T) {
	provider := NewYGOCatalogProvider()
	release := Release{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}
	scrape := newEmptyReleaseScrape(provider, []Release{release})

	set := testSetItem("TST", "Test Set", "")
	completePrint := testPrintItem("TST-EN001", &set.ID, nil)
	pendingPrint := testPrintItem("TST-EN002", &set.ID, nil)
	state := &setState{Set: set, Prints: []*domain.Item{completePrint, pendingPrint}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[completePrint] = state
	scrape.stateByPrint[pendingPrint] = state
	scrape.pending[pendingPrint] = true
	scrape.printsByCard["https://yugipedia.com/wiki/Pending_Card"] = []*domain.Item{pendingPrint}
	scrape.processed[canonicalPageURL(release.URL)] = true

	checkpoint := scrape.snapshot(false)
	if checkpoint.Complete {
		t.Error("checkpoint with pending prints must not be complete")
	}
	if len(checkpoint.Sets) != 1 || len(checkpoint.Sets[0].Prints) != 1 {
		t.Fatalf("complete snapshot sets = %+v", checkpoint.Sets)
	}
	if checkpoint.Sets[0].Prints[0].Code != "TST-EN001" {
		t.Fatalf("complete print = %+v", checkpoint.Sets[0].Prints[0])
	}
	if len(checkpoint.PendingPrints) != 1 || checkpoint.PendingPrints[0].Print.Code != "TST-EN002" {
		t.Fatalf("pending prints = %+v", checkpoint.PendingPrints)
	}
	if refs := checkpoint.PrintsByCard["https://yugipedia.com/wiki/Pending_Card"]; len(refs) != 1 || refs[0] != 0 {
		t.Fatalf("pending refs = %+v", refs)
	}

	restored, err := newReleaseScrapeFromCheckpoint(provider, checkpoint)
	if err != nil {
		t.Fatalf("restore checkpoint: %v", err)
	}
	if len(restored.states) != 1 || len(restored.states[0].Prints) != 2 {
		t.Fatalf("restored prints = %+v", restored.states)
	}
	if !restored.pending[restored.states[0].Prints[1]] {
		t.Error("pending print was not marked on restore")
	}
	if restored.isComplete() {
		t.Error("restored scrape with pending prints must not be complete")
	}
	items := restored.items()
	if len(items) != 2 || items[0].Kind != domain.KindSet || items[1].Code != "TST-EN001" {
		t.Fatalf("materialized items = %+v", items)
	}
}

func TestCachedResultsKeepGallerylessPrintPending(t *testing.T) {
	provider := NewYGOCatalogProvider()
	release := Release{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}
	scrape := newEmptyReleaseScrape(provider, []Release{release})

	set := testSetItem("TST", "Test Set", "")
	withGallery := testPrintItem("TST-EN001", &set.ID, nil)
	withPendingGallery := testPrintItem("TST-EN002", &set.ID, nil)
	state := &setState{Set: set, Prints: []*domain.Item{withGallery, withPendingGallery}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[withGallery] = state
	scrape.stateByPrint[withPendingGallery] = state
	scrape.processed[canonicalPageURL(release.URL)] = true

	withCardURL := "https://yugipedia.com/wiki/Card_With_Gallery"
	pendingCardURL := "https://yugipedia.com/wiki/Card_With_Pending_Gallery"
	galleryURL := "https://yugipedia.com/wiki/Card_Gallery:Card_With_Gallery"
	pendingGalleryURL := "https://yugipedia.com/wiki/Card_Gallery:Card_With_Pending_Gallery"
	galleryKey := canonicalPageURL(galleryURL)

	scrape.printsByCard[withCardURL] = []*domain.Item{withGallery}
	scrape.printsByCard[pendingCardURL] = []*domain.Item{withPendingGallery}
	scrape.markPendingLocked(withGallery, withPendingGallery)
	scrape.cardResults[withCardURL] = cardScrapeResult{
		Info:       newCardItem(domain.BrandYGO, nil, withCardURL, "", "Monster", nil),
		GalleryURL: galleryURL,
	}
	scrape.cardResults[pendingCardURL] = cardScrapeResult{
		Info:       newCardItem(domain.BrandYGO, nil, pendingCardURL, "", "Monster", nil),
		GalleryURL: pendingGalleryURL,
	}
	scrape.galleryResults[galleryKey] = []gallerySection{{
		Name:  "Worldwide English",
		Brand: domain.BrandYGO,
		Lang:  domain.LangEN,
		Prints: []galleryPrint{{
			Code:   "TST-EN001",
			Rarity: "Common",
			Image:  domain.Image{URLSmall: "small", URLLarge: "large"},
		}},
	}}

	scrape.reconcileCachedResultsLocked()

	if scrape.pending[withGallery] {
		t.Error("print with a parsed gallery is still pending")
	}
	if !scrape.pending[withPendingGallery] {
		t.Error("print with an unparsed gallery is not pending")
	}
	if _, ok := scrape.galleryRefs[galleryKey]; ok {
		t.Error("parsed gallery is still queued")
	}
	if refs, ok := scrape.galleryRefs[canonicalPageURL(pendingGalleryURL)]; !ok || len(refs) != 1 {
		t.Errorf("pending gallery was not queued: %+v", refs)
	}

	checkpoint := scrape.snapshot(false)
	if len(checkpoint.Sets) != 1 || len(checkpoint.Sets[0].Prints) != 1 {
		t.Fatalf("complete prints = %+v", checkpoint.Sets)
	}
	if len(checkpoint.PendingPrints) != 1 || checkpoint.PendingPrints[0].Print.Code != "TST-EN002" {
		t.Fatalf("pending prints = %+v", checkpoint.PendingPrints)
	}
}

func TestLegacyCheckpointMovesPendingPrintsOutOfSets(t *testing.T) {
	set := testSetItem("LEG", "Legacy Set", "")
	completePrint := testPrintItem("LEG-EN001", &set.ID, nil)
	pendingPrint := testPrintItem("LEG-EN002", &set.ID, nil)

	legacy := legacyReleasesCheckpoint{
		Version:  1,
		Complete: true,
		Sets: []savedSet{{
			Set:    *set,
			Prints: []domain.Item{*completePrint, *pendingPrint},
		}},
		PrintsByCard: map[string][]savedPrintRef{
			"https://yugipedia.com/wiki/Pending_Card": {{Set: 0, Print: 1}},
		},
	}

	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy checkpoint: %v", err)
	}

	checkpoint, found, err := decodeReleasesCheckpoint(data)
	if err != nil {
		t.Fatalf("decode legacy checkpoint: %v", err)
	}
	if !found {
		t.Fatal("legacy checkpoint was not found")
	}
	if checkpoint.Version != releasesStateVersion {
		t.Errorf("version = %d, want %d", checkpoint.Version, releasesStateVersion)
	}
	if checkpoint.Complete {
		t.Error("legacy checkpoint with pending refs must not be complete")
	}
	if len(checkpoint.Sets) != 1 || len(checkpoint.Sets[0].Prints) != 1 {
		t.Fatalf("complete prints = %+v", checkpoint.Sets)
	}
	if checkpoint.Sets[0].Prints[0].Code != "LEG-EN001" {
		t.Errorf("complete print = %+v", checkpoint.Sets[0].Prints[0])
	}
	if len(checkpoint.PendingPrints) != 1 || checkpoint.PendingPrints[0].Print.Code != "LEG-EN002" {
		t.Fatalf("pending prints = %+v", checkpoint.PendingPrints)
	}
	refs := checkpoint.PrintsByCard["https://yugipedia.com/wiki/Pending_Card"]
	if len(refs) != 1 || refs[0] != 0 {
		t.Fatalf("pending refs = %+v", refs)
	}
}

func TestLegacyCompleteSnapshotStaysComplete(t *testing.T) {
	set := testSetItem("LEG", "Legacy Set", "")
	print := testPrintItem("LEG-EN001", &set.ID, nil)

	legacy := legacyReleasesCheckpoint{
		Version:  1,
		Complete: true,
		Sets: []savedSet{{
			Set:    *set,
			Prints: []domain.Item{*print},
		}},
	}

	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy checkpoint: %v", err)
	}
	checkpoint, found, err := decodeReleasesCheckpoint(data)
	if err != nil {
		t.Fatalf("decode legacy checkpoint: %v", err)
	}
	if !found || !checkpoint.completeSnapshot() {
		t.Fatalf("checkpoint found=%v complete=%v pending=%d",
			found, checkpoint.Complete, len(checkpoint.PendingPrints))
	}
	if len(checkpoint.Sets) != 1 || len(checkpoint.Sets[0].Prints) != 1 {
		t.Fatalf("sets = %+v", checkpoint.Sets)
	}
}

func TestNewReleasesScrapeResumesSavedSnapshot(t *testing.T) {
	provider := NewYGOCatalogProvider()
	set := testSetItem("NEW", "Saved Set", "")
	print := testPrintItem("NEW-EN001", &set.ID, nil)
	release := Release{
		Name:      "Saved Set",
		URL:       set.Source,
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}

	scrape := newEmptyReleaseScrape(provider, []Release{release})
	state := &setState{Set: set, Prints: []*domain.Item{print}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[print] = state
	scrape.processed[canonicalPageURL(release.URL)] = true

	data, err := encodeReleasesCheckpoint(scrape.snapshot(true))
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}

	newRelease := Release{
		Name:      "New Set",
		URL:       "https://yugipedia.com/wiki/New_Set",
		ReleaseAt: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}

	restored, err := provider.newReleasesScrape([]Release{newRelease}, domain.ProviderOptions{
		Resume: true,
		ResumeState: &domain.RetrierRun{
			ID:           "run-1",
			ProviderData: data,
		},
	})
	if err != nil {
		t.Fatalf("new releases scrape: %v", err)
	}
	if !restored.resumable {
		t.Error("restored scrape is not resumable")
	}
	if len(restored.releases) != 2 {
		t.Fatalf("releases = %+v", restored.releases)
	}
	if len(restored.states) != 1 || len(restored.states[0].Prints) != 1 {
		t.Fatalf("restored states = %+v", restored.states)
	}
}

func TestResumeStatesRetriesPagesWithoutParsedData(t *testing.T) {
	provider := NewYGOCatalogProvider()
	scrape := newEmptyReleaseScrape(provider, nil)

	failedURL := "https://yugipedia.com/wiki/Failed_Card"
	unparsedURL := "https://yugipedia.com/wiki/Unparsed_Card"
	parsedURL := "https://yugipedia.com/wiki/Parsed_Card"
	missingURL := "https://yugipedia.com/wiki/Missing_Card"

	scrape.processed[canonicalPageURL(parsedURL)] = true

	states := scrape.resumeStates(&domain.RetrierRun{
		States: map[string]domain.URLState{
			failedURL: {
				URL:      failedURL,
				Visited:  true,
				Failed:   true,
				Attempts: 3,
				Error:    "timeout",
			},
			unparsedURL: {
				URL:       unparsedURL,
				Visited:   true,
				Completed: true,
			},
			parsedURL: {
				URL:       parsedURL,
				Visited:   true,
				Completed: true,
			},
			missingURL: {
				URL:      missingURL,
				NotFound: true,
			},
		},
	})

	if state := states[failedURL]; state.Terminal() || state.Attempts != 0 {
		t.Errorf("failed page was not reset: %+v", state)
	}
	if state := states[unparsedURL]; state.Terminal() {
		t.Errorf("completed page without parsed data was not reset: %+v", state)
	}
	if state := states[parsedURL]; !state.Completed {
		t.Errorf("parsed completed page was reset: %+v", state)
	}
	if state := states[missingURL]; !state.NotFound {
		t.Errorf("not-found page was reset: %+v", state)
	}
}

func TestResolveTerminalPagesCompletesMissingPages(t *testing.T) {
	provider := NewYGOCatalogProvider()
	release := Release{
		Name:      "Test Set",
		URL:       "https://yugipedia.com/wiki/Test_Set",
		ReleaseAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Lang:      domain.LangEN,
		Brand:     domain.BrandYGO,
	}
	scrape := newEmptyReleaseScrape(provider, []Release{release})

	set := testSetItem("TST", "Test Set", "")
	print := testPrintItem("TST-EN001", &set.ID, nil)
	state := &setState{Set: set, Prints: []*domain.Item{print}}
	scrape.states = []*setState{state}
	scrape.stateByPrint[print] = state
	scrape.pending[print] = true
	scrape.printsByCard["https://yugipedia.com/wiki/Missing_Card"] = []*domain.Item{print}
	scrape.processed[canonicalPageURL(release.URL)] = true
	scrape.pages = map[string]domain.URLState{
		"https://yugipedia.com/wiki/Missing_Card": {
			URL:      "https://yugipedia.com/wiki/Missing_Card",
			NotFound: true,
		},
	}

	scrape.resolveTerminalPagesLocked()

	if len(scrape.pending) != 0 || len(scrape.printsByCard) != 0 {
		t.Fatalf("missing page did not complete the print: pending=%d refs=%d",
			len(scrape.pending), len(scrape.printsByCard))
	}
	if !scrape.isComplete() {
		t.Fatal("scrape should be complete after resolving a missing card page")
	}
}
