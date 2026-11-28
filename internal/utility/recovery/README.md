# internal/utility/recovery

Maintenance utility that rebuilds the YGO provider snapshot from the catalog
already stored in PostgreSQL. It is used when `data/retrier.json` is lost but
the catalog rows are still available.

## What it does

1. Loads every global `SET` and `CARD` from `items` (`store_id IS NULL`).
2. Groups cards by `set_id`.
3. Builds a complete `releasesCheckpoint` in the current format.
4. Writes it as the provider snapshot of an `interrupted` YGO `all` run. The
   JSON store keeps the snapshot in the run sidecar under
   `data/retrier.json.d/`.

Cards whose set is missing are counted as orphans and are not included.
Store-owned rows are excluded.

## API

```go
type ItemSource interface {
    Load(ctx context.Context, kinds ...domain.Kind) ([]domain.Item, error)
}

func BuildSnapshot(items []domain.Item) (json.RawMessage, Stats, error)

func Run(ctx context.Context, source ItemSource, path string) (Stats, error)
```

`cmd/recover` passes a `catalogpg.Repository`, whose `Load` returns only global
rows. `Stats` reports items, sets, cards, orphan cards, bytes and the output
path.

## Running

Use the [`cmd/recover`](../../../cmd/recover/README.md) command. It requires
PostgreSQL on `localhost:5433` (or `DB_URL`).
