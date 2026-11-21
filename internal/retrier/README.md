# retrier

Generic, dynamic URL retry queue with three transports:

| Transport | Type | Use case |
| --- | --- | --- |
| `HTTP` | `*retrier.HTTP` | Plain HTTP and JSON APIs. |
| `Colly` | `*retrier.Colly` | Colly callbacks and HTML scraping. |
| `Chromedp` | `*retrier.Chromedp` | Real browser for JS challenges. |

All transports share the same queue, retry policy, filters, state tracking and
console progress.

## Quick start

```go
client := retrier.NewHTTP(nil)
client.Add("https://example.com", nil)

if err := client.Run(ctx); err != nil {
    return err
}

for pageURL, state := range client.States() {
    if state.Failed {
        log.Printf("%s failed after %d attempts: %s", pageURL, state.Attempts, state.Error)
    }
}
```

`Add` is safe while `Run` is executing. The queue is dynamic: a filter or
callback can add more URLs as pages are discovered.

`Run` returns only context errors. URLs that fail after exhausting their retries
are marked as failed but do not make `Run` return an error. Inspect `States()` or
`State(url)` for details.

## `Retrier` interface

```go
type Retrier interface {
    Add(url string, filter Filter)
    Run(ctx context.Context) error
    Continue(ctx context.Context, reportPath string, resolve FilterResolver) error
    States() map[string]URLState
    State(url string) (URLState, bool)
    Progress() Progress
    LastReport() string
}
```

## Run reports

Enable reports with `WithProvider`:

```go
client := retrier.NewHTTP(
    nil,
    retrier.WithProvider("ygo"),
    retrier.WithReportDir("reports"), // optional
)
```

Every `Run` writes:

```text
reports/<provider>_<state>_<UTC timestamp>.json
```

States:

- `success`: `Run` finished without an error.
- `cancelled`: the context was cancelled.
- `failed`: any other error, including a deadline.

The report contains `Progress`, `Order`, every `URLState` and the failed,
not-found and invalid URLs. `LastReport()` returns the path of the last report.

To write a report even when the browser cannot start, `Chromedp` registers the
report after `Run` starts; use `WithChromedpProvider("dragonshield")` to enable
it for the browser transport. `WithChromedpReportDir` changes its directory.

## Continue from a report

```go
err := client.Continue(
    ctx,
    "reports/ygo_cancelled_20261120T145510Z.json",
    func(state retrier.URLState) retrier.Filter {
        switch {
        case strings.Contains(state.URL, "/wiki/Card_Gallery:"):
            return galleryFilter
        case strings.Contains(state.URL, "/wiki/Set_Card_Lists:"):
            return setFilter
        default:
            return cardFilter
        }
    },
)
```

`Continue` restores the queue order and states, skips terminal URLs and calls
`resolve` only for non-terminal URLs. Filters are functions and cannot be
serialized, so the caller must return them here. If `resolve` returns `nil`, the
URL is resumed without a filter.

`Continue` returns `ErrQueueNotEmpty` when the retrier already has queued URLs.
It does not reconstruct provider-specific in-memory data; callers that keep
extracted data, such as the YGO checkpoint, must restore that data separately.

## URLs and filters

A `Filter` receives a successful response and reports whether it is invalid:

```go
type Filter func(Response) bool
```

- Returning `true` marks the URL as `Invalid` and stops retrying it.
- Returning `false` keeps the URL valid.
- Filters run only for HTTP 2xx responses.
- `Response` contains `URL`, `StatusCode`, `Header` and `Body`.

Helpers:

```go
retrier.RejectSelector(".noarticletext")
retrier.RejectWhen(func(response retrier.Response) bool {
    return len(response.Body) == 0
})
```

Providers also use filters as response handlers: parse the body and return
`false` to keep the URL valid.

## State

```go
type URLState struct {
    URL       string
    Visited   bool
    Scraped   bool
    Completed bool
    NotFound  bool
    Invalid   bool
    Failed    bool
    Attempts  int
    Error     string
}
```

A URL is terminal when it is `Completed`, `NotFound`, `Invalid` or `Failed`.

