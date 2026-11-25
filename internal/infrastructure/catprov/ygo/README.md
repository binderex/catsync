# ygo

Yu-Gi-Oh! catalog provider.

The package reads set releases from Yugipedia, resolves card information from
YGOPRODeck when possible, and returns denormalized `domain.Item` values (sets
and prints) ready for the catalog upsert.

## API

```go
func NewYGOCatalogProvider(persistence ...ports.RetrierPersistence) *Provider

func (p *Provider) Name() string

func (p *Provider) GetAllReleases(
    ctx context.Context,
    opts domain.ProviderOptions,
) ([]domain.Item, error)

func (p *Provider) GetNewReleases(
    ctx context.Context,
    opts domain.ProviderOptions,
) ([]domain.Item, error)
```

- `GetAllReleases` walks every set in `Category:All sets`.
- `GetNewReleases` walks the sets announced on the Yugipedia main page.
- `opts.Mediums` accepts `TCG`, `OCG` and `Rush Duel`. Rush Duel is opt-in and
  emits only official English editions; it stays disabled until an English
  release exists.
- Both methods return sets and prints as a flat `[]domain.Item`.
- `GetAllReleases` can checkpoint and resume a long crawl.

## Flow

```text
releases
   |
   v
set pages
   |
   +-- sets and base prints
   +-- lazy Set Card Lists pages
   +-- card pages
   +-- card galleries
   |
   v
[]domain.Item
```

### Sets

The set page provides names, prefixes, release dates, type and image.
One `domain.Item` with `Kind = SET` is created per valid language release.
Invalid or unreleased languages are skipped.

Set lists can be loaded lazily. Only existing `Set Card Lists` pages are
queued; redlinks and languages without a release are ignored.

### Prints

Each set-list row creates one `domain.Item` with `Kind = CARD` per rarity. The
provider keeps:

- full card code and serial code;
- language, set and release date;
- rarity and extended-art state;
- `SetID` and the card `Source`.

`GuaranteedCopies` works as follows:

- booster products are random, so the value stays `0`;
- non-booster sets count code and rarity occurrences in the set list;
- complex boxes use the regular pack values and ignore the bonus pack.

### Breakdown

The set page Breakdown is stored in `Set.Description` for each language.
Tables are excluded. Simple pack information fills `QuantityCardsPerPack` and
`QuantityPacksPerSet`.

Language rules:

- `TCG` applies to EN, FR, DE, IT, PT and ES;
- `Japanese` applies to Asian languages without a specific section;
- specific sections such as `Korean` or `Simplified Chinese` take priority;
- a breakdown without sections applies to every set language.

### Card information

Card info resolution order:

1. use the supplied card info;
2. search YGOPRODeck by card name;
3. scrape the card page as a fallback.

Rush Duel, Strategy and Token cards use the page fallback directly.

The resolved card info is copied onto each print: `Brand`, `Source`,
`VerifiedSource` and the card properties.

`VerifiedSource` is false when the page contains the hatnote:

```text
The English name given is not official.
```

### Galleries

Gallery pages are split into sections by heading:

- normal language sections;
- `Rush Duel - Japanese`;
- `Rush Duel - Korean`;
- ignored `Anime`, `Manga`, `Video games` and `Other` sections.

A print selects the section that matches its brand and language. This allows a
normal card and its Rush Duel version to share a gallery URL without mixing
images. Section IDs are not used because they can be duplicated.

## Files

| File | Responsibility |
| --- | --- |
| `catprov.go` | Provider, releases and public methods. |
| `run.go` | Resumable crawl queue and retrier orchestration. |
| `state.go` | Release checkpoint format and snapshot encoding. |
| `scrape_set.go` | Set page, breakdown and set-list page parsing. |
| `scrape_set_cards.go` | Set-list card rows and item materialization. |
| `scrape_card.go` | Card page info and print rows. |
| `scrape_gallery.go` | Card gallery parsing and image selection. |
| `ygoprodeck.go` | YGOPRODeck client and cache. |
| `mediawiki.go` | Semantic MediaWiki set index. |
| `utils.go` | Shared parsing helpers. |

## Persistence

The provider returns a flat `[]domain.Item` (sets first, then prints). The
`CatalogProviderUseCase` upserts everything through the PostgreSQL catalog
repository in one operation. Rows are written in batches of 250, and each batch
has its own timeout. A full catalog is larger than the old 15-minute persistence
deadline, so persistence runs until it finishes or the process is interrupted
with Ctrl+C. Persistence is idempotent; if it stops after writing some sets,
running `go run ./cmd/sync -catprov ygo -mode all` reuses the provider snapshot
stored in `data/retrier.json`, retries the pending pages and upserts the
catalog again without re-scraping the completed pages.

## Checkpoints and reports

`GetAllReleases` stores its parsed snapshot in `data/retrier.json` under the
active run's `provider_data` field:

```json
{
  "run_id": "...",
  "provider": "ygo",
  "mode": "all",
  "states": {},
  "provider_data": {
    "releases": [],
    "sets": [],
    "pages": {}
  }
}
```

The URL progress and the parsed snapshot therefore live in the same file. The
crawl report is written to `data/ygo-crawl-report.json`; the `data/` folder is
git-ignored.

If the retrier document is lost, the snapshot can be rebuilt from the catalog
already stored in PostgreSQL:

```powershell
go run ./cmd/recover
```

Card rows without a scraped card page derive `Source` from the card name, then
the card code, and finally the set page; `source` is part of the identity and
cannot be empty. Rows whose name is the `unknown` placeholder are not catalog
entries: the use case drops them and the migration deletes any existing row.

Only regular cards are persisted: a `CARD` row must have `Monster`, `Spell` or
`Trap` in `card_types`. Tokens, Strategy, Tip, Checklist, Skill and Non-game
cards are dropped by the use case and deleted by the migration.

Sets whose `Mediums` include both Rush Duel and TCG/OCG are skipped because
they are ambiguous. Sets that had cards but none of them is a regular card are
deleted; sets with no card data at all are kept.

Each crawl also writes `data/ygo-crawl-report.json` with sets, prints and
failures.

## Tests

```powershell
go test ./internal/infrastructure/catprov/ygo -short -count=1
go test ./internal/infrastructure/catprov/ygo -run TestGetNewReleasesLive \
  -v -count=1 -timeout 15m
```

The live test is skipped by `-short`.
