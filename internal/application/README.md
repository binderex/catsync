# internal/application

Application layer: ports (interfaces) and use cases. It depends on `domain`
only; adapters are injected from `cmd/sync`.

## Ports

- `CatalogRepository`: upsert catalog items and delete pruned sets.
- `CatalogProvider`: provider contract (`Name`, `GetNewReleases`,
  `GetAllReleases`).
- `RetrierRunRepository`: storage of retrier run snapshots.
- `RetrierPersistence`: domain-facing retrier persistence plus the run-observer
  methods used by the retrier utility.

## Use cases

| Use case | Responsibility |
| --- | --- |
| `CatalogUseCase` | Filter, prune, normalize and upsert catalog items. |
| `CatalogProviderUseCase` | Load the unfinished retrier run, resume the provider, upsert the result. |
| `RetrierPersistenceUseCase` | Save run progress, debounce writes and load resumable runs. |

See the package READMEs for details:

- [ports](ports/README.md)
- [usecases](usecases/README.md)
