# catprov

Catalog provider package. It fetches card and set data from external APIs and
websites and converts it into `catalog.CatalogItem` values ready to be persisted
by the catalog repository.

Only the **Yu-Gi-Oh!** provider (`YGOCatProv`) is implemented today. Pokémon is
planned as a future provider.

---

## Files

| File | Responsibility |
| --- | --- |
| `catprov.go` | Interface, errors, retry constants and User-Agent. |
| `ygo_catprov.go` | Provider entry points, Yugipedia spider and retries. |
| `ygo_catprov_mapper.go` | HTML-to-`CatalogItem` mappers. |
| `ygo_catprov_test.go` | Integration tests against YGOPRODeck and Yugipedia. |

---

## Interface

```go
type CatalogProvider interface {
 GetCards(name string, max time.Time) ([]catalog.CatalogItem, error)
 GetCardPrints(card catalog.CatalogItem) ([]catalog.CatalogItem, error)
 GetSets(cards []catalog.CatalogItem) ([]catalog.CatalogItem, error)
}
```

`max` is the release-date cutoff used by `GetCards`; production code normally
passes `time.Now().Add(24 * time.Hour)`.

---

## `YGOCatProv`

```go
type YGOCatProv struct {
 ygoproBaseURL    string
 yugipediaBaseURL string
 now              time.Time
 client           *http.Client
}
```

- `ygoproBaseURL`: `https://db.ygoprodeck.com/api/v7`.
- `yugipediaBaseURL`: `https://yugipedia.com`.
- `now`: reference instant used by mappers to filter future releases. It is set
  once in `NewYGOCatProv()`; tests may override it to make date filtering
  deterministic.
- `client`: HTTP client with a 30s timeout for YGOPRODeck requests.

There is no fallback to `time.Now()` inside the methods: `now` must always come
from the constructor (or be set explicitly by tests).

---

## End-to-end flow

```text
GetCards("ojama", max)
        │
        ▼
  YGOPRODeck cardinfo.php
        │  []CatalogItem (base card: name, description, images, etc.)
        ▼
GetCardPrints(card)
        │
        ├── Yugipedia: card page
        │       └── mappedPrints()  → one CatalogItem per print/rarity
        │
        └── Yugipedia: Card_Gallery:<card>
                └── mappedGalleryBoxes() → edition, rarity code, image,
                                           Extended artwork
                        │
                        ▼
                mergeGalleryIntoPrints()
                        │
                        ▼
        []CatalogItem with complete data for every print
                        │
                        ▼
GetSets(prints)
        │
        ├── extract unique SetName values from English cards
        ├── visit every set page (no candidate fallback)
        ├── mappedSetInfo()    → set variants per language + ReleasedAt
        ├── filter cards by released prefixes/languages
        └── mappedSetCarList() → SetImage, SetType, GuaranteedCopies
```

---

## `GetCards`

Queries the YGOPRODeck API:

```text
GET /cardinfo.php?fname=<name>&misc=yes
```

Behavior:

1. If `name` is empty, downloads the full catalog (`cardinfo.php?misc=yes`).
2. If the HTTP status is not `200`, returns `ErrCardsNotFound`.
3. Decodes the JSON and skips:
   - **Skill Cards** (`humanReadableCardType` contains `"Skill"`).
   - Cards whose TCG **and** OCG dates are later than `max` (usually
     `time.Now().Add(24h)`).
4. Builds one `catalog.CatalogItem` per card:
   - `ExternalID`: numeric ID formatted to 8 digits (`%08d`).
   - `Name`, `Description`, `Archetype`.
   - `CardInfo`: map with `card_types`, `atk`, `def`, `race`, `attribute`,
     `level`.
   - `CardImages`.
   - `Brand: BrandYGO`, `Type: CatalogTypeCard`, `Lang: LangEN`.
5. If no cards remain, returns `ErrCardsNotFound`.

YGOPRODeck returns English names and descriptions, so `LangEN` is fixed at this
stage.

---

## `GetCardPrints`

Fetches all prints of a card. It performs two Yugipedia visits.

