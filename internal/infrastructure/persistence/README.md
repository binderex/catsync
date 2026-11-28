# internal/infrastructure/persistence

Storage adapters.

| Adapter | Used for |
| --- | --- |
| `json` | Retrier run state in `data/retrier.json` plus provider sidecars in `data/retrier.json.d/`. |
| `sql/postgresql/catalog` | Denormalized global catalog rows in PostgreSQL; the same `items` table can hold store-owned rows with `store_id`. |

The retrier uses one JSON document for run state because the sync needs only
one writer and the document is easy to inspect. Provider snapshots live in
per-run sidecar files so progress saves stay small. The catalog is relational
because it is queried by the API and needs a unique identity index.

Catsync writes only global rows (`store_id IS NULL`). Store-owned rows share
the table and are managed by the other service; see the
[PostgreSQL catalog](sql/postgresql/catalog/README.md) for the scope rules.

## Docs

- [JSON store](json/README.md)
- [PostgreSQL catalog](sql/postgresql/catalog/README.md)
