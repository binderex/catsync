package retrier

import (
	"context"
	"net/http"
	"time"

	"github.com/gocolly/colly/v2"
)

const collyStateKey = "retrier-response"

// Colly retries URLs with a Colly collector.
type Colly struct {
	base
	collector *colly.Collector
}

// NewColly returns a Colly retrier. A nil collector uses a default one.
//
// The constructor enables URL revisits and HTTP status parsing on the
// collector, which are required to retry the same URL and to inspect error
// responses.
func NewColly(collector *colly.Collector, opts ...Option) *Colly {
	if collector == nil {
		collector = colly.NewCollector()
	}

	collector.AllowURLRevisit = true
	collector.ParseHTTPErrorResponse = true

	retrier := &Colly{
		base:      newBase(opts...),
		collector: collector,
	}
	retrier.registerCallbacks()

	return retrier
}

// Add queues a URL with its optional filter.
func (c *Colly) Add(url string, filter Filter) {
	c.add(url, filter)
}

// Run visits every pending URL until all of them reach a terminal state.
func (c *Colly) Run(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() {
		err = c.finishRun(started, ctx, err)
	}()

	return c.run(ctx, c.fetch)
}

// Continue loads a report and resumes its pending URLs.
func (c *Colly) Continue(ctx context.Context, reportPath string, resolve FilterResolver) error {
	if err := c.continueFromReport(reportPath, resolve); err != nil {
		return err
	}
	return c.Run(ctx)
}

// States returns a snapshot of every tracked URL.
func (c *Colly) States() map[string]URLState {
	return c.statesSnapshot()
}

// State returns the state of a single URL.
func (c *Colly) State(url string) (URLState, bool) {
	return c.state(url)
}

// Progress returns a snapshot of the queue counters.
func (c *Colly) Progress() Progress {
	return c.progress()
}

type collyResult struct {
	status int
	header http.Header
	body   []byte
	err    error
}

func (c *Colly) registerCallbacks() {
	c.collector.OnResponse(func(r *colly.Response) {
		result := collyResultFrom(r.Ctx)
		if result == nil {
			return
		}

		result.status = r.StatusCode
		if r.Headers != nil {
			result.header = *r.Headers
		}
		result.body = r.Body
	})

	c.collector.OnError(func(r *colly.Response, err error) {
		result := collyResultFrom(r.Ctx)
		if result == nil {
			return
		}

		result.err = err
		if r != nil {
			result.status = r.StatusCode
			if r.Headers != nil {
				result.header = *r.Headers
			}
			result.body = r.Body
		}
	})
}

func collyResultFrom(ctx *colly.Context) *collyResult {
	if ctx == nil {
		return nil
	}

	result, _ := ctx.GetAny(collyStateKey).(*collyResult)
	return result
}

func (c *Colly) fetch(ctx context.Context, url string) fetchResult {
	if err := ctx.Err(); err != nil {
		return fetchResult{Err: err}
	}

	result := &collyResult{}
	requestCtx := colly.NewContext()
	requestCtx.Put(collyStateKey, result)

	err := c.collector.Request(http.MethodGet, url, nil, requestCtx, nil)
	if result.err == nil {
		result.err = err
	}

	return fetchResult{
		StatusCode: result.status,
		Header:     result.header,
		Body:       result.body,
		Err:        result.err,
	}
}
