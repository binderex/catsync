# AGENTS.md

Guide for coding agents working on **Catsync**.

## What this project is

Catsync is the catalog synchronization service for **Binderex**. It fetches
trading-card catalog data from external providers, normalizes it into the local
catalog model, and persists it in PostgreSQL.

- Module: `github.com/binderex/catsync`
- Go: `1.27.3`
- Platform in use: Windows
- Database: PostgreSQL, configured with `DB_URL`
- Environment loader: `godotenv` (`.env` is git-ignored)

## Current repository state

The project is a **first version under active development**. The catalog uses
the single denormalized `domain.Item` entity and a PostgreSQL `itemModel`; the
retrier is a JSON-backed utility. The active code has no dependency on any
previous data format.

## Layout

| Path | Responsibility |
| --- | --- |
| `cmd/sync` | Single synchronization entry point; `-catprov` selects Dragon Shield or Yu-Gi-Oh!. |
| `cmd/recover` | Rebuilds the YGO snapshot from PostgreSQL when retrier persistence is lost. |
| `internal/utility/retrier` | Retrier utility (HTTP, Colly, Chromedp, progress, filters). No business rules. |
| `internal/utility/recovery` | Rebuilds the YGO provider snapshot from PostgreSQL. |
| `internal/domain` | Retrier and catalog domain types, identity and deduplication; no infrastructure imports. |
| `internal/application` | Ports and use cases for catalog providers and retrier persistence. |
| `internal/infrastructure/catprov` | Dragon Shield and Yu-Gi-Oh! providers returning `domain.Item`. |
| `internal/infrastructure/persistence/json` | Single-file retrier store. |
| `internal/infrastructure/persistence/sql/postgresql/catalog` | GORM `itemModel`, denormalized catalog upsert, cleanup and PostgreSQL migration. |

Provider docs:

- [`internal/infrastructure/catprov/dragonshield/README.md`](internal/infrastructure/catprov/dragonshield/README.md)
- [`internal/infrastructure/catprov/ygo/README.md`](internal/infrastructure/catprov/ygo/README.md)
- [`internal/utility/retrier/README.md`](internal/utility/retrier/README.md)

## Commands

Regular validation:

```powershell
go build ./...
go vet ./...
go test ./... -short
```

Run the synchronizations:

```powershell
go run ./cmd/sync -catprov ygo -mode new
go run ./cmd/sync -catprov ygo -mode all
go run ./cmd/sync -catprov dragonshield -mode all
```

Rebuild the snapshot if the retrier document is lost:

```powershell
go run ./cmd/recover
```

`-resume` defaults to `true`; `-resume=false` starts a new retrier run. Both
`all` and `new` store their parsed provider snapshot in retrier persistence.
`Sets` contains only complete sets and complete card prints; a card whose card
page or gallery is still pending lives in `PendingPrints` and stays in the
retrier queue for the next execution. Every YGO crawl writes
`data/ygo-crawl-report.json` with counts, set/print totals and the failed URLs.

`-mode new` scrapes the releases announced on the Yugipedia main page. It saves
and resumes its own checkpoint just like `all`. When the crawl completes, it
merges the scraped sets, prints and releases into the newest complete `all`
snapshot using upsert by identity; partial crawls do not update it. New releases
discovered while resuming are added to the pending queue. `-mode all` falls back
to the complete snapshot only when there is no unfinished run with provider
data.

A failed, canceled or incomplete crawl is never upserted, for either `new` or
`all`. No database write happens on cancellation or during the crawl. Progress
stays in retrier persistence, an incomplete run is marked `interrupted` so the
next execution resumes it, and only a complete crawl writes to PostgreSQL.

The retrier state is the `data/retrier.json` document inside the catsync
project. Large provider snapshots are stored in sidecar files under
`data/retrier.json.d/` so URL-progress saves never rewrite the full catalog
snapshot. The path can be changed with `-retrier-json` / `RETRIER_JSON_PATH`.
PostgreSQL is the local container from the parent
`C:\Users\natan\Proyectos\Binderex\compose.yaml` on port `5433`.
`DB_URL` must be set in the environment or in `.env`.

Live browser tests are opt-in. They open a real browser and are not part of the
default test run:

```powershell
$env:DRAGONSHIELD_LIVE="1"

go test ./internal/infrastructure/catprov/dragonshield -run TestGetSleevesLive -v -count=1 -timeout 12m
go test ./internal/infrastructure/catprov/ygo -run TestGetNewReleasesLive -v -count=1 -timeout 15m
go test ./internal/infrastructure/catprov/ygo -run TestFetchMediawikiAllSetsLive -v -count=1 -timeout 10m
```

`-short` skips the live tests.

## Dragon Shield provider rules

