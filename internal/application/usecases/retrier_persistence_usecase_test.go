package usecases

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

type fakeRunRepository struct {
	mu    sync.Mutex
	runs  map[domain.RetrierRunID]domain.RetrierRun
	saves []domain.RetrierRun
}

func newFakeRunRepository() *fakeRunRepository {
	return &fakeRunRepository{runs: make(map[domain.RetrierRunID]domain.RetrierRun)}
}

func (r *fakeRunRepository) Save(_ context.Context, run domain.RetrierRun, changed []domain.URLState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	copied := run
	copied.States = copyStates(run.States)
	for _, state := range changed {
		copied.States[state.URL] = state
	}
	r.runs[run.ID] = copied
	r.saves = append(r.saves, copied)
	return nil
}

func (r *fakeRunRepository) Load(_ context.Context, id domain.RetrierRunID) (domain.RetrierRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run, ok := r.runs[id]
	if !ok {
		return domain.RetrierRun{}, domain.ErrRunNotFound
	}
	run.States = copyStates(run.States)
	return run, nil
}

func (r *fakeRunRepository) List(_ context.Context, filter domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	summaries := make([]domain.RetrierRunSummary, 0, len(r.runs))
	for _, run := range r.runs {
		if filter.Provider != "" && run.Provider != filter.Provider {
			continue
		}
		if filter.Mode != "" && run.Mode != filter.Mode {
			continue
		}
		if filter.State != "" && run.State != filter.State {
			continue
		}
		summaries = append(summaries, domain.RetrierRunSummary{
			Version:   run.Version,
			ID:        run.ID,
			Provider:  run.Provider,
			Mode:      run.Mode,
			State:     run.State,
			StartedAt: run.StartedAt,
			UpdatedAt: run.UpdatedAt,
			Progress:  run.Progress,
		})
	}
	return summaries, nil
}

func (r *fakeRunRepository) Delete(_ context.Context, id domain.RetrierRunID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.runs[id]; !ok {
		return domain.ErrRunNotFound
	}
	delete(r.runs, id)
	return nil
}

func (r *fakeRunRepository) stored(id domain.RetrierRunID) domain.RetrierRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[id]
}

func TestPersistenceRunLifecycle(t *testing.T) {
	repo := newFakeRunRepository()
	uc := NewRetrierPersistenceUseCase(repo, WithSaveInterval(0))

	start := domain.RetrierRun{
		Provider: "test",
		States: map[string]domain.URLState{
			"http://a": {URL: "http://a"},
			"http://b": {URL: "http://b"},
		},
		Order: []string{"http://a", "http://b"},
	}

	id, err := uc.RunStarted(context.Background(), start)
	if err != nil {
		t.Fatalf("RunStarted: %v", err)
	}
	if id == "" {
		t.Fatal("empty run id")
	}

	run := repo.stored(id)
	if run.State != domain.RetrierRunRunning {
		t.Errorf("state = %q", run.State)
	}
	if run.Progress.Total != 2 {
		t.Errorf("progress = %+v", run.Progress)
	}

	changed := domain.URLState{
		URL:       "http://a",
		Visited:   true,
		Completed: true,
		Attempts:  1,
	}
	if err := uc.RunStateChanged(context.Background(), id, changed); err != nil {
		t.Fatalf("RunStateChanged: %v", err)
	}

	run = repo.stored(id)
	if !run.States["http://a"].Completed {
		t.Errorf("changed state not saved: %+v", run.States["http://a"])
	}

	finished := time.Now().UTC()
	final := domain.RetrierRun{
		Version:    2,
		ID:         id,
		Provider:   "test",
		State:      domain.RetrierRunSuccess,
		StartedAt:  run.StartedAt,
		UpdatedAt:  finished,
		FinishedAt: &finished,
		Order:      []string{"http://a", "http://b"},
		States: map[string]domain.URLState{
			"http://a": {URL: "http://a", Visited: true, Completed: true, Attempts: 1},
			"http://b": {URL: "http://b", Visited: true, Completed: true, Attempts: 1},
		},
	}
	if err := uc.RunFinished(context.Background(), final); err != nil {
		t.Fatalf("RunFinished: %v", err)
	}

	run = repo.stored(id)
	if run.State != domain.RetrierRunSuccess {
		t.Errorf("final state = %q", run.State)
	}
	if run.Progress.Completed != 2 {
		t.Errorf("final progress = %+v", run.Progress)
	}
}

