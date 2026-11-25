# cmd/recover

Rebuilds the YGO provider snapshot from the catalog already stored in
PostgreSQL. Use it when `data/retrier.json` is lost but the extracted data is
still in the database.

```powershell
go run ./cmd/recover
go run ./cmd/recover -output data/retrier.json
```

## Requirements

- `DB_URL` in the environment or `.env`, or the local default
  `postgres://binderex:binderex@localhost:5433/binderex?sslmode=disable`.
- The `items` table populated.

The command writes an `interrupted` YGO `all` run with `provider_data` set, so
a later `go run ./cmd/sync -catprov ygo -mode all` reuses the snapshot without
scraping.

## Notes

- The catalog rows are the source of truth. Cards whose set is missing are
  reported as orphans and skipped.
- The retrier document is git-ignored.
- See [`internal/utility/recovery`](../../internal/utility/recovery/README.md)
  for the implementation.
