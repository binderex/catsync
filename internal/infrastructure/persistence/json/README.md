# internal/infrastructure/persistence/json

Retrier store backed by a JSON document plus provider-data sidecars.

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
      "states": {}
    }
  }
}
```

The provider-specific crawl snapshot is stored per run in
`data/retrier.json.d/<run-id>.json` and is exposed again as
`RetrierRun.ProviderData` by `Load`. Keeping it out of the main document means a
periodic URL-progress save only rewrites the small run document, not the whole
catalog snapshot.

`FileRepository` implements `ports.RetrierRunRepository`:

- `Save` merges changed states and rewrites the document atomically. A non-nil
  `ProviderData` rewrites the run sidecar; a nil `ProviderData` keeps the
  existing sidecar.
- `Load` reads the run document and attaches its provider sidecar.
- `List` filters by provider, mode and state.
- `Delete` removes the run and its sidecar.

The provider snapshot is written through
`RetrierPersistence.SaveProviderData`. Older documents that still contain an
inline `provider_data` field are migrated to sidecars the next time the
document is saved.

## Writing

The main document and each sidecar are written to a temporary file and renamed
into place. On Windows an existing file is removed before the rename.

## Tests

```powershell
go test ./internal/infrastructure/persistence/json
```
