package ygo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/retrier"
)

const releasesStateVersion = 1

var releasesStatePath = "ygo-releases-state.json"

// releasesCheckpoint is the resumable state of GetAllReleases. Sets keep the
// extracted catalog data and the page maps keep enough information to rebuild
// the pending queue without repeating completed requests.
type releasesCheckpoint struct {
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

	Processed map[string]bool             `json:"processed,omitempty"`
	Pages     map[string]retrier.URLState `json:"pages,omitempty"`
	Failures  []pageFailure               `json:"failures,omitempty"`
}

type savedSet struct {
	Set    catalog.Set           `json:"set"`
	Prints []catalog.CardPrinted `json:"prints"`
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
	Info         *catalog.ItemInfo             `json:"info,omitempty"`
	Translations map[catalog.Lang]cardLangData `json:"translations,omitempty"`
	GalleryURL   string                        `json:"gallery_url,omitempty"`
	Image        catalog.Image                 `json:"image,omitempty"`
	Kind         cardKind                      `json:"kind,omitempty"`
	OK           bool                          `json:"ok,omitempty"`
}

func loadReleasesCheckpoint(path string) (releasesCheckpoint, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return releasesCheckpoint{}, false, nil
	}
	if err != nil {
		return releasesCheckpoint{}, false, fmt.Errorf("read releases checkpoint: %w", err)
	}

	var checkpoint releasesCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return releasesCheckpoint{}, false, fmt.Errorf("decode releases checkpoint: %w", err)
	}
	if checkpoint.Version != 0 && checkpoint.Version != releasesStateVersion {
		return releasesCheckpoint{}, false, fmt.Errorf("unsupported releases checkpoint version %d", checkpoint.Version)
	}

	return checkpoint, true, nil
}

func newReleaseScrapeFromCheckpoint(provider *Provider, checkpoint releasesCheckpoint) (*releaseScrape, error) {
	scrape := newEmptyReleaseScrape(provider, checkpoint.Releases)

	for _, saved := range checkpoint.Sets {
		set := saved.Set
		set.CardsPrinted = nil

		printed := make([]catalog.CardPrinted, len(saved.Prints))
		copy(printed, saved.Prints)

		state := &setState{
			Set:    &set,
			Prints: make([]*catalog.CardPrinted, len(printed)),
		}
		for index := range printed {
			state.Prints[index] = &printed[index]
			scrape.stateByPrint[state.Prints[index]] = state
		}
		scrape.states = append(scrape.states, state)
	}

	for key, refs := range checkpoint.PrintsByCard {
		scrape.printsByCard[key] = scrape.printsFromSaved(refs)
	}
	for key, refs := range checkpoint.GalleryRefs {
		scrape.galleryRefs[key] = scrape.printsFromSaved(refs)
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

func (s *releaseScrape) printsFromSaved(refs []savedPrintRef) []*catalog.CardPrinted {
	prints := make([]*catalog.CardPrinted, 0, len(refs))
	for _, ref := range refs {
		if ref.Set < 0 || ref.Set >= len(s.states) {
			continue
		}
		state := s.states[ref.Set]
		if ref.Print < 0 || ref.Print >= len(state.Prints) {
			continue
		}
		prints = append(prints, state.Prints[ref.Print])
	}
	return prints
}

func (s *releaseScrape) snapshot(complete bool) releasesCheckpoint {
	s.mu.Lock()
	defer s.mu.Unlock()

	setIndexes := make(map[*setState]int, len(s.states))
	printIndexes := make(map[*catalog.CardPrinted]savedPrintRef)

	for setIndex, state := range s.states {
		setIndexes[state] = setIndex
		for printIndex, print := range state.Prints {
			if print != nil {
				printIndexes[print] = savedPrintRef{Set: setIndex, Print: printIndex}
			}
		}
	}

	sets := make([]savedSet, len(s.states))
	for index, state := range s.states {
		set := *state.Set
		set.CardsPrinted = nil

		prints := make([]catalog.CardPrinted, len(state.Prints))
		for printIndex, print := range state.Prints {
			if print != nil {
				prints[printIndex] = *print
			}
		}
		sets[index] = savedSet{Set: set, Prints: prints}
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

	printsByCard := make(map[string][]savedPrintRef, len(s.printsByCard))
	for key, refs := range s.printsByCard {
		printsByCard[key] = savedRefs(refs, printIndexes)
	}
	galleryRefs := make(map[string][]savedPrintRef, len(s.galleryRefs))
	for key, refs := range s.galleryRefs {
		galleryRefs[key] = savedRefs(refs, printIndexes)
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

	pages := make(map[string]retrier.URLState, len(s.pages))
	for key, value := range s.pages {
		pages[key] = value
	}

	setURLs := make(map[string]bool, len(s.releases))
	for _, release := range s.releases {
		setURLs[canonicalPageURL(release.URL)] = true
	}

	return releasesCheckpoint{
		Version:        releasesStateVersion,
		Complete:       complete,
		Releases:       append([]Release(nil), s.releases...),
		Sets:           sets,
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

func savedRefs(refs []*catalog.CardPrinted, indexes map[*catalog.CardPrinted]savedPrintRef) []savedPrintRef {
	saved := make([]savedPrintRef, 0, len(refs))
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

func saveReleasesCheckpoint(path string, checkpoint releasesCheckpoint) error {
	checkpoint.Version = releasesStateVersion
	checkpoint.SavedAt = time.Now().UTC()

	return writeJSONAtomic(path, checkpoint)
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
