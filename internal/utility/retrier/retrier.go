// Package retrier is a small utility for fetching a queue of URLs with retries
// and optional persistence. It is technical infrastructure, not business
// domain: providers use it to crawl pages, and the retrier persistence use case
// plugs into it through RunObserver.
package retrier

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

const (
	defaultProgressInterval = 200 * time.Millisecond
	finalSaveTimeout        = 10 * time.Second
)

// RunObserver receives run lifecycle events. The retrier persistence use case
// implements it to save progress and allow resuming.
type RunObserver interface {
	RunStarted(ctx context.Context, run domain.RetrierRun) (domain.RetrierRunID, error)
	RunStateChanged(ctx context.Context, runID domain.RetrierRunID, state domain.URLState) error
	RunFinished(ctx context.Context, run domain.RetrierRun) error
}

// Presenter renders progress. ConsolePresenter is the built-in implementation.
type Presenter interface {
	Show(domain.Progress)
	Finish(domain.Progress, error)
}

// Option customizes a Retrier.
type Option func(*Retrier)

// WithPolicy replaces the retry policy.
func WithPolicy(policy domain.Policy) Option {
	return func(r *Retrier) {
		if policy != (domain.Policy{}) {
			r.policy = policy
		}
	}
}

// WithProvider sets the provider name used by the run snapshot and persistence.
func WithProvider(provider string) Option {
	return func(r *Retrier) {
		r.provider = provider
	}
}

// WithMode tags the run so persistence can distinguish crawl modes, for example
// "new" and "all".
func WithMode(mode string) Option {
	return func(r *Retrier) {
		r.mode = mode
	}
}

// WithObserver attaches a run observer.
func WithObserver(observer RunObserver) Option {
	return func(r *Retrier) {
		r.observer = observer
	}
}

// WithRunID reuses an existing persisted run ID. Resuming a crawl with it
// updates the same run record instead of creating a new one.
func WithRunID(id domain.RetrierRunID) Option {
	return func(r *Retrier) {
		if id != "" {
			r.runID = id
		}
	}
}

// WithPresenter attaches a progress presenter.
func WithPresenter(presenter Presenter) Option {
	return func(r *Retrier) {
		r.presenter = presenter
	}
}

// WithUserAgent sets the User-Agent used by the HTTP transport.
func WithUserAgent(userAgent string) Option {
	return func(r *Retrier) {
		r.userAgent = userAgent
	}
}

// Retrier owns one queue and one transport. It is created through NewHTTP,
// NewColly or NewChromedp.
type Retrier struct {
	mu sync.Mutex

	policy    domain.Policy
	provider  string
	mode      string
	observer  RunObserver
	presenter Presenter
	userAgent string

	execPath         string
	headless         bool
	timeout          time.Duration
	challengeTimeout time.Duration

	fetch func(ctx context.Context, url string) (domain.Response, error)
	open  func(ctx context.Context) error
	close func() error

	states map[string]*domain.URLState
	order  []string

	runID     domain.RetrierRunID
	startedAt time.Time
}

