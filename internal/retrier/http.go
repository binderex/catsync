package retrier

import (
	"context"
	"io"
	"net/http"
	"time"
)

// HTTP retries URLs with a net/http client.
type HTTP struct {
	base
	client *http.Client
}

// NewHTTP returns an HTTP retrier. A nil client uses a client with a 30 second
// timeout.
func NewHTTP(client *http.Client, opts ...Option) *HTTP {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &HTTP{
		base:   newBase(opts...),
		client: client,
	}
}

// Add queues a URL with its optional filter.
func (h *HTTP) Add(url string, filter Filter) {
	h.add(url, filter)
}

// Run visits every pending URL until all of them reach a terminal state.
func (h *HTTP) Run(ctx context.Context) error {
	return h.run(ctx, h.fetch)
}

// States returns a snapshot of every tracked URL.
func (h *HTTP) States() map[string]URLState {
	return h.statesSnapshot()
}

// State returns the state of a single URL.
func (h *HTTP) State(url string) (URLState, bool) {
	return h.state(url)
}

// Progress returns a snapshot of the queue counters.
func (h *HTTP) Progress() Progress {
	return h.progress()
}

func (h *HTTP) fetch(ctx context.Context, url string) fetchResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fetchResult{Err: err}
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fetchResult{Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fetchResult{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Err:        err,
		}
	}

	return fetchResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}
}