### 1. Card page

Tries these candidates in order until one is valid:

```text
<Name>
<Name>_(card)
<ExternalID>
```

A page is considered invalid when it contains:

```css
.noarticletext, .set-lists--navbar
```

`noarticletext` means the page does not exist or has no article content.
`set-lists--navbar` means a set page was found instead of a card page.

### 2. Gallery

On the card page it looks for:

```html
<a href="/wiki/Card_Gallery:..." title="Card Gallery:...">Gallery</a>
```

and visits that URL. If no gallery link exists, it still returns the prints
found on the card page.

### Print mapping

`mappedPrints` parses the card page HTML:

- `table.wikitable` rows provide localized names and descriptions from
  `<span lang="...">`.
- `table[id^='cts--']` tables provide one table per language with Release,
  Number, Set, localized set name and Rarity columns.
- Skips codes containing placeholders (`?`, e.g. `MZMC-EN0??`).
- Skips prints whose release date is later than `now + 24h`.
- Generates **one `CatalogItem` per rarity**.
- Fills: `Code`, `SerialCode`, `SetName`, `SetCode`, `SetRegionalCode`,
  `Rarity`, `Lang`, `ReleasedAt`, and localized `Name` / `Description`.

### Gallery mapping

`mappedGalleryBoxes` walks `li.gallerybox` elements and extracts:

- `Code` from the first link in `.gallerytext` (validated with `AAAA-BBBB`).
- `Rarity` and `RarityCode`.
- `Edition` (`1st Edition`, `Unlimited Edition`, etc.).
- `ExtendedArtwork`: `true` when the gallery box text contains
  `"Extended artwork"` (case-insensitive).
- `PrintImage`: small/large URLs derived from the thumbnail.

### Merge and uniqueness

`mergeGalleryIntoPrints` matches prints and gallery boxes by:

```text
Code + Rarity (+ Edition / ExtendedArtwork as variants)
```

- The first matching box enriches the base print.
- If another box shares the code/rarity but differs in **edition** or
  **Extended artwork**, a copy is created.
- Variants are deduplicated with:

```text
CODE | rarity | edition | extended_artwork
```

This matters because the composite unique index of `CatalogItem` includes
`ExtendedArtwork`: a normal print and its Extended artwork variant are separate
rows.

### Errors

- No candidate page found → `[]CatalogItem{card}, ErrCardPrintsNotFound`.
- Page found but no prints → `[]CatalogItem{card}, ErrCardPrintsNotFound`.
- Fatal network/block error → `[]CatalogItem{card}, err`.
- Gallery failure → returns the base prints without gallery enrichment.

---

## `GetSets`

Receives a list of cards/prints and enriches their set information.

1. Extracts unique `SetName` values from cards with `Lang == LangEN` using:

   ```go
   setNames := make(map[string]struct{})
   ```

2. If there are no names, returns `ErrSetNotFound`.
3. Visits **every** set page (there is no candidate logic for sets).
4. For each set:
   - `mappedSetInfo`: parses the infobox and returns one variant per language
     with `Name`, `SetName`, `SetType`, `SetCode`, `SetRegionalCode`,
     `SetImage` and `ReleasedAt`.
   - Filters input cards by `SetName`.
   - Skips codes containing `?`.
   - Computes released prefixes (`IMPH-JP`, `RA05-EN`, etc.) and released
     languages.
   - Keeps only cards whose prefix/language is released.
   - `mappedSetCarList`: fills `SetImage`, `SetType` and `GuaranteedCopies`.
5. Returns sets plus updated cards.
6. If a set fails, it continues with the remaining sets and returns
   `errors.Join` with all errors. Partial results may be returned together with
   the error.

### `ReleasedAt`

- `mappedPrints` fills `ReleasedAt` with each print's release date.
- `mappedSetInfo` fills `ReleasedAt` with each set variant's release date.
- An unknown date stays as `time.Time{}` (zero value).

### `GuaranteedCopies`

`mappedSetCarList`:

- Detects whether the set is a booster (`booster pack`, `mega pack`,
  `mini pack`).