func newRetrier(policy domain.Policy, opts ...Option) *Retrier {
	r := &Retrier{
		policy:   policy,
		headless: false,
		timeout:  3 * time.Minute,
		states:   make(map[string]*domain.URLState),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Add queues a URL with an optional filter.
func (r *Retrier) Add(url string, filter domain.Filter) {
	if url == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.states[url]; exists {
		return
	}

	r.states[url] = &domain.URLState{URL: url, Filter: filter}
	r.order = append(r.order, url)
}

// Restore replaces the queue with persisted states. resolve rebuilds the filter
// of every pending URL.
func (r *Retrier) Restore(
	states map[string]domain.URLState,
	order []string,
	resolve domain.FilterResolver,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.states = make(map[string]*domain.URLState, len(states))
	r.order = make([]string, 0, len(states))

	for _, url := range order {
		state, ok := states[url]
		if !ok {
			continue
		}
		state.URL = url
		state.Filter = nil
		if !state.Terminal() && resolve != nil {
			state.Filter = resolve(state)
		}
		r.states[url] = &state
		r.order = append(r.order, url)
	}

	for url, state := range states {
		if _, exists := r.states[url]; exists {
			continue
		}
		state.URL = url
		state.Filter = nil
		if !state.Terminal() && resolve != nil {
			state.Filter = resolve(state)
		}
		r.states[url] = &state
		r.order = append(r.order, url)
	}
}

// Fetch retrieves one URL using the retry policy without touching the queue.
func (r *Retrier) Fetch(ctx context.Context, url string) (domain.Response, error) {
	response, err := r.fetchWithRetry(ctx, url)
	if err != nil {
		return response, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response, fmt.Errorf("%w: %d", domain.ErrUnexpectedStatus, response.StatusCode)
	}
	return response, nil
}

// Run processes the queue until it is empty. Failed URLs do not make Run return
// an error; inspect States for details.
func (r *Retrier) Run(ctx context.Context) (runErr error) {
	if r.startedAt.IsZero() {
		r.startedAt = time.Now().UTC()
	}

	if r.observer != nil {
		run := r.snapshot(domain.RetrierRunRunning, "")
		runID, err := r.observer.RunStarted(ctx, run)
		if err != nil {
			return fmt.Errorf("start retrier run: %w", err)
		}
		r.runID = runID
	}

	stopProgress := r.startProgressLoop()
	defer stopProgress()

	sessionOpen := r.open != nil && r.hasPending()
	sessionClosed := false
	if sessionOpen {
		if err := r.open(ctx); err != nil {
			return fmt.Errorf("open transport: %w", err)
		}
		defer func() {
			if !sessionClosed && r.close != nil {
				_ = r.close()
			}
		}()
	}

	for {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}

		url, ok := r.next()
		if !ok {
			break
		}

		if err := r.processOne(ctx, url); err != nil {
			runErr = err
			break
		}
	}

	if sessionOpen && r.close != nil {
		_ = r.close()
		sessionClosed = true
	}

	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}

	finalState := domain.RetrierRunSuccess
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			finalState = domain.RetrierRunCancelled
		} else {
			finalState = domain.RetrierRunFailed
		}
	}

	finished := time.Now().UTC()
	run := r.snapshot(finalState, errorString(runErr))
	run.FinishedAt = &finished
	run.Duration = finished.Sub(run.StartedAt).Round(time.Millisecond).String()

	if r.observer != nil {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalSaveTimeout)
		if err := r.observer.RunFinished(saveCtx, run); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("finish retrier run: %w", err))
		}
		cancel()
	}

	if r.presenter != nil {
		r.presenter.Finish(run.Progress, runErr)
	}

	return runErr
}

func (r *Retrier) processOne(ctx context.Context, url string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, filter, attempt, ok := r.beginAttempt(url)
		if !ok {
			return nil
		}
		r.notifyState(ctx, url)

		response, fetchErr := r.fetch(ctx, url)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		status := response.StatusCode
		if fetchErr == nil && status >= http.StatusOK && status < http.StatusMultipleChoices {
			if filter != nil && filter(response) {
				r.markInvalid(url)
			} else {
				r.markCompleted(url)
			}
			r.notifyState(ctx, url)
			return nil
		}

		if status == http.StatusNotFound {
			r.markNotFound(url)
			r.notifyState(ctx, url)
			return nil
		}

		var retryAfter time.Duration
		if response.Header != nil {
			retryAfter = domain.ParseRetryAfter(response.Header.Get("Retry-After"))
		}

		if !domain.IsRetryableStatus(status) || attempt >= r.policy.Attempts(status) {
			r.markFailed(url, failureReason(response, fetchErr))
			r.notifyState(ctx, url)
			return nil
		}

		if !sleep(ctx, r.policy.Wait(status, attempt, retryAfter)) {
			return ctx.Err()
		}
	}
}

func (r *Retrier) fetchWithRetry(ctx context.Context, url string) (domain.Response, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return domain.Response{URL: url}, err
		}

		response, fetchErr := r.fetch(ctx, url)
		status := response.StatusCode

		if fetchErr == nil && status >= http.StatusOK && status < http.StatusMultipleChoices {
			return response, nil
		}
		if status == http.StatusNotFound {
			return response, fmt.Errorf("%w: %s", domain.ErrNotFound, url)
		}

		if !domain.IsRetryableStatus(status) || attempt >= r.policy.Attempts(status) {
			if fetchErr != nil {
				return response, fetchErr
			}
			return response, fmt.Errorf("%w: %d", domain.ErrUnexpectedStatus, status)
		}

		var retryAfter time.Duration
		if response.Header != nil {
			retryAfter = domain.ParseRetryAfter(response.Header.Get("Retry-After"))
		}

		if !sleep(ctx, r.policy.Wait(status, attempt, retryAfter)) {
			return response, ctx.Err()
		}
	}
}

