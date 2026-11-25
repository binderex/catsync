# internal/utility/recovery

Maintenance utility that rebuilds the YGO provider snapshot from the catalog
already stored in PostgreSQL. It is used when `data/retrier.json` is lost but
the catalog rows are still available.

## What it does

1. Loads every `SET` and `CARD` from `items`.
2. Groups cards by `set_id`.
3. Builds a complete `releasesCheckpoint` in the current format.
4. Writes it as `provider_data` of an `interrupted` YGO `all` run in the
   retrier JSON document.

Cards whose set is missing are counted as orphans and are not included.

## API

```go
func BuildSnapshot(items []domain.Item) (json.RawMessage, Stats, error)

func Run(ctx context.Context, db *gorm.DB, path string) (Stats, error)
```

`Stats` reports items, sets, cards, orphan cards, bytes and the output path.

## Running

Use the [`cmd/recover`](../../../cmd/recover/README.md) command. It requires
PostgreSQL on `localhost:5433` (or `DB_URL`).
