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

The repository is **mid-refactor**. The new catalog model (`catalog.Item`,
`catalog.CardPrinted`, `catalog.Set`, `catalog.ItemInfo`) and the new retrier
package exist. The legacy catalog provider files were removed; the active
providers are Dragon Shield and Yu-Gi-Oh!.

## Layout

| Path | Responsibility |
| --- | --- |
| `cmd/sync/dragonshield` | Scrapes Dragon Shield sleeves and upserts them. |
| `cmd/sync/ygo` | Scrapes Yu-Gi-Oh! releases, resumes from a checkpoint and upserts sets/prints. |
| `internal/catprov/dragonshield` | Dragon Shield provider and HTML parsing. |
| `internal/catprov/ygo` | Yu-Gi-Oh! provider, release checkpoint, card and set scrapers. |
| `internal/retrier` | Generic retry queue with HTTP, Colly and Chromedp transports. |
| `internal/catalog` | Catalog model, identity rules, repository and service. |
| `internal/database` | PostgreSQL connection and migrations. |

Provider docs:

- [`internal/catprov/dragonshield/README.md`](internal/catprov/dragonshield/README.md)
- [`internal/catprov/ygo/README.md`](internal/catprov/ygo/README.md)
- [`internal/retrier/README.md`](internal/retrier/README.md)

## Commands

Regular validation:

```powershell
go test ./internal/retrier ./internal/catprov/dragonshield ./internal/catprov/ygo ./cmd/sync/dragonshield ./cmd/sync/ygo
go vet ./internal/retrier ./internal/catprov/dragonshield ./internal/catprov/ygo ./cmd/sync/dragonshield ./cmd/sync/ygo
```

Run the synchronizations:

```powershell
go run ./cmd/sync/dragonshield
go run ./cmd/sync/ygo                 # GetNewReleases (default)
go run ./cmd/sync/ygo -mode all       # GetAllReleases, fresh crawl
go run ./cmd/sync/ygo -mode all -resume
```

`-mode all` starts a fresh crawl and overwrites `ygo-releases-state.json`;
`-mode all -resume` continues from that checkpoint. The default mode is
`new` (`GetNewReleases`) and does not use a checkpoint. Every crawl writes
`ygo-crawl-report.json` with counts, set/print totals and the failed URLs.

`DB_URL` must be set in the environment or in `.env`.

Live browser tests are opt-in. They open a real browser and are not part of the
default test run:

```powershell
$env:DRAGONSHIELD_LIVE="1"

go test ./internal/retrier -run TestChromedpNavigatesDragonShield -v -count=1 -timeout 5m
go test ./internal/catprov/dragonshield -run TestGetSleevesLive -v -count=1 -timeout 12m
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
- `subtypes` contains `color`, `size`, `texture`, `glare_type`, `quality`,
  `items_per_case`, `material`, `layer`, and `clear` when applicable.
- The main product image is taken from `.js-product-gallery-image img` and
  rendered at `width=480` and `width=800`.

## Database rules

- `database.New(dsn)` opens PostgreSQL and runs `Migrate`.
- `Item`, `CardPrinted` and `Set` share the `items` table.
- **Do not merge their `AutoMigrate` calls into one call.** GORM deduplicates
  models by table name inside a single `AutoMigrate`, so only one model would
  contribute its columns. Migrate them one at a time:

  ```go
  for _, model := range []any{
      &catalog.Item{},
      &catalog.CardPrinted{},
      &catalog.Set{},
      &catalog.ItemInfo{},
  } {
      if err := db.AutoMigrate(model); err != nil {
          return err
      }
  }
  ```

- `idx_items_identity` includes `subtypes`; it is part of the item identity.
- `subtypes` is not in `itemMutableColumns`. Do not add it unless the user asks
  to change the identity rules.

## Coding conventions

- Run `gofmt` on every Go file you change.
- Wrap errors with `%w` so callers can use `errors.Is` / `errors.As`.
- Keep exported types and functions documented.
- Prefer `context.Context` for I/O and pass cancellation through.
- Keep provider-specific parsing in the provider package; keep transport and
  retry logic in `internal/retrier`.
- Do not commit `.env`, cookies, browser profiles or generated credentials.

## Testing notes

- Unit tests for the Dragon Shield parser use HTML fixtures and do not need a
  browser.
- The Chromedp test is an integration test: it launches Brave/Chrome/Edge and
  navigates to the real collection.
- `TestGetSleevesLive` scrapes the full collection. At the time of writing it
  collected 230 sleeves in about 5m24s.
- If a live test fails because Cloudflare returns a challenge, run it with a
  visible browser and solve the challenge if the window asks for it.
- The retrier progress dashboard only renders when stdout is a terminal. Test
  output will not show it.
