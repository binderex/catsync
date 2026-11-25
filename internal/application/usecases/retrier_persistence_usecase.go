package usecases

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
	"github.com/binderex/catsync/internal/utility/retrier"
)

const (
	defaultSaveInterval = 2 * time.Second
	persistenceTimeout  = 10 * time.Second
)

// PersistenceOption customizes RetrierPersistenceUseCase.
type PersistenceOption func(*RetrierPersistenceUseCase)

// WithSaveInterval changes how often intermediate progress is written. Zero
// writes every state change.
func WithSaveInterval(interval time.Duration) PersistenceOption {
	return func(uc *RetrierPersistenceUseCase) {
		if interval >= 0 {
			uc.saveInterval = interval
		}
	}
}

// RetrierPersistenceUseCase saves live retrier progress and resumes runs. It is
// injected into catalog providers through the retrier utility as a RunObserver.
type RetrierPersistenceUseCase struct {
	repo         ports.RetrierRunRepository
	saveInterval time.Duration

	mu     sync.Mutex
	saveMu sync.Mutex

	runID     domain.RetrierRunID
	provider  string
	mode      string
	startedAt time.Time

	states map[string]domain.URLState
	order  []string
	dirty  map[string]domain.URLState

	providerData json.RawMessage

	lastSave time.Time
}

// NewRetrierPersistenceUseCase creates a persistence use case backed by repo.
// The concrete repository implementation decides where the runs are stored.
func NewRetrierPersistenceUseCase(
	repo ports.RetrierRunRepository,
	opts ...PersistenceOption,
) *RetrierPersistenceUseCase {
	uc := &RetrierPersistenceUseCase{
		repo:         repo,
		saveInterval: defaultSaveInterval,
		states:       make(map[string]domain.URLState),
		dirty:        make(map[string]domain.URLState),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(uc)
		}
	}

	return uc
}

