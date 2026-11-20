# cmd/sync

Catsync synchronization commands.

- `cmd/sync/dragonshield` synchronizes Dragon Shield sleeves.
- `cmd/sync/ygo` synchronizes Yu-Gi-Oh! sets and card prints.

Both commands:

- load `.env` when present;
- require `DB_URL`;
- run database migrations when connecting;
- persist through `catalog.Service`;
- stop on `Ctrl+C`.

YGO also persists partial results after cancellation. Dragon Shield only
upserts after collecting the complete sleeve list.

Run the commands from the repository root so checkpoints and reports use the
expected paths.

## Requirements

- Go 1.27.3 or newer.
- PostgreSQL reachable through `DB_URL`.
- Brave, Chrome or Edge for Dragon Shield.

Example `.env`:

```dotenv
DB_URL=postgres://user:password@localhost:5432/catsync?sslmode=disable

# Optional for Dragon Shield:
# DRAGONSHIELD_BROWSER=C:\path\to\brave.exe
```

## Dragon Shield

```powershell
go run ./cmd/sync/dragonshield
```

The command has no flags.

It opens a visible browser, follows the card sleeve collection and its
pagination, and upserts every product variant. A Cloudflare challenge may need
to be solved manually in the browser window.

The retrier writes a report to:

```text
reports/dragonshield_<state>_<date>.json
```

The browser is detected automatically. To use a specific browser:

```powershell
$env:DRAGONSHIELD_BROWSER = "C:\path\to\brave.exe"
go run ./cmd/sync/dragonshield
```

## Yu-Gi-Oh

### Modes

```powershell
# Newly announced sets, from the Yugipedia main page (default)
go run ./cmd/sync/ygo

# Same mode, explicitly
go run ./cmd/sync/ygo -mode new

# Every set in Category:All sets
go run ./cmd/sync/ygo -mode all

# Continue a full crawl from its checkpoint
go run ./cmd/sync/ygo -mode all -resume
```

Flags:

- `-mode`: `new` or `all`. Default: `new`.
- `-resume`: continues from `ygo-releases-state.json`.
  Only valid with `-mode all`.
- `-mediums`: comma-separated list. Empty means all.

### `new` mode

- Scrapes the sets announced on the Yugipedia main page.
- Does not use a checkpoint.
- Each run starts over. A later run can fill pages that failed before.

### `all` mode

- Walks every set in `Category:All sets`.
- Can take a long time.
- Writes progress to:

```text
ygo-releases-state.json
```

`-resume` continues pending pages and reuses extracted sets. Without it, the
command removes the old checkpoint and starts from scratch.

### Filtering by medium

```powershell
go run ./cmd/sync/ygo -mediums "TCG,OCG"
go run ./cmd/sync/ygo -mode all -mediums "Rush Duel"
```

### Reports

Every YGO run writes:

- `ygo-crawl-report.json`: catalog summary with sets, prints and failures.
- `reports/ygo_<state>_<date>.json`: retrier queue report.

Retrier states are `success`, `cancelled` or `failed`.

`ygo-crawl-report.json` is overwritten on each run. Retrier reports use a new
UTC timestamp.

### Persistence

- `ItemInfo` rows are fetched by brand and upserted in batches.
- Sets use `UpsertSetWithPrints`, which resolves the persisted set ID before
  linking prints.
- Upserts are designed to update existing rows without duplicates.
- On cancellation, YGO persists partial sets and saves the checkpoint in
  `all` mode.

## Verification

```powershell
go build ./...
go test ./... -short -count=1
go vet ./...
```

Live provider test:

```powershell
go test ./internal/catprov/ygo \
  -run TestGetNewReleasesLive -v -count=1 -timeout 15m
```

Live tests are not part of `-short`.
