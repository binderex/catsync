package ygo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

const releasesStateVersion = 2

// releasesCheckpoint is the resumable state of GetAllReleases. Sets keep the
// complete extracted catalog data and the page maps keep enough information to
// rebuild the pending queue without repeating completed requests.
//
// Sets contains the sets whose page was parsed and only their complete prints.
// Prints whose card page or gallery is still pending live in PendingPrints:
// they are resume data, not part of the scraped snapshot.
type releasesCheckpoint struct {
	Version  int       `json:"version"`
	SavedAt  time.Time `json:"saved_at"`
	Complete bool      `json:"complete"`

	Releases []Release  `json:"releases"`
	Sets     []savedSet `json:"sets"`

	PendingPrints []savedPendingPrint `json:"pending_prints,omitempty"`

	ListPages map[string]savedListPage `json:"list_pages,omitempty"`

	// PrintsByCard and GalleryRefs index PendingPrints.
	PrintsByCard map[string][]int `json:"prints_by_card,omitempty"`
	GalleryRefs  map[string][]int `json:"gallery_refs,omitempty"`

	CardResults    map[string]savedCardResult  `json:"card_results,omitempty"`
	GalleryResults map[string][]gallerySection `json:"gallery_results,omitempty"`

	Processed map[string]bool            `json:"processed,omitempty"`
	Pages     map[string]domain.URLState `json:"pages,omitempty"`
	Failures  []pageFailure              `json:"failures,omitempty"`
}

type savedSet struct {
	Set    domain.Item   `json:"set"`
	Prints []domain.Item `json:"prints"`
}

// savedPendingPrint is one print that is not complete yet. Set is an index into
// releasesCheckpoint.Sets.
type savedPendingPrint struct {
	Set   int         `json:"set"`
	Print domain.Item `json:"print"`
}

func (checkpoint releasesCheckpoint) hasPending() bool {
	return len(checkpoint.PendingPrints) > 0 ||
		len(checkpoint.PrintsByCard) > 0 ||
		len(checkpoint.GalleryRefs) > 0
}

func (checkpoint releasesCheckpoint) completeSnapshot() bool {
	return checkpoint.Complete && !checkpoint.hasPending()
}

// legacyReleasesCheckpoint is the version 1 format, where pending prints were
// still stored inside Sets and the map references used set/print indexes.
type legacyReleasesCheckpoint struct {
	Version  int       `json:"version"`
	SavedAt  time.Time `json:"saved_at"`
	Complete bool      `json:"complete"`

	Releases []Release  `json:"releases"`
	Sets     []savedSet `json:"sets"`

	ListPages    map[string]savedListPage   `json:"list_pages,omitempty"`
	PrintsByCard map[string][]savedPrintRef `json:"prints_by_card,omitempty"`
	GalleryRefs  map[string][]savedPrintRef `json:"gallery_refs,omitempty"`

	CardResults    map[string]savedCardResult  `json:"card_results,omitempty"`
	GalleryResults map[string][]gallerySection `json:"gallery_results,omitempty"`

	Processed map[string]bool            `json:"processed,omitempty"`
	Pages     map[string]domain.URLState `json:"pages,omitempty"`
	Failures  []pageFailure              `json:"failures,omitempty"`
}

type savedPrintRef struct {
	Set   int `json:"set"`
	Print int `json:"print"`
}

type savedListPage struct {
	SetIndexes []int `json:"set_indexes"`
	Booster    bool  `json:"booster"`
}

type savedCardResult struct {
	Info         *domain.Item                 `json:"info,omitempty"`
	Translations map[domain.Lang]cardLangData `json:"translations,omitempty"`
	GalleryURL   string                       `json:"gallery_url,omitempty"`
	Image        domain.Image                 `json:"image,omitempty"`
	Kind         cardKind                     `json:"kind,omitempty"`
	OK           bool                         `json:"ok,omitempty"`
}

func decodeReleasesCheckpoint(data []byte) (releasesCheckpoint, bool, error) {
	if len(data) == 0 {
		return releasesCheckpoint{}, false, nil
	}

	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return releasesCheckpoint{}, false, fmt.Errorf("decode releases checkpoint: %w", err)
	}

	switch header.Version {
	case 0, 1:
		var legacy legacyReleasesCheckpoint
		if err := json.Unmarshal(data, &legacy); err != nil {
			return releasesCheckpoint{}, false, fmt.Errorf("decode releases checkpoint: %w", err)
		}
		return convertLegacyCheckpoint(legacy), true, nil
	case releasesStateVersion:
		var checkpoint releasesCheckpoint
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			return releasesCheckpoint{}, false, fmt.Errorf("decode releases checkpoint: %w", err)
		}
		return checkpoint, true, nil
	default:
		return releasesCheckpoint{}, false,
			fmt.Errorf("unsupported releases checkpoint version %d", header.Version)
	}
}

