# Catsync

Catsync is the catalog synchronization service for **Binderex**. It keeps the
Binderex trading-card catalog up to date with data from external providers and
persists that data in PostgreSQL.

## What Catsync does

- Synchronizes trading-card catalog data: cards, prints and sets.
- Normalizes data from different providers into a single catalog model.
- Keeps provider-specific logic isolated from the rest of the application.
- Deduplicates catalog entries and keeps the database updated.
- Maintains the catalog database schema.

## Providers

| Provider | Status | Data sources |
|---|---|---|
| Yu-Gi-Oh! | Implemented | YGOPRODeck + Yugipedia |
| Pokémon | Planned | TBD |

## Repository responsibilities

- **Catalog domain and persistence:** catalog model, identity rules and upserts.
- **Provider integrations:** fetching and normalizing external catalog data.
- **Database:** PostgreSQL connection and schema migrations.
- **Synchronization entry point:** command-line service for catalog sync jobs.

Provider implementation details are documented in
[`internal/catprov/README.md`](internal/catprov/README.md).
