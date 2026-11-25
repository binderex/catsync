# Catsync

Catsync is the catalog synchronization service for **Binderex**. It fetches
trading-card data from external providers, normalizes it into one denormalized
catalog model and writes it to PostgreSQL.

> **Status: first version, under active development.**
> The schema, flags and internal APIs may change. There are no data migrations
> yet; the catalog is treated as version 1.

## Responsibilities

- discover cards, prints and sets from external providers;
- normalize provider data into a single `domain.Item` model;
- deduplicate by a stable identity before every upsert;
- keep the PostgreSQL catalog schema up to date;
- persist crawl progress so long synchronizations can resume.

## Architecture

```text
cmd/sync
   |
   v
internal/application  (ports + use cases)
   |
   v
internal/domain       (catalog identity, retrier run types)
   ^
   |
internal/infrastructure (providers, PostgreSQL, JSON retrier)
internal/utility/retrier (HTTP/Colly/Chromedp queue)
```

- **`internal/domain`**: the catalog `Item`, its identity, deduplication and
  the retrier run model. No infrastructure imports.
- **`internal/application`**: ports (interfaces) and use cases. It orchestrates
  providers, catalog upserts and retrier persistence.
- **`internal/infrastructure`**: Dragon Shield and Yu-Gi-Oh! providers,
  PostgreSQL catalog adapter and the single-file JSON retrier store.
- **`internal/utility/retrier`**: a small URL queue with retries and transports.
  It has no business rules.

## Catalog model

Every row lives in one denormalized `items` table (`domain.Item`). Sets, cards,
sleeves and playmats share the table and are distinguished by `kind`.

The upsert identity is:

```text
(kind, brand, lang, code, set_id, rarity, edition, extended_art, source, variant_key)
```

- `source` is the item's page URL and **cannot be empty**.
- `variant_key` is a generated column derived from `properties->'variant'`.
- Non-identity data lives in `properties`.
- `CatalogUseCase.Upsert` removes duplicates and non-standard rows before the
  repository writes them.

Current catalog rules:

- only monsters, spells and traps are persisted;
- tokens, strategy, tip, checklist, skill and non-game cards are dropped;
- items named `unknown` are dropped;
- sets that have cards but no regular card left are deleted;
- sets with no card data are kept.

## Providers

| Provider | Capability | Status |
| --- | --- | --- |
| Yu-Gi-Oh! TCG/OCG | Cards, prints, sets | Implemented |
| Yu-Gi-Oh! Rush Duel | Cards, prints, sets | Implemented, disabled until an official English release |
| Dragon Shield | Sleeves | Implemented |
| Pokémon | Cards, prints, sets | Planned |

## Storage

PostgreSQL is the container from the parent
`C:\Users\natan\Proyectos\Binderex\compose.yaml`, exposed on port `5433`:

```text
postgres://binderex:binderex@localhost:5433/binderex?sslmode=disable
```

The retrier state is one JSON document at `data/retrier.json` (`version: 2`).
It holds the URL progress and, in `provider_data`, the parsed provider snapshot
(for example the YGO sets and prints). The crawl report is
`data/ygo-crawl-report.json`. Both live under the git-ignored `data/` folder.

## Commands

```powershell
go run ./cmd/sync -catprov ygo -mode new
go run ./cmd/sync -catprov ygo -mode all
go run ./cmd/sync -catprov dragonshield -mode all
```

`-resume` defaults to `true`; use `-resume=false` for a fresh crawl.

## Configuration

- `DB_URL` or `CATSYNC_DB_URL`: PostgreSQL DSN.
- `RETRIER_JSON_PATH`: retrier document path. Default: `data/retrier.json`.
- `RETRIER_SAVE_INTERVAL`: intermediate save interval. Default: `10s`.
- `DRAGONSHIELD_BROWSER`: optional Dragon Shield browser executable.

The command loads `.env` from the repository root and, as a fallback, the
parent `C:\Users\natan\Proyectos\Binderex\.env`.

## Development

```powershell
go build ./...
go vet ./...
go test ./... -short
```

## Documentation

- [cmd/sync](cmd/sync/README.md)
- [cmd/recover](cmd/recover/README.md)
- [Domain](internal/domain/README.md)
- [Application](internal/application/README.md)
- [Ports](internal/application/ports/README.md)
- [Use cases](internal/application/usecases/README.md)
- [Infrastructure](internal/infrastructure/README.md)
- [Retrier utility](internal/utility/retrier/README.md)
- [Recovery utility](internal/utility/recovery/README.md)
- [Yu-Gi-Oh! provider](internal/infrastructure/catprov/ygo/README.md)
- [Dragon Shield provider](internal/infrastructure/catprov/dragonshield/README.md)
- [Persistence](internal/infrastructure/persistence/README.md)
- [JSON retrier store](internal/infrastructure/persistence/json/README.md)
- [PostgreSQL catalog](internal/infrastructure/persistence/sql/postgresql/catalog/README.md)