func TestPersistenceLoadLatestFiltersProviderAndMode(t *testing.T) {
	repo := newFakeRunRepository()
	uc := NewRetrierPersistenceUseCase(repo)

	now := time.Now().UTC()
	runs := []domain.RetrierRun{
		{
			Version:   2,
			ID:        "ygo-new",
			Provider:  "ygo",
			Mode:      "new",
			State:     domain.RetrierRunCancelled,
			UpdatedAt: now.Add(-2 * time.Hour),
			States:    map[string]domain.URLState{"http://ygo-new": {URL: "http://ygo-new"}},
			Order:     []string{"http://ygo-new"},
		},
		{
			Version:   2,
			ID:        "ygo-all",
			Provider:  "ygo",
			Mode:      "all",
			State:     domain.RetrierRunCancelled,
			UpdatedAt: now.Add(-time.Hour),
			States:    map[string]domain.URLState{"http://ygo-all": {URL: "http://ygo-all"}},
			Order:     []string{"http://ygo-all"},
		},
		{
			Version:   2,
			ID:        "ds-all",
			Provider:  "dragonshield",
			Mode:      "all",
			State:     domain.RetrierRunCancelled,
			UpdatedAt: now,
			States:    map[string]domain.URLState{"http://ds": {URL: "http://ds"}},
			Order:     []string{"http://ds"},
		},
	}
	for _, run := range runs {
		if err := repo.Save(context.Background(), run, nil); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		provider string
		mode     string
		want     domain.RetrierRunID
	}{
		{provider: "ygo", mode: "new", want: "ygo-new"},
		{provider: "ygo", mode: "all", want: "ygo-all"},
		{provider: "dragonshield", mode: "all", want: "ds-all"},
		{provider: "ygo", mode: "missing"},
	}

	for _, tc := range cases {
		run, found, err := uc.LoadLatest(context.Background(), tc.provider, tc.mode)
		if err != nil {
			t.Fatalf("LoadLatest(%q, %q): %v", tc.provider, tc.mode, err)
		}
		if tc.want == "" {
			if found {
				t.Errorf("LoadLatest(%q, %q) found %q, want none", tc.provider, tc.mode, run.ID)
			}
			continue
		}
		if !found {
			t.Errorf("LoadLatest(%q, %q) found nothing", tc.provider, tc.mode)
			continue
		}
		if run.ID != tc.want {
			t.Errorf("LoadLatest(%q, %q) = %q, want %q", tc.provider, tc.mode, run.ID, tc.want)
		}
	}
}

func TestPersistenceLoadLatestSkipsFinishedRuns(t *testing.T) {
	repo := newFakeRunRepository()
	uc := NewRetrierPersistenceUseCase(repo)

	now := time.Now().UTC()
	finished := now.Add(-30 * time.Minute)
	runs := []domain.RetrierRun{
		{
			Version:    2,
			ID:         "finished",
			Provider:   "ygo",
			Mode:       "all",
			State:      domain.RetrierRunSuccess,
			UpdatedAt:  now,
			FinishedAt: &finished,
			States:     map[string]domain.URLState{"http://done": {URL: "http://done", Completed: true}},
			Order:      []string{"http://done"},
		},
		{
			Version:   2,
			ID:        "cancelled",
			Provider:  "ygo",
			Mode:      "all",
			State:     domain.RetrierRunCancelled,
			UpdatedAt: now.Add(-time.Hour),
			States:    map[string]domain.URLState{"http://pending": {URL: "http://pending"}},
			Order:     []string{"http://pending"},
		},
	}
	for _, run := range runs {
		if err := repo.Save(context.Background(), run, nil); err != nil {
			t.Fatal(err)
		}
	}

	run, found, err := uc.LoadLatest(context.Background(), "ygo", "all")
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	if !found || run.ID != "cancelled" {
		t.Fatalf("LoadLatest = %q, found=%v, want cancelled", run.ID, found)
	}
}

func TestPersistenceSavesProviderDataAndKeepsItOnResume(t *testing.T) {
	repo := newFakeRunRepository()
	uc := NewRetrierPersistenceUseCase(repo)

	start := domain.RetrierRun{
		Provider: "ygo",
		Mode:     "all",
		States: map[string]domain.URLState{
			"http://a": {URL: "http://a"},
		},
		Order: []string{"http://a"},
	}

	id, err := uc.RunStarted(context.Background(), start)
	if err != nil {
		t.Fatalf("RunStarted: %v", err)
	}

	data := json.RawMessage(`{"sets":2,"prints":3}`)
	if err := uc.SaveProviderData(context.Background(), "ygo", "all", data); err != nil {
		t.Fatalf("SaveProviderData: %v", err)
	}

	run := repo.stored(id)
	if string(run.ProviderData) != string(data) {
		t.Fatalf("provider data = %s, want %s", run.ProviderData, data)
	}

	// Resuming the same run keeps the snapshot attached.
	resumedID, err := uc.RunStarted(context.Background(), domain.RetrierRun{
		ID:       id,
		Provider: "ygo",
		Mode:     "all",
		States:   start.States,
		Order:    start.Order,
	})
	if err != nil {
		t.Fatalf("RunStarted resume: %v", err)
	}
	if resumedID != id {
		t.Fatalf("run id = %q, want %q", resumedID, id)
	}
	if string(repo.stored(id).ProviderData) != string(data) {
		t.Fatal("provider data was lost on resume")
	}
}

func TestPersistenceDebouncesProgress(t *testing.T) {
	repo := newFakeRunRepository()
	uc := NewRetrierPersistenceUseCase(repo, WithSaveInterval(time.Hour))

	id, err := uc.RunStarted(context.Background(), domain.RetrierRun{
		Provider: "test",
		States:   map[string]domain.URLState{"http://a": {URL: "http://a"}},
		Order:    []string{"http://a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	saves := len(repo.saves)
	state := domain.URLState{URL: "http://a", Visited: true, Completed: true}
	if err := uc.RunStateChanged(context.Background(), id, state); err != nil {
		t.Fatal(err)
	}
	if len(repo.saves) != saves {
		t.Fatalf("intermediate save was not debounced")
	}

	if err := uc.RunFinished(context.Background(), domain.RetrierRun{
		Version: 2,
		ID:      id,
		State:   domain.RetrierRunSuccess,
		States:  map[string]domain.URLState{"http://a": state},
		Order:   []string{"http://a"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.saves) <= saves {
		t.Fatal("final save did not run")
	}
}
