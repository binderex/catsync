# internal/infrastructure/persistence/sql/postgresql/catalog

PostgreSQL adapter for the denormalized catalog.

## Table

- one `items` table for sets, cards, sleeves and playmats;
- `store_id` is nullable: `NULL` means a global catalog item (the only rows
  Catsync writes), non-null means a store-owned item;
- `stores` is a minimal seed table (`id`, `name`, timestamps) and `items.store_id`
  has an FK to it;
- `domain.Item` is the catalog entity; its JSON tags belong to the retrier
  snapshot and it has no GORM tags;
- `itemModel` is the GORM model for `items`; it owns the persistence types and
  GORM tags;
- no `item_infos` and no `user_id`.

`domain.Item.ID`, `SetID` and `StoreID` are strings. `itemModel` converts them
to `uuid.UUID` and rejects invalid values before writing.

### Store seed

`Migrate` creates the minimal store table with `IF NOT EXISTS` so the FK can be
added even when the store service has not created anything yet:

```sql
CREATE TABLE IF NOT EXISTS stores (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
```

The store service can extend that table later. `items.store_id` is nullable and
references `stores(id)` with `ON DELETE CASCADE`.

Identity index:

```text
(store_id, kind, brand, lang, code, set_id, rarity, edition,
 extended_art, source, variant_key)
NULLS NOT DISTINCT
```

`variant_key` is generated from `properties->'variant'`. Global rows are unique
among themselves; each store has its own uniqueness scope.

Additional constraints and cleanup:

- `items_source_not_empty`: `CHECK (store_id IS NOT NULL OR source <> '')`;
- migration deletes `unknown` names only for `store_id IS NULL`;
- migration deletes tokens, strategy, tip, checklist, skill and non-game cards
  only for `store_id IS NULL`;
- orphan-card cleanup only considers global cards and sets.

## Upsert

`Repository.Upsert`:

1. splits sets and other items;
2. inserts/updates sets in batches of 250;
3. resolves persisted set IDs and attaches them to prints;
4. inserts/updates the remaining items;
5. propagates `verified_source` to every global row that shares a `source`.

Every batch commits on its own. There is no transaction around the whole
catalog: a remote database or a connection pooler can take minutes to write all
the rows, and a single long transaction hides progress, holds locks and rolls
everything back when it times out. With independent commits the rows become
visible while the sync runs and a failure keeps the batches that already
finished. The sync is idempotent, so rerunning it completes the rest.

Only mutable columns are updated:

```text
name, description, image, release_at, properties,
verified_source, updated_at
```

The use case must send unique, normalized items.

## Delete

`Repository.Delete` removes global sets that had cards but no regular card left.
It also removes global rows that still reference those sets. Store-owned rows
are never touched.

## Migration

`Open(dsn)` connects, configures the pool, runs `Migrate` and returns the GORM
handle. The migration is idempotent and does not backfill old data. Existing
rows are global because their `store_id` is `NULL`.