- Dragon Shield is protected by Cloudflare. Keep the `Chromedp` transport for
  this provider; plain HTTP and Colly receive challenge pages or 403/429
  responses.
- `Chromedp` starts a real browser with a temporary profile. The default is a
  **visible** window, because headless browsers are often challenged.
- The browser is detected automatically. Override it with:

  ```powershell
  $env:DRAGONSHIELD_BROWSER="C:\path\to\brave.exe"
  ```

- The collection grid is rendered by JavaScript with skeleton cards. Product
  URLs come from two sources:
  1. `.js-collection-grid a[href]` when the grid has hydrated.
  2. The embedded `var meta = {"products": [...]}` object as a fallback.
     This is the normal path while the grid shows skeletons.
- One catalog item is created per product variant.
- The item code is the variant SKU, not only the visible product number.
- Variant attributes live in `properties["variant"]`: `color`, `size`,
  `texture`, `glare_type`, `quality`, `items_per_case`, `material`, `layer`,
  and `clear` when applicable.
- The main product image is taken from `.js-product-gallery-image img` and
  rendered at `width=480` and `width=800`.

## Database rules

- The active catalog path is `catalogpg.Open(dsn)`, which calls
  `catalogpg.Migrate` and creates the denormalized `items` table.
- `domain.Item` is the only catalog entity and keeps the JSON tags used by the
  retrier snapshot. It has no GORM tags.
- The PostgreSQL adapter owns `itemModel`, the GORM model for `items`. Its
  fields use explicit persistence types (`uuid.UUID`, `string`) and GORM tags.
- `domain.Item.ID`, `SetID` and `StoreID` are strings. `itemModel` converts them
  to UUIDs and rejects invalid values before writing.
- `items.store_id` is nullable: `NULL` is a global catalog item (the only rows
  Catsync writes); non-null is a store-owned item. The migration seeds a minimal
  `stores` table (`stores.id`, snake_case) and adds the FK.
- The identity index starts with `store_id` and uses `NULLS NOT DISTINCT`, so
  global rows are unique among themselves and each store has its own scope.
- Catsync only reads and writes global rows: `Load`, `loadSetIDs`, `Delete`,
  source verification and migration cleanup filter `store_id IS NULL`.
- `items_source_not_empty` is `CHECK (store_id IS NOT NULL OR source <> '')`.
- There is no `user_id`; non-identity data lives in `Properties`.
- The identity index is
  `(store_id, kind, brand, lang, code, set_id, rarity, edition, extended_art, source, variant_key)`
  with `NULLS NOT DISTINCT`. `variant_key` is a generated column.
- `source` is part of the identity. Global rows cannot have an empty source;
  `CatalogUseCase.Upsert` rejects empty sources and the migration adds the
  `items_source_not_empty` check constraint.
- `CatalogUseCase.Upsert` fills IDs/defaults and calls
  `domain.DeduplicateItems` before the repository; the repository expects
  unique, normalized items. It rejects items with `StoreID` set, because
  Catsync only writes global rows.
- Items named `unknown` are placeholders: the use case drops them and the
  migration deletes existing global rows.
- Only regular `CARD` rows whose `card_types` contains `Monster`, `Spell` or
  `Trap` are persisted. Tokens, Strategy, Tip, Checklist, Skill and Non-game
  cards are dropped; the migration deletes existing global ones.
- Sets that belong to Rush Duel and TCG/OCG at the same time are skipped.
  Sets that had cards but no regular card left are deleted (`SSTK` is the
  token-only example); sets with no card data are kept.
- Cards whose `set_id` points to a missing set are dropped; the migration
  deletes existing global orphan cards.

## Coding conventions

- Run `gofmt` on every Go file you change.
- Wrap errors with `%w` so callers can use `errors.Is` / `errors.As`.
- Keep exported types and functions documented.
- Prefer `context.Context` for I/O and pass cancellation through.
- Keep provider-specific parsing in the provider package; keep transport and
  retry logic in `internal/utility/retrier` for the new providers.
- Do not commit `.env`, cookies, browser profiles or generated credentials.

## Testing notes

- Unit tests for the Dragon Shield parser use HTML fixtures and do not need a
  browser.
- `TestGetSleevesLive` is opt-in (`DRAGONSHIELD_LIVE=1`) and scrapes the real
  collection. At the time of writing it collected 230 sleeves in about 5m24s.
- The YGO live tests are `TestGetNewReleasesLive` and
  `TestFetchMediawikiAllSetsLive`; `-short` skips them.
- If a live test fails because Cloudflare returns a challenge, run it with a
  visible browser and solve the challenge if the window asks for it.
- The retrier progress dashboard only renders when stdout is a terminal. Test
  output will not show it.
- GORM slow-query logs are disabled in the sync; only warnings and errors are
  printed.