// RunStarted creates or updates the persisted header of a run.
func (uc *RetrierPersistenceUseCase) RunStarted(
	ctx context.Context,
	run domain.RetrierRun,
) (domain.RetrierRunID, error) {
	if uc.repo == nil {
		return "", domain.ErrPersistenceDisabled
	}

	// Resuming a run keeps the provider snapshot already stored with it.
	var existingProviderData json.RawMessage
	if run.ID != "" {
		if existing, err := uc.repo.Load(ctx, run.ID); err == nil {
			existingProviderData = existing.ProviderData
		}
	}

	uc.mu.Lock()

	if run.ID == "" {
		run.ID = domain.RetrierRunID(uuid.NewString())
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if len(run.ProviderData) > 0 {
		uc.providerData = run.ProviderData
	} else if existingProviderData != nil {
		uc.providerData = existingProviderData
	}

	uc.runID = run.ID
	uc.provider = run.Provider
	uc.mode = run.Mode
	uc.startedAt = run.StartedAt
	uc.states = copyStates(run.States)
	uc.order = copyOrder(run.Order, run.States)
	uc.dirty = make(map[string]domain.URLState)
	uc.lastSave = time.Now()

	persisted := uc.buildRunLocked(domain.RetrierRunRunning, nil, "")
	uc.mu.Unlock()

	if err := uc.save(ctx, persisted, statesInOrder(persisted.States, persisted.Order)); err != nil {
		return "", err
	}

	return persisted.ID, nil
}

// RunStateChanged updates one URL and persists the run when the save interval
// has elapsed.
func (uc *RetrierPersistenceUseCase) RunStateChanged(
	ctx context.Context,
	runID domain.RetrierRunID,
	state domain.URLState,
) error {
	if uc.repo == nil {
		return domain.ErrPersistenceDisabled
	}

	uc.mu.Lock()
	if uc.runID == "" {
		uc.runID = runID
	}
	if runID != "" && uc.runID != runID {
		uc.mu.Unlock()
		return fmt.Errorf("persist state: run %q does not match active run %q", runID, uc.runID)
	}

	uc.states[state.URL] = state
	uc.dirty[state.URL] = state

	if uc.saveInterval > 0 && time.Since(uc.lastSave) < uc.saveInterval {
		uc.mu.Unlock()
		return nil
	}

	changed := dirtyStates(uc.dirty)
	uc.dirty = make(map[string]domain.URLState)
	uc.lastSave = time.Now()

	persisted := uc.buildRunLocked(domain.RetrierRunRunning, nil, "")
	uc.mu.Unlock()

	if err := uc.save(ctx, persisted, changed); err != nil {
		uc.restoreDirty(changed)
		return err
	}

	return nil
}

// RunFinished persists the final state of a run.
func (uc *RetrierPersistenceUseCase) RunFinished(
	ctx context.Context,
	run domain.RetrierRun,
) error {
	if uc.repo == nil {
		return domain.ErrPersistenceDisabled
	}

	uc.mu.Lock()
	if run.ID != "" {
		uc.runID = run.ID
	}
	if run.Provider != "" {
		uc.provider = run.Provider
	}
	if run.Mode != "" {
		uc.mode = run.Mode
	}
	if !run.StartedAt.IsZero() {
		uc.startedAt = run.StartedAt
	}
	uc.states = copyStates(run.States)
	uc.order = copyOrder(run.Order, run.States)
	uc.dirty = make(map[string]domain.URLState)
	providerData := uc.providerData
	uc.mu.Unlock()

	run.ProviderData = providerData

	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now().UTC()
	}
	if run.FinishedAt == nil {
		finished := run.UpdatedAt
		run.FinishedAt = &finished
	}
	if run.Version == 0 {
		run.Version = 2
	}
	if run.Progress.Total == 0 && len(run.States) > 0 {
		run.Progress = domain.ProgressFromStates(run.States)
	}
	if len(run.Failures) == 0 {
		run.Failures = domain.FailuresFromStates(run.States)
	}
	if len(run.Order) == 0 {
		run.Order = copyOrder(nil, run.States)
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()

	return uc.save(saveCtx, run, statesInOrder(run.States, run.Order))
}

// Save writes a full run snapshot.
func (uc *RetrierPersistenceUseCase) Save(ctx context.Context, run domain.RetrierRun) error {
	if uc.repo == nil {
		return domain.ErrPersistenceDisabled
	}

	return uc.save(ctx, run, statesInOrder(run.States, run.Order))
}

// SaveProviderData attaches a provider-specific crawl snapshot to the active
// or latest run for provider and mode. When no retrier queue was used (for
// example, a complete checkpoint import), it creates a completed run record.
func (uc *RetrierPersistenceUseCase) SaveProviderData(
	ctx context.Context,
	provider, mode string,
	data json.RawMessage,
) error {
	if uc.repo == nil {
		return domain.ErrPersistenceDisabled
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()

	uc.mu.Lock()
	uc.providerData = data
	runID := uc.runID
	uc.mu.Unlock()

	run, err := uc.providerRun(saveCtx, runID, provider, mode)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if run.ID == "" {
		run = domain.RetrierRun{
			Version:    2,
			ID:         domain.RetrierRunID(uuid.NewString()),
			Provider:   provider,
			Mode:       mode,
			State:      domain.RetrierRunSuccess,
			StartedAt:  now,
			FinishedAt: &now,
		}
	}
	if run.Version == 0 {
		run.Version = 2
	}
	run.ProviderData = data
	run.UpdatedAt = now

	return uc.save(saveCtx, run, statesInOrder(run.States, run.Order))
}

func (uc *RetrierPersistenceUseCase) providerRun(
	ctx context.Context,
	runID domain.RetrierRunID,
	provider, mode string,
) (domain.RetrierRun, error) {
	if runID != "" {
		if run, err := uc.repo.Load(ctx, runID); err == nil {
			return run, nil
		}
	}

	summaries, err := uc.repo.List(ctx, domain.RetrierRunFilter{
		Provider: provider,
		Mode:     mode,
	})
	if err != nil {
		return domain.RetrierRun{}, err
	}
	if len(summaries) == 0 {
		return domain.RetrierRun{}, nil
	}

	return uc.repo.Load(ctx, summaries[0].ID)
}

// Load returns a persisted run.
func (uc *RetrierPersistenceUseCase) Load(
	ctx context.Context,
	id domain.RetrierRunID,
) (domain.RetrierRun, error) {
	if uc.repo == nil {
		return domain.RetrierRun{}, domain.ErrPersistenceDisabled
	}
	return uc.repo.Load(ctx, id)
}

// LoadLatest returns the latest resumable run for provider and mode.
func (uc *RetrierPersistenceUseCase) LoadLatest(
	ctx context.Context,
	provider, mode string,
) (domain.RetrierRun, bool, error) {
	if uc.repo == nil {
		return domain.RetrierRun{}, false, domain.ErrPersistenceDisabled
	}

	summaries, err := uc.repo.List(ctx, domain.RetrierRunFilter{
		Provider: provider,
		Mode:     mode,
	})
	if err != nil {
		return domain.RetrierRun{}, false, err
	}

	var latest domain.RetrierRunSummary
	found := false
	for _, summary := range summaries {
		if !resumableState(summary.State) {
			continue
		}
		if !found || summary.UpdatedAt.After(latest.UpdatedAt) {
			latest = summary
			found = true
		}
	}
	if !found {
		return domain.RetrierRun{}, false, nil
	}

	run, err := uc.repo.Load(ctx, latest.ID)
	if err != nil {
		return domain.RetrierRun{}, false, err
	}
	return run, true, nil
}

// List returns the persisted runs that match filter.
func (uc *RetrierPersistenceUseCase) List(
	ctx context.Context,
	filter domain.RetrierRunFilter,
) ([]domain.RetrierRunSummary, error) {
	if uc.repo == nil {
		return nil, domain.ErrPersistenceDisabled
	}
	return uc.repo.List(ctx, filter)
}

// Delete removes a persisted run.
func (uc *RetrierPersistenceUseCase) Delete(ctx context.Context, id domain.RetrierRunID) error {
	if uc.repo == nil {
		return domain.ErrPersistenceDisabled
	}
	return uc.repo.Delete(ctx, id)
}

func resumableState(state domain.RetrierRunState) bool {
	switch state {
	case domain.RetrierRunRunning, domain.RetrierRunCancelled, domain.RetrierRunInterrupted:
		return true
	default:
		return false
	}
}

func (uc *RetrierPersistenceUseCase) save(
	ctx context.Context,
	run domain.RetrierRun,
	changed []domain.URLState,
) error {
	uc.saveMu.Lock()
	defer uc.saveMu.Unlock()

	if err := uc.repo.Save(ctx, run, changed); err != nil {
		return fmt.Errorf("save retrier run: %w", err)
	}

	return nil
}

func (uc *RetrierPersistenceUseCase) buildRunLocked(
	state domain.RetrierRunState,
	finishedAt *time.Time,
	errMsg string,
) domain.RetrierRun {
	updated := time.Now().UTC()
	progress := domain.ProgressFromStates(uc.states)

	if progress.Current == "" && len(uc.order) > 0 {
		for _, url := range uc.order {
			if current, ok := uc.states[url]; ok && !current.Terminal() {
				progress.Current = url
				progress.CurrentAttempt = current.Attempts
				break
			}
		}
	}

	return domain.RetrierRun{
		Version:      2,
		ID:           uc.runID,
		Provider:     uc.provider,
		Mode:         uc.mode,
		State:        state,
		StartedAt:    uc.startedAt,
		UpdatedAt:    updated,
		FinishedAt:   finishedAt,
		Error:        errMsg,
		Progress:     progress,
		Order:        append([]string(nil), uc.order...),
		States:       copyStates(uc.states),
		Failures:     domain.FailuresFromStates(uc.states),
		ProviderData: uc.providerData,
	}
}

func (uc *RetrierPersistenceUseCase) restoreDirty(states []domain.URLState) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	for _, state := range states {
		if _, exists := uc.dirty[state.URL]; !exists {
			uc.dirty[state.URL] = state
		}
	}
}

func copyStates(states map[string]domain.URLState) map[string]domain.URLState {
	copied := make(map[string]domain.URLState, len(states))
	for url, state := range states {
		copied[url] = state
	}
	return copied
}

func copyOrder(order []string, states map[string]domain.URLState) []string {
	copied := make([]string, 0, len(states))
	seen := make(map[string]bool, len(states))

	for _, url := range order {
		if _, ok := states[url]; !ok || seen[url] {
			continue
		}
		copied = append(copied, url)
		seen[url] = true
	}

	missing := make([]string, 0, len(states)-len(copied))
	for url := range states {
		if seen[url] {
			continue
		}
		missing = append(missing, url)
	}
	sort.Strings(missing)
	copied = append(copied, missing...)

	return copied
}

func statesInOrder(states map[string]domain.URLState, order []string) []domain.URLState {
	result := make([]domain.URLState, 0, len(states))
	seen := make(map[string]bool, len(states))

	for _, url := range order {
		state, ok := states[url]
		if !ok || seen[url] {
			continue
		}
		result = append(result, state)
		seen[url] = true
	}
	for url, state := range states {
		if seen[url] {
			continue
		}
		result = append(result, state)
		seen[url] = true
	}

	return result
}

func dirtyStates(dirty map[string]domain.URLState) []domain.URLState {
	states := make([]domain.URLState, 0, len(dirty))
	for _, state := range dirty {
		states = append(states, state)
	}
	return states
}

// compile-time checks.
var (
	_ retrier.RunObserver      = (*RetrierPersistenceUseCase)(nil)
	_ ports.RetrierPersistence = (*RetrierPersistenceUseCase)(nil)
)
