# catalog

Catalog domain, identity rules and persistence.

## Model

The catalog uses two tables:

| Table | Go type | Purpose |
| --- | --- | --- |
| `item_infos` | `ItemInfo` | Shared, language-independent information for cards, sets and optional products. |
| `items` | `Item`, `CardPrinted`, `Set` | Concrete catalog rows. `CardPrinted` and `Set` embed `Item` and share this table. |

```text
item_infos 1 ─── N items
                      ├── CardPrinted (kind = CARD)
                      ├── Set         (kind = SET)
                      └── Item        (kind = ACCESSORY or another product)

Set 1 ─── N CardPrinted
        items.set_id = items.id (kind = SET)
```

`ItemInfoID` and `SetID` are optional:

- `ItemInfoID = nil` means the item has no shared card/set information. This is
  valid for external products such as accessories.
- `SetID = nil` means the item does not belong to a set. This is valid for sets,
  standalone accessories and cards without a set.
- A card print normally has both `ItemInfoID` and `SetID`.

## Identity

### `items`

The unique index is:

```text
(kind, brand, lang, code, item_info_id, rarity, edition, extended_art, set_id)
```

It is created as `idx_items_identity` with `NULLS NOT DISTINCT`, so items with
`item_info_id = NULL` or `set_id = NULL` still participate in the identity.

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
| `GetItemInfo(ctx, source, brand, kind)` | Returns the first item info matching the non-empty filters. |
| `UpsertItemInfo(ctx, info)` | Inserts or updates one item info by `source`. |
| `UpsertItemInfos(ctx, infos)` | Batch version of `UpsertItemInfo`. |
| `UpsertItems(ctx, items)` | Inserts or updates plain `Item` rows, such as accessories. |
| `UpsertCardPrinted(ctx, prints)` | Inserts or updates card print rows. |
| `UpsertSets(ctx, sets)` | Inserts or updates set rows. The `CardsPrinted` association is not persisted. |

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
func (s *Service) GetItemInfo(ctx context.Context, source string, brand Brand, kind Kind) (*ItemInfo, error)
func (s *Service) UpsertItemInfo(ctx context.Context, info *ItemInfo) error
func (s *Service) UpsertItemInfos(ctx context.Context, infos []ItemInfo) error
func (s *Service) UpsertItems(ctx context.Context, items []Item) error
func (s *Service) UpsertCardPrinted(ctx context.Context, prints []CardPrinted) error
func (s *Service) UpsertSets(ctx context.Context, sets []Set) error
```

## Upsert behavior

### `item_infos`

Conflict target: `source`.

Mutable columns:

```text
images, verified_source, set_type, set_code, archetype, card_types,
properties, updated_at
```

`kind`, `brand` and `source` are identity columns and are not updated on
conflict. A missing `source` returns `ErrItemInfoSourceRequired`.

### `items`

Conflict target:

```text
kind, brand, lang, code, item_info_id, rarity, edition, extended_art, set_id
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

It is not a database column. Because `items` also contains sets and accessories,
always filter by kind when loading it:

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

If an accessory belongs to a set and sets `SetID`, it would also match
`set_id = set.ID`. The `kind = CARD` filter is what keeps `CardsPrinted`
semantically correct.

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
if err := service.UpsertCardPrinted(ctx, []catalog.CardPrinted{*print}); err != nil {
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
