# internal/infrastructure/persistence

Storage adapters.

| Adapter | Used for |
| --- | --- |
| `json` | Retrier run state in one `data/retrier.json` document. |
| `sql/postgresql/catalog` | Denormalized catalog rows in PostgreSQL. |

The retrier is a single JSON file because the sync needs only one writer and
the document is easy to inspect. The catalog is relational because it is
queried by the API and needs a unique identity index.

## Docs

- [JSON store](json/README.md)
- [PostgreSQL catalog](sql/postgresql/catalog/README.md)
