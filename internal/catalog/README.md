# catalog

Catalog domain, identity rules and persistence.

## Model

The catalog uses two tables:

| Table | Go type | Purpose |
| --- | --- | --- |
| `item_infos` | `ItemInfo` | Shared card/set information. |
| `items` | `Item`, `CardPrinted`, `Set` | Concrete rows; shared table. |

```text
item_infos 1 ─── N items
                      ├── CardPrinted (kind = CARD)
                      ├── Set         (kind = SET)
                      ├── Item        (kind = PLAYMAT)
                      └── Item        (kind = SLEEVE)

Set 1 ─── N CardPrinted
        items.set_id = items.id (kind = SET)
```

`Item` also exposes an optional `ItemInfo` association backed by

```go
ItemInfo *ItemInfo `gorm:"foreignKey:ItemInfoID;references:ID"`
```

It is a read/preload helper for scrapers and tests; the persisted column is
still `ItemInfoID`. `UpsertItems`, `UpsertCardPrinted` and `UpsertSets` omit
associations, so an attached `ItemInfo` does not write a second row.

`ItemInfoID` and `SetID` are optional:

- `ItemInfoID = nil` means the item has no shared card/set information. This is
  valid for external products such as playmats and sleeves.
- `SetID = nil` means the item does not belong to a set. This is valid for sets,
  standalone playmats/sleeves and cards without a set.
- A card print normally has both `ItemInfoID` and `SetID`.

## Identity

### `items`

The unique index is:

```text
(kind, brand, lang, code, item_info_id, rarity, edition, extended_art, set_id, subtypes)
```

It is created as `idx_items_identity` with `NULLS NOT DISTINCT`, so items with
`item_info_id = NULL` or `set_id = NULL` still participate in the identity.
`subtypes` is `jsonb` and is part of the identity so product variants that only
differ by subtype, such as sleeve sizes, are stored as separate rows.

### `item_infos`

Two unique indexes are created:

```text
(kind, brand, source)  -- idx_item_infos_identity
(source)               -- idx_item_infos_source
```

`source` is unique on its own, so the upsert conflicts on `source`.

## IDs

IDs are `uuid.UUID` from `github.com/google/uuid`.

- `Item.ID` and `ItemInfo.ID` use `uuid.New()` in the constructors.
- PostgreSQL columns are `uuid` with `gen_random_uuid()` as a fallback for rows
  inserted without an ID.
- Optional relations use `*uuid.UUID`.

## Repository

`Repository` exposes:

| Method | Purpose |
| --- | --- |
| `GetItemInfo(ctx, source, brand, kind)` | First item info matching filters. |
| `GetItemInfosByBrand(ctx, brand)` | All item infos for one brand. |
| `UpsertItemInfo(ctx, info)` | Upserts one item info by `source`. |
| `UpsertItemInfos(ctx, infos)` | Batch version of `UpsertItemInfo`. |
| `UpsertItems(ctx, items)` | Upserts playmats and sleeves. |
| `UpsertCardPrinted(ctx, prints)` | Upserts card print rows. |
| `UpsertSets(ctx, sets)` | Upserts sets; prints are not persisted. |
| `UpsertSetWithPrints(ctx, set, prints)` | Upserts a set and its prints. |

`GetItemInfo` applies only the filters that are not empty:

```go
info, err := repo.GetItemInfo(ctx, source, "", "")
info, err = repo.GetItemInfo(ctx, "", catalog.BrandYGO, catalog.KindSet)
```

If all filters are empty it returns `ErrItemInfoFilterRequired`. If no row
matches, the returned error wraps `gorm.ErrRecordNotFound`.

## Service

`Service` wraps the repository and applies a timeout to upserts:

```go
func (s *Service) GetItemInfo(
    ctx context.Context,
    source string,
    brand Brand,
    kind Kind,
) (*ItemInfo, error)
func (s *Service) GetItemInfosByBrand(
    ctx context.Context,
    brand Brand,
) ([]ItemInfo, error)
func (s *Service) UpsertItemInfo(
    ctx context.Context,
    info *ItemInfo,
) error
func (s *Service) UpsertItemInfos(
    ctx context.Context,
    infos []ItemInfo,
) error
func (s *Service) UpsertItems(
    ctx context.Context,
    items []Item,
) error
func (s *Service) UpsertCardPrinted(
    ctx context.Context,
    prints []CardPrinted,
) error
func (s *Service) UpsertSets(
    ctx context.Context,
    sets []Set,
) error
func (s *Service) UpsertSetWithPrints(
    ctx context.Context,
    set *Set,
    prints []CardPrinted,
) error
```

## Playmats and sleeves

Playmats and sleeves are plain `Item` rows (`KindPlaymat`, `KindSleeve`). They
do not belong to a set and may have no `ItemInfo`. Both constructors accept a
brand and a subtype map:

