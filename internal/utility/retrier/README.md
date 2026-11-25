# internal/utility/retrier

Small URL queue with retries. This package is technical infrastructure: it has
no catalog or business rules and does not know about storage.

## Creating a retrier

```go
queue := retrier.NewHTTP(client)
queue := retrier.NewColly(collector)
queue := retrier.NewChromedp(
    retrier.WithExecPath(browserPath),
    retrier.WithHeadless(false),
)
```

Options:

- `WithPolicy`: replace the retry policy;
- `WithProvider`, `WithMode`: tag the run for persistence;
- `WithObserver`: attach `RunObserver` (implemented by the retrier persistence
  use case);
- `WithPresenter`: progress rendering;
- `WithRunID`: reuse a persisted run ID when resuming;
- `WithUserAgent`, `WithExecPath`, `WithHeadless`, `WithTimeout`,
  `WithChallengeTimeout`.

## Queue API

- `Add(url, filter)`: append a URL;
- `Restore(states, order, resolver)`: rebuild a persisted queue;
- `Fetch(ctx, url)`: fetch one URL with retries, without the queue;
- `Run(ctx)`: process pending URLs;
- `States()`, `Progress()`: snapshots.

A `Filter` returns `true` when a successful response is invalid. Filters can
also parse the response and add more URLs to the queue.

## Observer

`RunObserver` receives:

```go
RunStarted(ctx, run) (runID, error)
RunStateChanged(ctx, runID, state) error
RunFinished(ctx, run) error
```

The retrier persistence use case implements it to save the JSON document and
support resuming.

## Transports

- `NewHTTP`: `net/http` with retries;
- `NewColly`: Colly callbacks, including HTML callbacks from the provider;
- `NewChromedp`: real browser for Cloudflare-protected sites.

`RejectSelector` and `RejectWhen` build response filters.

## Tests

```powershell
go test ./internal/utility/retrier
```
