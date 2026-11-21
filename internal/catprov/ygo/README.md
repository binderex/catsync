# ygo

Yu-Gi-Oh! catalog provider.

The package reads set releases from Yugipedia, resolves card information from
YGOPRODeck when possible, and returns complete `catalog.Set` values with their
`CardPrinted` rows and `ItemInfo` relations.

## API

```go
func NewYGOCatalogProvider() *Provider

func (p *Provider) GetAllReleases(
    ctx context.Context,
    resume bool,
    mediums ...string,
) ([]catalog.Set, error)

func (p *Provider) GetNewReleases(
    ctx context.Context,
    mediums ...string,
) ([]catalog.Set, error)
```

- `GetAllReleases` walks every set in `Category:All sets`.
- `GetNewReleases` walks the sets announced on the Yugipedia main page.
- `mediums` accepts `TCG`, `OCG` and `Rush Duel`.
- Both methods return sets with their prints.
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
[]catalog.Set
```

### Sets

The set page provides names, prefixes, release dates, type and image.
One `catalog.Set` is created per valid language release. Invalid or unreleased
languages are skipped.

Set lists can be loaded lazily. Only existing `Set Card Lists` pages are
queued; redlinks and languages without a release are ignored.

### Prints

Each set-list row creates one `CardPrinted` per rarity. The provider keeps:

- full card code and serial code;
- language, set and release date;
- rarity and extended-art state;
- `SetID` and, after resolution, `ItemInfoID`.

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

`ItemInfo` resolution order:

1. use the supplied `ItemInfo`;
2. search YGOPRODeck by card name;
3. scrape the card page as a fallback.

Rush Duel, Strategy and Token cards use the page fallback directly.

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
| `state.go` | Checkpoint format and JSON persistence. |
| `scrape_set.go` | Set, breakdown and set-list parsing. |
| `scrape_card.go` | Card pages, galleries and `ItemInfo`. |
| `ygoprodeck.go` | YGOPRODeck client and cache. |
| `mediawiki.go` | Semantic MediaWiki set index. |
| `utils.go` | Shared parsing helpers. |

## Persistence

After the crawl, `cmd/sync/ygo` persists the result in three phases:

1. upsert the deduplicated `ItemInfo` rows;
2. upsert all sets in bulk and resolve their persisted IDs in one query;
3. attach those IDs to the prints and upsert all card prints in bulk.

Rows are written in batches of 250, and each batch has its own timeout. A full
catalog is larger than the old 15-minute persistence deadline, so persistence
runs until it finishes or the process is interrupted with Ctrl+C. Persistence
is idempotent; if it stops after writing some sets, running
`go run ./cmd/sync/ygo -mode all -resume` reuses `ygo-releases-state.json`,
retries the pending pages and upserts the catalog again without re-scraping the
completed pages.

## Checkpoints and reports

`GetAllReleases` uses:

```text
ygo-releases-state.json
```

for resumable full crawls.

Each crawl also writes:

- `ygo-crawl-report.json`, with sets, prints and failures;
- `reports/ygo_<state>_<date>.json`, the generic retrier report.

## Tests

```powershell
go test ./internal/catprov/ygo -short -count=1
go test ./internal/catprov/ygo -run TestGetNewReleasesLive \
  -v -count=1 -timeout 15m
```

The live test is skipped by `-short`.
