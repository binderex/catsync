# Catsync

Catsync is the catalog synchronization service for **Binderex**. It keeps a
trading-card catalog aligned with external providers and stores the normalized
data in PostgreSQL.

## What Catsync is

Catsync is the data ingestion layer between external trading-card sources and
the Binderex catalog.

It is responsible for:

- discovering cards, prints and sets from external providers;
- normalizing provider-specific data into one catalog model;
- resolving shared card and set information;
- deduplicating catalog entries through stable identities;
- keeping the catalog database schema and data up to date.

## What it synchronizes

- **Cards and prints:** card identities, localized names, images, rarities,
  editions, release dates and set membership.
- **Sets:** localized set data, release dates, set codes, product breakdowns
  and card lists.
- **Products:** sleeves and other catalog items that do not necessarily share
  card or set information.

## Providers

| Provider | Capability | Status |
| --- | --- | --- |
| Yu-Gi-Oh! TCG/OCG | Cards, prints, sets | Implemented |
| Yu-Gi-Oh! Rush Duel | Cards, prints, sets | Implemented |
| Dragon Shield | Sleeves | Implemented |
| Pokémon | Cards, prints, sets | Planned |

Provider-specific behavior stays inside each provider package. The catalog
model and persistence rules are shared by all providers.

## Main areas

- **Provider integrations:** fetch external data and normalize it.
- **Catalog domain:** model, identities, repository and upsert rules.
- **Database:** PostgreSQL connection and migrations.
- **Synchronization commands:** entry points for scheduled catalog jobs.

## Documentation

- [Catalog model and persistence](internal/catalog/README.md)
- [Yu-Gi-Oh! provider](internal/catprov/ygo/README.md)
- [Dragon Shield provider](internal/catprov/dragonshield/README.md)
- [Retry and run reports](internal/retrier/README.md)
- [Synchronization commands](cmd/sync/README.md)