// convertLegacyCheckpoint moves the prints referenced by the old card/gallery
// maps out of Sets. A legacy snapshot therefore becomes a complete-only
// snapshot plus the pending state needed to resume it.
func convertLegacyCheckpoint(legacy legacyReleasesCheckpoint) releasesCheckpoint {
	type printLocation struct {
		set   int
		print int
	}

	checkpoint := releasesCheckpoint{
		Version:        releasesStateVersion,
		SavedAt:        legacy.SavedAt,
		Releases:       legacy.Releases,
		ListPages:      legacy.ListPages,
		CardResults:    legacy.CardResults,
		GalleryResults: legacy.GalleryResults,
		Processed:      legacy.Processed,
		Pages:          legacy.Pages,
		Failures:       legacy.Failures,
	}

	pendingIndex := make(map[printLocation]int)
	addRefs := func(source map[string][]savedPrintRef) map[string][]int {
		if len(source) == 0 {
			return nil
		}

		result := make(map[string][]int, len(source))
		for url, refs := range source {
			for _, ref := range refs {
				if ref.Set < 0 || ref.Set >= len(legacy.Sets) {
					continue
				}
				if ref.Print < 0 || ref.Print >= len(legacy.Sets[ref.Set].Prints) {
					continue
				}

				location := printLocation{set: ref.Set, print: ref.Print}
				index, ok := pendingIndex[location]
				if !ok {
					index = len(checkpoint.PendingPrints)
					pendingIndex[location] = index
					checkpoint.PendingPrints = append(checkpoint.PendingPrints, savedPendingPrint{
						Set:   ref.Set,
						Print: legacy.Sets[ref.Set].Prints[ref.Print],
					})
				}
				result[url] = append(result[url], index)
			}
		}
		for url, indexes := range result {
			if len(indexes) == 0 {
				delete(result, url)
			}
		}
		return result
	}

	checkpoint.PrintsByCard = addRefs(legacy.PrintsByCard)
	checkpoint.GalleryRefs = addRefs(legacy.GalleryRefs)

	checkpoint.Sets = make([]savedSet, len(legacy.Sets))
	for setIndex, saved := range legacy.Sets {
		prints := make([]domain.Item, 0, len(saved.Prints))
		for printIndex, print := range saved.Prints {
			if _, pending := pendingIndex[printLocation{set: setIndex, print: printIndex}]; pending {
				continue
			}
			prints = append(prints, print)
		}
		checkpoint.Sets[setIndex] = savedSet{Set: saved.Set, Prints: prints}
	}

	checkpoint.Complete = legacy.Complete && !checkpoint.hasPending()
	return checkpoint
}

func newReleaseScrapeFromCheckpoint(provider *Provider, checkpoint releasesCheckpoint) (*releaseScrape, error) {
	scrape := newEmptyReleaseScrape(provider, checkpoint.Releases)
	scrape.resumable = true

	for _, saved := range checkpoint.Sets {
		set := saved.Set

		printed := make([]domain.Item, len(saved.Prints))
		copy(printed, saved.Prints)

		state := &setState{
			Set:    &set,
			Prints: make([]*domain.Item, len(printed)),
		}
		for index := range printed {
			state.Prints[index] = &printed[index]
			scrape.stateByPrint[state.Prints[index]] = state
		}
		scrape.states = append(scrape.states, state)
	}

	pendingPrints := make([]*domain.Item, len(checkpoint.PendingPrints))
	for index, saved := range checkpoint.PendingPrints {
		if saved.Set < 0 || saved.Set >= len(scrape.states) {
			continue
		}

		print := saved.Print
		pointer := &print
		state := scrape.states[saved.Set]
		state.Prints = append(state.Prints, pointer)
		scrape.stateByPrint[pointer] = state
		scrape.pending[pointer] = true
		pendingPrints[index] = pointer
	}

	refsFromIndexes := func(indexes []int) []*domain.Item {
		refs := make([]*domain.Item, 0, len(indexes))
		for _, index := range indexes {
			if index < 0 || index >= len(pendingPrints) {
				continue
			}
			refs = append(refs, pendingPrints[index])
		}
		return refs
	}

	for key, indexes := range checkpoint.PrintsByCard {
		if refs := refsFromIndexes(indexes); len(refs) > 0 {
			scrape.printsByCard[key] = refs
		}
	}
	for key, indexes := range checkpoint.GalleryRefs {
		if refs := refsFromIndexes(indexes); len(refs) > 0 {
			scrape.galleryRefs[key] = refs
		}
	}

	for key, savedPage := range checkpoint.ListPages {
		target := &setListPage{
			URL:     key,
			Booster: savedPage.Booster,
		}
		for _, index := range savedPage.SetIndexes {
			if index >= 0 && index < len(scrape.states) {
				target.States = append(target.States, scrape.states[index])
			}
		}
		scrape.listPageTargets[key] = target
	}

	for key, saved := range checkpoint.CardResults {
		scrape.cardResults[key] = cardScrapeResult{
			Info:         saved.Info,
			Translations: saved.Translations,
			GalleryURL:   saved.GalleryURL,
			Image:        saved.Image,
			Kind:         saved.Kind,
			OK:           saved.OK,
		}
	}
	for key, sections := range checkpoint.GalleryResults {
		scrape.galleryResults[key] = sections
	}

	for key, processed := range checkpoint.Processed {
		scrape.processed[key] = processed
	}
	scrape.pages = checkpoint.Pages

	return scrape, nil
}

