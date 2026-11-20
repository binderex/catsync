// Package retrier provides a generic URL retrier with a dynamic queue,
// per-URL validity filters and pluggable HTTP or Colly transports.
package retrier

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Response is the data a Filter can inspect.
type Response struct {
	URL        string
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Filter reports whether a response is invalid. A true result marks the URL as
// Invalid and stops retrying it.
type Filter func(Response) bool

// RejectSelector returns a Filter that marks a response invalid when any of the
// CSS selectors matches the response body.
func RejectSelector(selectors ...string) Filter {
	return func(resp Response) bool {
		if len(resp.Body) == 0 || len(selectors) == 0 {
			return false
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
		if err != nil {
			return false
		}

		for _, selector := range selectors {
			if doc.Find(selector).Length() > 0 {
				return true
			}
		}

		return false
	}
}

// RejectWhen adapts an arbitrary predicate to a Filter.
func RejectWhen(predicate func(Response) bool) Filter {
	return Filter(predicate)
}

// URLState is the retry state of a single URL.
type URLState struct {
	URL       string `json:"url"`
	Visited   bool   `json:"visited"`
	Scraped   bool   `json:"scraped"`
	Completed bool   `json:"completed"`
	NotFound  bool   `json:"not_found"`
	Invalid   bool   `json:"invalid"`
	Failed    bool   `json:"failed"`
	Attempts  int    `json:"attempts"`
	Error     string `json:"error,omitempty"`

	// Filter decides whether a successful response is invalid. It is not
	// serialized and is kept per URL.
	Filter Filter `json:"-"`
}

// Terminal reports whether the URL no longer needs more attempts.
func (s URLState) Terminal() bool {
	return s.Completed || s.NotFound || s.Invalid || s.Failed
}

// Retrier is implemented by the HTTP and Colly transports.
type Retrier interface {
	// Add queues a URL with its optional filter. It is safe to call while Run
	// is executing; URLs already present are not queued again.
	Add(url string, filter Filter)
	// Run visits every pending URL until all of them reach a terminal state.
	// Failed URLs do not produce an error: inspect States for details.
	Run(ctx context.Context) error
	// States returns a snapshot of every tracked URL.
	States() map[string]URLState
	// State returns the state of a single URL.
	State(url string) (URLState, bool)
	// Progress returns a snapshot of the queue counters.
	Progress() Progress
}

type fetchResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Err        error
}

type fetchFunc func(ctx context.Context, url string) fetchResult

type base struct {
	mu      sync.Mutex
	policy  Policy
	current string
	states  map[string]*URLState
	order   []string
}

func (b *base) add(url string, filter Filter) {
	if url == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.states[url]; exists {
		return
	}

	b.states[url] = &URLState{URL: url, Filter: filter}
	b.order = append(b.order, url)
}

func (b *base) statesSnapshot() map[string]URLState {
	b.mu.Lock()
	defer b.mu.Unlock()

	snapshot := make(map[string]URLState, len(b.states))
	for url, state := range b.states {
		snapshot[url] = *state
	}
	return snapshot
}

func (b *base) state(url string) (URLState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, exists := b.states[url]
	if !exists {
		return URLState{}, false
	}
	return *state, true
}

func (b *base) nextURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, url := range b.order {
		if state := b.states[url]; state != nil && !state.Terminal() {
			return url
		}
	}
	return ""
}

func (b *base) beginAttempt(url string) (int, Filter, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.states[url]
	if state == nil || state.Terminal() {
		return 0, nil, false
	}

	state.Visited = true
	state.Attempts++
	b.current = url

	return state.Attempts, state.Filter, true
}

func (b *base) markCompleted(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state := b.states[url]; state != nil {
		state.Scraped = true
		state.Completed = true
	}
}

func (b *base) markNotFound(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state := b.states[url]; state != nil {
		state.NotFound = true
	}
}

func (b *base) markInvalid(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state := b.states[url]; state != nil {
		state.Scraped = true
		state.Invalid = true
	}
}

func (b *base) markFailed(url, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state := b.states[url]; state != nil {
		state.Failed = true
		state.Error = reason
	}
}

func (b *base) run(ctx context.Context, fetch fetchFunc) error {
	if !stdoutIsTerminal() {
		return b.runQueue(ctx, fetch)
	}

	return b.runWithProgress(ctx, fetch)
}

func (b *base) runQueue(ctx context.Context, fetch fetchFunc) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		url := b.nextURL()
		if url == "" {
			return nil
		}

		if !b.process(ctx, url, fetch) {
			return ctx.Err()
		}
	}
}

func (b *base) runWithProgress(ctx context.Context, fetch fetchFunc) error {
	start := time.Now()

	fmt.Print("\x1b[?25l") // hide cursor

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		last := b.progress()
		b.showProgress(last, start)
		lastRender := time.Now()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				progress := b.progress()
				now := time.Now()

				// Redraw when progress changes, or once per second so the
				// duration does not look frozen during a slow request.
				if progress == last && now.Sub(lastRender) < time.Second {
					continue
				}

				last = progress
				lastRender = now
				b.showProgress(progress, start)
			case <-stop:
				if progress := b.progress(); progress != last {
					b.showProgress(progress, start)
				}
				return
			}
		}
	}()

	defer func() {
		close(stop)
		<-done
		fmt.Print("\x1b[?25h\n") // show cursor
	}()

	return b.runQueue(ctx, fetch)
}

func (b *base) process(ctx context.Context, url string, fetch fetchFunc) bool {
	for {
		if err := ctx.Err(); err != nil {
			return false
		}

		attempt, filter, ok := b.beginAttempt(url)
		if !ok {
			return true
		}

		result := fetch(ctx, url)

		if ctx.Err() != nil {
			return false
		}

		if result.Err == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
			if filter != nil && filter(Response{
				URL:        url,
				StatusCode: result.StatusCode,
				Header:     result.Header,
				Body:       result.Body,
			}) {
				b.markInvalid(url)
				return true
			}

			b.markCompleted(url)
			return true
		}

		if result.StatusCode == http.StatusNotFound {
			b.markNotFound(url)
			return true
		}

		var retryAfter time.Duration
		if result.Header != nil {
			retryAfter = ParseRetryAfter(result.Header.Get("Retry-After"))
		}

		if !IsRetryableStatus(result.StatusCode) || attempt >= b.policy.Attempts(result.StatusCode) {
			b.markFailed(url, failureReason(result))
			return true
		}

		if !sleep(ctx, b.policy.Wait(result.StatusCode, attempt, retryAfter)) {
			return false
		}
	}
}

func failureReason(result fetchResult) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	return fmt.Sprintf("unexpected status %d", result.StatusCode)
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
