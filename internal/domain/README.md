# internal/domain

Core types and rules. This package has no infrastructure imports and no I/O.

## Catalog

`catalog.go` defines the single denormalized catalog entity:

```go
type Item struct {
    ID             string
    StoreID        *string
    Kind           Kind
    Brand          Brand
    Lang           Lang
    Code           string
    SetID          *string
    Rarity         string
    Edition        string
    ExtendedArt    bool
    Source         string
    VerifiedSource bool
    Name           string
    Description    string
    Image          Image
    ReleaseAt      time.Time
    Properties     map[string]any
}
```

IDs are strings in the domain. The PostgreSQL adapter converts them to
`uuid.UUID`. `StoreID` is `nil` for global catalog rows and set for store-owned
rows; Catsync only produces global rows.

The struct keeps its `json` tags because the retrier snapshot serializes
`domain.Item`. It has no GORM tags; the persistence model lives in the
PostgreSQL adapter.

### Identity

`Item.IdentityKey()` is the canonical identity used by the upsert. It includes:

```text
store_id, kind, brand, lang, code, set_id, rarity, edition,
extended_art, source, variant
```

`store_id` keeps global and store-owned rows in separate identity scopes.
`source` is part of the identity. Global rows must have a non-empty source;
store-owned rows may leave it empty.

### Properties

Non-identity data lives in `Properties`:

- `variant`: product variant attributes; it drives `variant_key`;
- `images`, `serial_code`, `rarity_code`, `guaranteed_copies`;
- set fields such as `regional_code`, `quantity_cards_per_pack`,
  `quantity_packs_per_set`, `set_type`, `set_code`;
- card fields such as `archetype`, `card_types`, `attribute`, `level`, `atk`,
  `def`, `race`.

The package provides `Property`, `SetProperty`, `PropertyString`,
`PropertyUint`, `PropertyBool`, `PropertyImages` and `SetPropertyImages`.

### Deduplication

`DeduplicateItems(items)` removes duplicate identities, keeps the last
occurrence and remaps `SetID` references when duplicate sets are dropped.
It runs a second deduplication pass after remapping because two prints can
collapse onto the same identity.

Callers must assign IDs first. The catalog use case does that in
`prepareItems` before calling this function.

## Retrier

`retrier.go` defines the transport-free retrier types:

- `Response`, `Filter`, `FilterResolver`;
- `URLState` and `Progress`;
- `Policy` and retry helpers;
- `RetrierRun`, `RetrierRunSummary`, `RetrierRunFilter`;
- run states and errors.

`RetrierRun.ProviderData` carries a provider-specific crawl snapshot (for
example the parsed YGO sets and prints) so the JSON document is the only state
file. `ProviderOptions` (in `catprov.go`) carries the provider mediums, resume
flag, mode and the persisted `ResumeState`.

## Tests

```powershell
go test ./internal/domain
```
