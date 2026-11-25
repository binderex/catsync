package ports

import (
	"context"
	"encoding/json"

	"github.com/binderex/catsync/internal/domain"
)

// RetrierRunRepository stores live retrier run snapshots. The concrete
// implementation decides where (JSON file, database, ...); the use case
// receives the implementation, it never selects one with a switch.
type RetrierRunRepository interface {
	Save(ctx context.Context, run domain.RetrierRun, changed []domain.URLState) error
	Load(ctx context.Context, id domain.RetrierRunID) (domain.RetrierRun, error)
	List(ctx context.Context, filter domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error)
	Delete(ctx context.Context, id domain.RetrierRunID) error
}

// RetrierPersistence is the domain-facing persistence of retrier runs. It is
// what the catalog providers receive to save their crawl and resume it
// automatically. The run observer methods let it be attached directly to the
// retrier utility.
type RetrierPersistence interface {
	RunStarted(ctx context.Context, run domain.RetrierRun) (domain.RetrierRunID, error)
	RunStateChanged(ctx context.Context, runID domain.RetrierRunID, state domain.URLState) error
	RunFinished(ctx context.Context, run domain.RetrierRun) error

	Save(ctx context.Context, run domain.RetrierRun) error
	Load(ctx context.Context, id domain.RetrierRunID) (domain.RetrierRun, error)
	LoadLatest(ctx context.Context, provider, mode string) (domain.RetrierRun, bool, error)
	List(ctx context.Context, filter domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error)
	Delete(ctx context.Context, id domain.RetrierRunID) error

	// SaveProviderData attaches a provider-specific crawl snapshot to the
	// active or latest run for provider+mode.
	SaveProviderData(ctx context.Context, provider, mode string, data json.RawMessage) error
}