- For non-booster sets, counts `code|rarity` occurrences in the card list
  tables, excluding the "Bonus cards" section.
- Assigns that count to `GuaranteedCopies`.

Examples used in tests:

- `CH02-EN001` Secret Rare → `0` (bonus).
- `CH02-EN001` Ultra Rare → `1`.
- `SR14-EN045` Super Rare → `1`.

---

## HTTP, retries and rate limiting

Mappers receive `*goquery.Document` values. Network access is handled by Colly
through `newYugipediaCollector()`.

Configuration:

- Browser-like Chrome User-Agent.
- `Accept` and `Accept-Language` headers.
- `Parallelism: 1`.
- 1s delay plus up to 500ms random delay between requests.
- 30s timeout per request.
- `AllowURLRevisit` so retries can request the same URL again.

`visitYugipediaPage` applies this policy:

| Case | Wait | Attempts |
| --- | --- | --- |
| `429 Too Many Requests` | `Retry-After` or 5s → 10s → 20s | 4 |
| `401/403` (blocked) | `Retry-After` or 30s → 60s → 120s | 3 |
| `5xx`, timeout or network error | 2s → 4s → 8s | 3 |
| `404` or other non-retryable | no wait | next candidate |

Maximum wait per attempt is 2 minutes.

Errors that stop candidate iteration:

- `ErrTooManyRequests`
- `ErrClientForbidden`

---

## Package errors

```go
ErrClientTimeout
ErrTooManyRequests
ErrPageNotFound
ErrInvalidPage
ErrClientForbidden
ErrCardsNotFound
ErrCardPrintsNotFound
ErrSetNotFound
errNextCandidate // internal
```

`errNextCandidate` is internal: it means the current page is unusable and the
next candidate may be tried. It is wrapped with `%w` so callers can detect it
using `errors.Is`.

---

## Tests

These are **integration tests**: they call the real YGOPRODeck and Yugipedia
services. They are skipped with `-short`.

```bash
go test ./internal/catprov -v
go test ./internal/catprov -short          # no network
```

Current tests:

- `TestGetCardsDateFiltering`: date filtering and basic fields.
- `TestGetCardPrints`: Dark Magician.
- `TestGetCardPrintsFields`: Ojamassimilation; validates `ReleasedAt`, code,
  set, rarity, etc.
- `TestGetCardPrintsEditions`: same code with different editions.
- `TestGetCardPrintsSkipsPlaceholderCodes`: drops codes containing `?`.
- `TestGetCardPrintsExtendedArtwork`: detects Extended artwork variants.
- `TestGetCardsAndCardPrints`: integration limited to 5 cards.
- `TestGetSets`: full GetCards → GetCardPrints → GetSets flow with every card
  (can take ~90s).
- `TestGetSetsGuaranteedCopies`: `GuaranteedCopies` with dummy cards.
- `TestGetSetsImmortalPhoenix`: JP/SC variants and date filtering.

### Timeout

The full suite takes about 2 minutes. When running from VS Code, the default
`go.testTimeout` is 30s; increase it:

```json
{
  "go.testTimeout": "5m"
}
```

Or run from a terminal:

```bash
go test ./internal/catprov -v -timeout 10m
```

---

## Relevant dependencies

- `github.com/gocolly/colly/v2`: spider, HTML callbacks and rate limiting.
- `github.com/PuerkitoBio/goquery`: DOM parsing/navigation in the mappers.
- `github.com/binderex/catsync/internal/catalog`: output model.

---

## Notes for future changes

- To add a new provider, implement `CatalogProvider` and keep mappers decoupled
  from HTTP.
- If `catalog.CatalogItem` changes, review every mapper: fields are **values**
  (not pointers), and an unknown date/image is represented by its zero value.
- `ExtendedArtwork` is part of print identity. Do not collapse Extended artwork
  variants into the regular print.
- Tests depend on the network and on Yugipedia's current HTML. If a test starts
  failing, first check the site's classes, selectors and table structure.
- `GetSets` only extracts set names from English cards. To support sets from
  other languages, change the `setNames` extraction.