// States returns a snapshot of every tracked URL.
func (r *Retrier) States() map[string]domain.URLState {
	r.mu.Lock()
	defer r.mu.Unlock()

	states := make(map[string]domain.URLState, len(r.states))
	for url, state := range r.states {
		states[url] = *state
	}
	return states
}

// Progress returns a snapshot of the queue counters.
func (r *Retrier) Progress() domain.Progress {
	r.mu.Lock()
	defer r.mu.Unlock()

	progress := domain.ProgressFromStates(r.statesSnapshotLocked())
	progress.Current = r.currentStateLocked()
	progress.CurrentAttempt = r.currentAttemptLocked()
	return progress
}

func (r *Retrier) snapshot(state domain.RetrierRunState, errMsg string) domain.RetrierRun {
	r.mu.Lock()
	defer r.mu.Unlock()

	started := r.startedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}

	states := r.statesSnapshotLocked()
	progress := domain.ProgressFromStates(states)
	progress.Current = r.currentStateLocked()
	progress.CurrentAttempt = r.currentAttemptLocked()

	return domain.RetrierRun{
		Version:   2,
		ID:        r.runID,
		Provider:  r.provider,
		Mode:      r.mode,
		State:     state,
		StartedAt: started,
		UpdatedAt: time.Now().UTC(),
		Progress:  progress,
		Order:     append([]string(nil), r.order...),
		States:    states,
		Failures:  domain.FailuresFromStates(states),
		Error:     errMsg,
	}
}

func (r *Retrier) notifyState(ctx context.Context, url string) {
	if r.observer == nil {
		return
	}

	state, ok := r.state(url)
	if !ok {
		return
	}
	if err := r.observer.RunStateChanged(ctx, r.runID, state); err != nil {
		log.Printf("retrier persistence: %v", err)
	}
}

func (r *Retrier) startProgressLoop() func() {
	if r.presenter == nil {
		return func() {}
	}

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(defaultProgressInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.presenter.Show(r.Progress())
			case <-stop:
				return
			}
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}

func (r *Retrier) hasPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, state := range r.states {
		if !state.Terminal() {
			return true
		}
	}
	return false
}

func (r *Retrier) next() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, url := range r.order {
		if state := r.states[url]; state != nil && !state.Terminal() {
			return url, true
		}
	}
	return "", false
}

func (r *Retrier) beginAttempt(url string) (domain.URLState, domain.Filter, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.states[url]
	if state == nil || state.Terminal() {
		return domain.URLState{}, nil, 0, false
	}

	state.Visited = true
	state.Attempts++

	return *state, state.Filter, state.Attempts, true
}

func (r *Retrier) state(url string) (domain.URLState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.states[url]
	if state == nil {
		return domain.URLState{}, false
	}
	return *state, true
}

func (r *Retrier) markCompleted(url string) {
	r.update(url, func(state *domain.URLState) {
		state.Scraped = true
		state.Completed = true
	})
}

func (r *Retrier) markNotFound(url string) {
	r.update(url, func(state *domain.URLState) {
		state.NotFound = true
	})
}

func (r *Retrier) markInvalid(url string) {
	r.update(url, func(state *domain.URLState) {
		state.Scraped = true
		state.Invalid = true
	})
}

func (r *Retrier) markFailed(url, reason string) {
	r.update(url, func(state *domain.URLState) {
		state.Failed = true
		state.Error = reason
	})
}

func (r *Retrier) update(url string, update func(*domain.URLState)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if state := r.states[url]; state != nil {
		update(state)
	}
}

func (r *Retrier) statesSnapshotLocked() map[string]domain.URLState {
	states := make(map[string]domain.URLState, len(r.states))
	for url, state := range r.states {
		states[url] = *state
	}
	return states
}

func (r *Retrier) currentStateLocked() string {
	for _, url := range r.order {
		if state := r.states[url]; state != nil && state.Visited && !state.Terminal() {
			return url
		}
	}
	return ""
}

func (r *Retrier) currentAttemptLocked() int {
	for _, url := range r.order {
		if state := r.states[url]; state != nil && state.Visited && !state.Terminal() {
			return state.Attempts
		}
	}
	return 0
}

func failureReason(response domain.Response, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("unexpected status %d", response.StatusCode)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sleep(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