## Progress

```go
type Progress struct {
    Total          int
    Done           int
    Pending        int
    Completed      int
    NotFound       int
    Invalid        int
    Failed         int
    Current        string
    CurrentAttempt int
}
```

`Progress()` returns a snapshot of the queue. `Current` is the URL being
attempted or the last one attempted, and `CurrentAttempt` is its attempt number.

When `stdout` is a terminal, `Run` also renders an in-place dashboard:

- clears the terminal;
- hides the cursor while running and restores it at the end;
- redraws when the progress changes;
- refreshes the duration once per second;
- is disabled automatically when stdout is redirected or captured by tests.

The dashboard is implemented by `showProgress` in `progress.go`.

## Retry policy

`DefaultPolicy` is used by all transports:

| Field | Default |
| --- | --- |
| `MaxAttempts` | 3 |
| `TooManyAttempts` | 3 |
| `BlockedAttempts` | 3 |
| `RetryDelay` | 2s |
| `TooManyBaseDelay` | 5s |
| `BlockedBaseDelay` | 30s |
| `MaxDelay` | 2m |

The policy applies exponential backoff and honors `Retry-After`. Retryable
statuses include network/timeout errors, 408, 425, 429, 401/403, the standard
5xx gateway errors (500, 502, 503, 504) and the Cloudflare origin errors
520-527 and 530. A 404 is terminal and is never retried.

```go
policy := retrier.DefaultPolicy
policy.MaxAttempts = 5

client := retrier.NewHTTP(nil, retrier.WithPolicy(policy))
```

`WithPolicy` is available for `HTTP` and `Colly`. `Chromedp` currently uses the
default policy.

## HTTP transport

```go
client := retrier.NewHTTP(&http.Client{Timeout: 30 * time.Second})
```

Passing `nil` uses a client with a 30-second timeout.

## Colly transport

```go
collector := colly.NewCollector(
    colly.AllowedDomains("example.com", "www.example.com"),
)
collector.OnHTML("body", func(element *colly.HTMLElement) {
    // parse element.DOM
})

client := retrier.NewColly(collector)
```

`NewColly` enables `AllowURLRevisit` and `ParseHTTPErrorResponse` so the retrier
can repeat URLs and inspect error responses.

## Chromedp transport

```go
browser := retrier.NewChromedp()
browser.Add(
    "https://www.dragonshield.com/collections/card-sleeves",
    func(response retrier.Response) bool {
        // response.Body is the rendered HTML
        return false
    },
)

if err := browser.Run(ctx); err != nil {
    return err
}
```

`Chromedp` is intended for JavaScript challenges such as Cloudflare. It:

- starts a real browser on the first `Run`;
- uses a temporary user-data directory, so it does not touch the user's browser
  profile;
- runs with a visible window by default, because Cloudflare often challenges
  headless browsers;
- navigates with `chromedp.NavigateResponse`, waits until Cloudflare challenge
  markers disappear, then returns the rendered HTML;
- closes the browser when `Run` ends.

Browser selection:

- `DRAGONSHIELD_BROWSER` environment variable, when set;
- otherwise Brave, Chrome or Edge is detected automatically;
- `retrier.DefaultBrowserPath()` returns the detected path, or an empty string.

Options:

```go
browser := retrier.NewChromedp(
    retrier.WithChromedpExecPath(`C:\path\to\brave.exe`),
    retrier.WithChromedpHeadless(false),
    retrier.WithChromedpTimeout(3*time.Minute),
    retrier.WithChromedpChallengeTimeout(2*time.Minute),
)
```

If a challenge requires interaction, the visible window lets the user solve it.
The retrier waits up to the challenge timeout.

## Tests

Unit tests:

```powershell
go test ./internal/retrier
go test ./internal/retrier -short
```

Live browser test, opt-in:

```powershell
$env:DRAGONSHIELD_LIVE="1"
go test ./internal/retrier `
  -run TestChromedpNavigatesDragonShield -v `
  -count=1 -timeout 5m
```

The live test opens the detected browser and waits for the Dragon Shield
collection page.
