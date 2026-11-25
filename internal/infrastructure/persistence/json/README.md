# internal/infrastructure/persistence/json

Single-file retrier store.

## Format

`data/retrier.json`:

```json
{
  "version": 2,
  "runs": {
    "ygo-all-20261120T185351Z": {
      "version": 2,
      "run_id": "ygo-all-20261120T185351Z",
      "provider": "ygo",
      "mode": "all",
      "state": "success",
      "started_at": "...",
      "updated_at": "...",
      "progress": {},
      "order": [],
      "states": {},
      "provider_data": {}
    }
  }
}
```

`FileRepository` implements `ports.RetrierRunRepository`:

- `Save` merges changed states and rewrites the document atomically;
- `Load` reads one run;
- `List` filters by provider, mode and state;
- `Delete` removes a run.

`provider_data` holds the provider-specific crawl snapshot (for example the
parsed YGO sets and prints). It is written through
`RetrierPersistence.SaveProviderData`.

## Writing

The whole document is written to a temporary file and renamed into place.
On Windows an existing file is removed before the rename.

## Tests

```powershell
go test ./internal/infrastructure/persistence/json
```
