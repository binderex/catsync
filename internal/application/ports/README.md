# internal/application/ports

Interfaces implemented by infrastructure adapters.

## Catalog

```go
type CatalogRepository interface {
    Upsert(ctx context.Context, items []domain.Item) error
    Delete(ctx context.Context, ids []string) error
}
```

`Delete` removes global sets that had cards but no regular card left. Store-owned
rows are not touched.

## Catalog provider

```go
type CatalogProvider interface {
    Name() string
    GetNewReleases(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error)
    GetAllReleases(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error)
}
```

`Name()` is used to filter retrier runs by provider.

## Retrier

- `RetrierRunRepository`: `Save`, `Load`, `List`, `Delete` for run snapshots.
  The concrete adapter is injected; the use case never selects one with a
  switch.
- `RetrierPersistence`: the domain-facing persistence plus `RunStarted`,
  `RunStateChanged` and `RunFinished`, so it can be attached to the retrier
  utility as its observer. `SaveProviderData` attaches the provider snapshot
  (for example the parsed YGO sets and prints) to the active run.

## Rules

- Ports stay small and transport-free.
- Implementations live under `internal/infrastructure`.
- Compile-time assertions in adapters keep the contract in sync.
