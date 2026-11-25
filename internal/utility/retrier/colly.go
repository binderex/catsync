package retrier

import (
	"context"
	"net/http"

	"github.com/gocolly/colly/v2"

	"github.com/binderex/catsync/internal/domain"
)

const collyStateKey = "retrier-response"

type collyTransport struct {
	collector *colly.Collector
}

// NewColly creates a retrier backed by a Colly collector. A nil collector uses
// a default one. The collector is configured to revisit URLs and parse HTTP
// error responses; provider callbacks such as OnHTML keep working.
func NewColly(collector *colly.Collector, opts ...Option) *Retrier {
	if collector == nil {
		collector = colly.NewCollector()
	}
	collector.AllowURLRevisit = true
	collector.ParseHTTPErrorResponse = true

	r := newRetrier(domain.DefaultPolicy, opts...)
	transport := &collyTransport{collector: collector}
	transport.registerCallbacks()
	r.fetch = transport.fetch
	return r
}

func (t *collyTransport) fetch(ctx context.Context, url string) (domain.Response, error) {
	if err := ctx.Err(); err != nil {
		return domain.Response{URL: url}, err
	}

	result := &collyResult{}
	requestCtx := colly.NewContext()
	requestCtx.Put(collyStateKey, result)

	err := t.collector.Request(http.MethodGet, url, nil, requestCtx, nil)
	if result.err == nil {
		result.err = err
	}

	return domain.Response{
		URL:        url,
		StatusCode: result.status,
		Header:     result.header,
		Body:       result.body,
	}, result.err
}

type collyResult struct {
	status int
	header http.Header
	body   []byte
	err    error
}

func (t *collyTransport) registerCallbacks() {
	t.collector.OnResponse(func(response *colly.Response) {
		result := collyResultFrom(response.Ctx)
		if result == nil {
			return
		}

		result.status = response.StatusCode
		if response.Headers != nil {
			result.header = *response.Headers
		}
		result.body = response.Body
	})

	t.collector.OnError(func(response *colly.Response, err error) {
		if response == nil {
			return
		}

		result := collyResultFrom(response.Ctx)
		if result == nil {
			return
		}

		result.err = err
		result.status = response.StatusCode
		if response.Headers != nil {
			result.header = *response.Headers
		}
		result.body = response.Body
	})
}

func collyResultFrom(ctx *colly.Context) *collyResult {
	if ctx == nil {
		return nil
	}

	result, _ := ctx.GetAny(collyStateKey).(*collyResult)
	return result
}
