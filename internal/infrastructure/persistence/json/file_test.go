package jsonstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/binderex/catsync/internal/domain"
)

func TestFileRepositorySaveLoadAndFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retrier.json")
	repo := NewFile(path)
	ctx := context.Background()

	ygoRun := domain.RetrierRun{
		Version:  2,
		ID:       "ygo-all",
		Provider: "ygo",
		Mode:     "all",
		State:    domain.RetrierRunSuccess,
		States: map[string]domain.URLState{
			"http://a": {URL: "http://a", Visited: true, Completed: true},
		},
		Order: []string{"http://a"},
	}
	if err := repo.Save(ctx, ygoRun, nil); err != nil {
		t.Fatalf("Save ygo: %v", err)
	}

	sleeveRun := domain.RetrierRun{
		Version:  2,
		ID:       "ds-all",
		Provider: "dragonshield",
		Mode:     "all",
		State:    domain.RetrierRunCancelled,
		States: map[string]domain.URLState{
			"http://b": {URL: "http://b"},
		},
		Order: []string{"http://b"},
	}
	if err := repo.Save(ctx, sleeveRun, nil); err != nil {
		t.Fatalf("Save sleeves: %v", err)
	}

	loaded, err := repo.Load(ctx, "ygo-all")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Provider != "ygo" || !loaded.States["http://a"].Completed {
		t.Errorf("loaded = %+v", loaded)
	}

	summaries, err := repo.List(ctx, domain.RetrierRunFilter{Provider: "dragonshield", Mode: "all"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "ds-all" {
		t.Fatalf("summaries = %+v", summaries)
	}

	if err := repo.Delete(ctx, "ds-all"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Load(ctx, "ds-all"); err == nil {
		t.Fatal("deleted run is still loadable")
	}

	// A new repository over the same file sees the persisted run.
	reopened := NewFile(path)
	summaries, err = reopened.List(ctx, domain.RetrierRunFilter{})
	if err != nil {
		t.Fatalf("List reopened: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "ygo-all" {
		t.Fatalf("reopened summaries = %+v", summaries)
	}
}

func TestFileRepositoryReplace(t *testing.T) {
	repo := NewFile(filepath.Join(t.TempDir(), "retrier.json"))
	ctx := context.Background()

	if err := repo.Save(ctx, domain.RetrierRun{
		ID:       "old",
		Provider: "ygo",
		Mode:     "all",
		State:    domain.RetrierRunCancelled,
	}, nil); err != nil {
		t.Fatalf("Save old: %v", err)
	}

	if err := repo.Replace(ctx, []domain.RetrierRun{{
		ID:           "new",
		Provider:     "ygo",
		Mode:         "all",
		State:        domain.RetrierRunInterrupted,
		ProviderData: []byte(`{"complete":true}`),
	}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	summaries, err := repo.List(ctx, domain.RetrierRunFilter{Provider: "ygo", Mode: "all"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "new" {
		t.Fatalf("summaries = %+v", summaries)
	}

	run, err := repo.Load(ctx, "new")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(string(run.ProviderData), `"complete": true`) {
		t.Errorf("provider data = %s", run.ProviderData)
	}
}

func TestFileRepositoryMergesChangedStates(t *testing.T) {
	repo := NewFile(filepath.Join(t.TempDir(), "retrier.json"))
	ctx := context.Background()

	run := domain.RetrierRun{
		ID:       "run-1",
		Provider: "ygo",
		Mode:     "all",
		State:    domain.RetrierRunRunning,
		States: map[string]domain.URLState{
			"http://a": {URL: "http://a"},
		},
		Order: []string{"http://a", "http://b"},
	}
	if err := repo.Save(ctx, run, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	changed := domain.URLState{URL: "http://b", Visited: true, Completed: true}
	if err := repo.Save(ctx, run, []domain.URLState{changed}); err != nil {
		t.Fatalf("Save changed: %v", err)
	}

	loaded, err := repo.Load(ctx, "run-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.States["http://b"].Completed {
		t.Errorf("changed state was not merged: %+v", loaded.States)
	}
	if loaded.Progress.Total != 2 {
		t.Errorf("progress = %+v", loaded.Progress)
	}
}