func (s *releaseScrape) snapshot(complete bool) releasesCheckpoint {
	s.mu.Lock()
	defer s.mu.Unlock()

	pendingSet := s.pendingSetLocked()
	setIndexes := make(map[*setState]int, len(s.states))
	pendingIndexes := make(map[*domain.Item]int)
	sets := make([]savedSet, 0, len(s.states))
	pendingPrints := make([]savedPendingPrint, 0)

	for _, state := range s.states {
		if state == nil || state.Set == nil {
			continue
		}

		setIndex := len(sets)
		setIndexes[state] = setIndex
		saved := savedSet{Set: *state.Set, Prints: make([]domain.Item, 0, len(state.Prints))}

		for _, print := range state.Prints {
			if print == nil {
				continue
			}
			if pendingSet[print] {
				pendingIndexes[print] = len(pendingPrints)
				pendingPrints = append(pendingPrints, savedPendingPrint{
					Set:   setIndex,
					Print: *print,
				})
				continue
			}
			saved.Prints = append(saved.Prints, *print)
		}
		sets = append(sets, saved)
	}

	listPages := make(map[string]savedListPage, len(s.listPageTargets))
	for key, target := range s.listPageTargets {
		savedPage := savedListPage{Booster: target.Booster}
		for _, state := range target.States {
			if index, ok := setIndexes[state]; ok {
				savedPage.SetIndexes = append(savedPage.SetIndexes, index)
			}
		}
		listPages[key] = savedPage
	}

	printsByCard := make(map[string][]int, len(s.printsByCard))
	for key, refs := range s.printsByCard {
		if indexes := pendingRefs(refs, pendingIndexes); len(indexes) > 0 {
			printsByCard[key] = indexes
		}
	}
	galleryRefs := make(map[string][]int, len(s.galleryRefs))
	for key, refs := range s.galleryRefs {
		if indexes := pendingRefs(refs, pendingIndexes); len(indexes) > 0 {
			galleryRefs[key] = indexes
		}
	}

	cardResults := make(map[string]savedCardResult, len(s.cardResults))
	for key, result := range s.cardResults {
		cardResults[key] = savedCardResult{
			Info:         result.Info,
			Translations: result.Translations,
			GalleryURL:   result.GalleryURL,
			Image:        result.Image,
			Kind:         result.Kind,
			OK:           result.OK,
		}
	}

	processed := make(map[string]bool, len(s.processed))
	for key, value := range s.processed {
		processed[key] = value
	}

	pages := make(map[string]domain.URLState, len(s.pages))
	for key, value := range s.pages {
		pages[key] = value
	}

	setURLs := make(map[string]bool, len(s.releases))
	for _, release := range s.releases {
		setURLs[canonicalPageURL(release.URL)] = true
	}

	checkpointComplete := complete && len(pendingPrints) == 0 &&
		len(printsByCard) == 0 && len(galleryRefs) == 0

	return releasesCheckpoint{
		Version:        releasesStateVersion,
		Complete:       checkpointComplete,
		Releases:       append([]Release(nil), s.releases...),
		Sets:           sets,
		PendingPrints:  pendingPrints,
		ListPages:      listPages,
		PrintsByCard:   printsByCard,
		GalleryRefs:    galleryRefs,
		CardResults:    cardResults,
		GalleryResults: copyGalleryResults(s.galleryResults),
		Processed:      processed,
		Pages:          pages,
		Failures:       pageFailures(pages, setURLs),
	}
}

func pendingRefs(refs []*domain.Item, indexes map[*domain.Item]int) []int {
	saved := make([]int, 0, len(refs))
	for _, print := range refs {
		if index, ok := indexes[print]; ok {
			saved = append(saved, index)
		}
	}
	return saved
}

func copyGalleryResults(source map[string][]gallerySection) map[string][]gallerySection {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string][]gallerySection, len(source))
	for key, sections := range source {
		result[key] = sections
	}
	return result
}

func encodeReleasesCheckpoint(checkpoint releasesCheckpoint) (json.RawMessage, error) {
	checkpoint.Version = releasesStateVersion
	checkpoint.SavedAt = time.Now().UTC()

	data, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, fmt.Errorf("encode releases checkpoint: %w", err)
	}
	return data, nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json report: %w", err)
	}

	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create json report directory: %w", err)
		}
	}

	temp, err := os.CreateTemp(directory, ".ygo-report-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary json report: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write json report: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close json report: %w", err)
	}

	if err := os.Rename(tempName, path); err != nil {
		// Windows does not replace an existing file with Rename.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace json report: %w", err)
		}
		if renameErr := os.Rename(tempName, path); renameErr != nil {
			return fmt.Errorf("replace json report: %w", renameErr)
		}
	}

	return nil
}
