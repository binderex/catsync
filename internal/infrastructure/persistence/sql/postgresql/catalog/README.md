# internal/infrastructure/persistence/sql/postgresql/catalog

PostgreSQL adapter for the denormalized catalog.

## Table

- one `items` table for sets, cards, sleeves and playmats;
- `domain.Item` is the only model;
- no `item_infos` and no `user_id`.

Identity index:

```text
(kind, brand, lang, code, set_id, rarity, edition,
 extended_art, source, variant_key)
NULLS NOT DISTINCT
```

`variant_key` is generated from `properties->'variant'`.

Additional constraints and cleanup:

- `items_source_not_empty`: `CHECK (source <> '')`;
- migration deletes `unknown` names;
- migration deletes tokens, strategy, tip, checklist, skill and non-game cards.

## Upsert

`Repository.Upsert`:

1. splits sets and other items;
2. inserts/updates sets in batches of 250;
3. resolves persisted set IDs and attaches them to prints;
4. inserts/updates the remaining items;
5. propagates `verified_source` to every row that shares a `source`.

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

`Repository.Delete` removes sets that had cards but no regular card left. It
also removes any row that still references those sets.

## Migration

`Open(dsn)` connects, configures the pool, runs `Migrate` and returns the GORM
handle. The migration is idempotent and does not backfill old data.
