# internal/application/usecases

Catalog and retrier workflows.

## CatalogUseCase

`Upsert(ctx, items)` is the only write path to the catalog repository.

Flow:

1. compute which sets have cards and which have regular cards;
2. drop `unknown` names;
3. drop non-standard cards (tokens, strategy, tip, checklist, skill, non-game,
   and anything without `Monster` / `Spell` / `Trap`);
4. drop sets that had cards but no regular card left;
5. assign IDs, `Properties` and `UpdatedAt` (`prepareItems`);
6. deduplicate identities (`domain.DeduplicateItems`);
7. reject items with `StoreID` set (Catsync is global-only);
8. reject empty `source` values;
9. upsert the unique rows and delete pruned sets.

## CatalogProviderUseCase

Orchestrates one provider:

1. optionally loads the latest unfinished retrier run for provider + mode;
2. injects it as `ProviderOptions.ResumeState`;
3. calls `GetNewReleases` or `GetAllReleases`;
4. upserts partial results even when the crawl is interrupted;
5. returns the number of persisted rows.

`-resume=false` skips the retrier lookup.

## RetrierPersistenceUseCase

Implements the retrier observer and the persistence port:

- `RunStarted` creates or updates the run header;
- `RunStateChanged` debounces intermediate saves;
- `RunFinished` writes the final snapshot;
- `LoadLatest` returns the newest resumable run for a provider and mode;
- `SaveProviderData` attaches the parsed provider snapshot to the active or
  latest run for a provider and mode.

The use case receives a `ports.RetrierRunRepository`; the JSON file adapter is
injected by `cmd/sync`.

## Tests

```powershell
go test ./internal/application/usecases -short
```