```go
playmat := catalog.NewPlaymat(
    nil,
    catalog.BrandDS,
    map[string]any{"size": "standard", "finish": "japanese"},
    catalog.LangEN,
    "DS-MAT-001",
    "Dragon Shield Playmat",
    "",
    image,
    releaseAt,
)

sleeve := catalog.NewSleeve(
    nil,
    catalog.BrandDS,
    map[string]any{"finish": "matte", "size": "standard"},
    catalog.LangEN,
    "DS-SLV-001",
    "Dragon Shield Sleeves",
    "",
    image,
    releaseAt,
)
```

`Brand` and `Subtypes` are stored on the shared `Item` row but are only set by
these constructors. Card prints and sets use the regular `NewItem` constructor.

## Upsert behavior

### `item_infos` upsert

Conflict target: `source`.

Mutable columns:

```text
images, verified_source, set_type, set_code, archetype, card_types,
properties, updated_at
```

`kind`, `brand` and `source` are identity columns and are not updated on
conflict. A missing `source` returns `ErrItemInfoSourceRequired`.

### `items` upsert

Conflict target:

```text
kind, brand, lang, code, item_info_id, rarity, edition, extended_art, set_id, subtypes
```

Mutable columns:

```text
name, description, image, release_at, serial_code, rarity_code,
guaranteed_copies, regional_code, quantity_cards_per_pack,
quantity_packs_per_set, updated_at
```

Rows are deduplicated in memory before writing; the last occurrence wins. Writes
are sent in batches of 250 rows.

## `CardsPrinted` relationship

`Set.CardsPrinted` is the inverse of `CardPrinted.SetID`:

```go
type Set struct {
    Item
    CardsPrinted []CardPrinted
}
```

It is not a database column. Because `items` also contains sets, playmats and
sleeves, always filter by kind when loading it:

```go
db.Preload("CardsPrinted", "kind = ?", catalog.KindCard).Find(&set)
```

or query explicitly:

```go
var prints []catalog.CardPrinted
err := db.
    Where("kind = ? AND set_id = ?", catalog.KindCard, set.ID).
    Find(&prints).Error
```

If a playmat or sleeve belongs to a set and sets `SetID`, it would also match
`set_id = set.ID`. The `kind = CARD` filter is what keeps `CardsPrinted`
semantically correct.

## Upserting a set with its prints

`UpsertSetWithPrints` writes the set first and then its prints inside the same
transaction:

```go
if err := service.UpsertSetWithPrints(ctx, set, prints); err != nil {
    return err
}
```

The method:

- assigns the set kind to `KindSet`;
- generates an ID if the set has none;
- resolves the persisted set ID when the identity already exists, so the prints
  always reference the existing set row;
- assigns `SetID = set.ID` to every print;
- assigns `kind = KindCard` to every print;
- upserts the set and prints atomically.

It does **not** delete prints that are no longer present. If a set loses prints,
add a reconciliation step that deletes the missing rows before or after the
upsert, within the same transaction.

## Example

```go
info := catalog.NewCardInfo(
    catalog.BrandYGO,
    cardImages,
    cardSource,
    "Blue-Eyes",
    "Normal Monster",
    map[string]any{"atk": 3000, "def": 2500},
)
if err := service.UpsertItemInfo(ctx, info); err != nil {
    return err
}

item := catalog.NewItem(
    &info.ID,
    catalog.KindCard,
    catalog.LangEN,
    "LOB-EN001",
    "Blue-Eyes White Dragon",
    "This legendary dragon is a powerful engine of destruction.",
    cardImage,
    releaseAt,
)

setInfo := catalog.NewSetInfo(
    catalog.BrandYGO,
    setImages,
    setSource,
    "Booster pack",
    "LOB",
)
if err := service.UpsertItemInfo(ctx, setInfo); err != nil {
    return err
}

setItem := catalog.NewItem(
    &setInfo.ID,
    catalog.KindSet,
    catalog.LangEN,
    "LOB",
    "Legend of Blue Eyes White Dragon",
    "",
    setImage,
    releaseAt,
)
set := catalog.NewSet(*setItem, "LOB-EN", 9, 24)

print := catalog.NewCardPrinted(
    *item,
    &set.ID,
    "001",
    "Ultra Rare",
    "UR",
    "1st Edition",
    false,
    0,
)
err := service.UpsertCardPrinted(
    ctx,
    []catalog.CardPrinted{*print},
)
if err != nil {
    return err
}
```

## Migrations

`internal/database.Migrate` runs `AutoMigrate` for `Item`, `CardPrinted`, `Set`
and `ItemInfo`, then creates the unique indexes explicitly.

Foreign key migration is disabled because `Set` and `CardPrinted` share the
`items` table and the `set_id` relation is self-referencing. The relation is
validated in the repository/service layer.

## Errors

| Error | Meaning |
| --- | --- |
| `ErrItemInfoFilterRequired` | `GetItemInfo` was called without any filter. |
| `ErrItemInfoSourceRequired` | An item info without `source` was upserted. |
| `gorm.ErrRecordNotFound` | No item info matched the filters. |
