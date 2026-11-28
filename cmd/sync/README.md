# cmd/sync

Single synchronization entry point for Catsync.

```powershell
go run ./cmd/sync -catprov ygo -mode new
go run ./cmd/sync -catprov ygo -mode all
go run ./cmd/sync -catprov dragonshield -mode all
```

## Files

| File | Responsibility |
| --- | --- |
| `main.go` | Wiring: flags, use cases, provider selection and execution. |
| `config.go` | DSN, retrier path/interval, medium parsing and environment helpers. |

## Flags

- `-catprov`: `ygo` or `dragonshield`. Required.
- `-mode`: `new` or `all`. Default: `new`.
- `-resume`: resume the latest unfinished run. Default: `true`.
  Use `-resume=false` for a fresh crawl.
- `-mediums`: comma-separated YGO mediums. Default: `TCG,OCG`.
- `-rush-duel`: include Rush Duel. Default: `false` until an official English
  release exists.
- `-retrier-json`: retrier document. Default: `data/retrier.json`.

## Configuration

The command loads `.env` from the project directory.

- `DB_URL` (or `CATSYNC_DB_URL`): PostgreSQL DSN.
- When `DB_URL` is missing, the DSN is built from `POSTGRES_HOST`,
  `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB`.
- `RETRIER_JSON_PATH`: overrides `-retrier-json`.
- `RETRIER_SAVE_INTERVAL`: seconds or Go duration between intermediate saves.
  Default: `10s`.
- `DRAGONSHIELD_BROWSER`: optional browser executable.

PostgreSQL is available on port `5433`.

## State files

- `data/retrier.json`: retrier run state in the current `domain.RetrierRun`
  format (`version: 2`). It contains the URL progress.
- `data/retrier.json.d/<run>.json`: sidecar with the parsed provider snapshot
  for that run. Keeping it separate prevents periodic URL saves from rewriting
  the full catalog snapshot.
- `data/ygo-crawl-report.json`: summary of the last YGO crawl.

Both `new` and `all` save their snapshots. A snapshot contains only complete
sets and complete card prints; pending prints are kept as resume data. The run
is left `interrupted` when pages are pending so the next execution continues it.
`-resume=false` starts a new run. The `data/` folder is git-ignored.

## Validation

```powershell
go build ./...
go vet ./...
go test ./... -short
```
