# internal/infrastructure

Adapters for the application ports.

```text
catprov/
  dragonshield/   Dragon Shield sleeve scraper
  ygo/            Yu-Gi-Oh! provider, checkpoint and scrapers
persistence/
  json/           retrier document and provider-data sidecars
  sql/postgresql/catalog/  itemModel, catalog repository and migration
```

The catalog adapter writes only global rows (`store_id IS NULL`). Store-owned
rows share the `items` table and are managed by the store service.

## Providers

Both providers implement `ports.CatalogProvider` and return flat
`[]domain.Item` slices. Provider-specific parsing stays inside each package;
transport and retry logic comes from `internal/utility/retrier`.

- [Yu-Gi-Oh!](catprov/ygo/README.md)
- [Dragon Shield](catprov/dragonshield/README.md)

## Persistence

- [Persistence overview](persistence/README.md)
- [JSON retrier store](persistence/json/README.md)
- [PostgreSQL catalog](persistence/sql/postgresql/catalog/README.md)
