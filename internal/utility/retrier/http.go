package retrier

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/binderex/catsync/internal/domain"
)

type httpTransport struct {
	client    *http.Client
	userAgent string
}

// NewHTTP creates a retrier backed by a net/http client. A nil client uses a
// 30 second timeout.
func NewHTTP(client *http.Client, opts ...Option) *Retrier {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	r := newRetrier(domain.DefaultPolicy, opts...)
	transport := &httpTransport{client: client, userAgent: r.userAgent}
	r.fetch = transport.fetch
	return r
}

func (t *httpTransport) fetch(ctx context.Context, url string) (domain.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Response{URL: url}, err
	}
	if t.userAgent != "" {
		request.Header.Set("User-Agent", t.userAgent)
	}

	response, err := t.client.Do(request)
	if err != nil {
		return domain.Response{URL: url}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return domain.Response{
			URL:        url,
			StatusCode: response.StatusCode,
			Header:     response.Header,
		}, err
	}

	return domain.Response{
		URL:        url,
		StatusCode: response.StatusCode,
		Header:     response.Header,
		Body:       body,
	}, nil
}
