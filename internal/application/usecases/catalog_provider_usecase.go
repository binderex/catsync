package usecases

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
)

// CatalogProviderUseCase orchestrates one catalog provider: it loads the latest
// unfinished retrier run, lets the provider resume it, and upserts the result
// through the catalog use case.
type CatalogProviderUseCase struct {
	provider    ports.CatalogProvider
	catalog     *CatalogUseCase
	persistence ports.RetrierPersistence
}

// NewCatalogProviderUseCase creates a provider use case. persistence may be nil
// to disable resuming.
func NewCatalogProviderUseCase(
	provider ports.CatalogProvider,
	catalog *CatalogUseCase,
	persistence ports.RetrierPersistence,
) *CatalogProviderUseCase {
	return &CatalogProviderUseCase{
		provider:    provider,
		catalog:     catalog,
		persistence: persistence,
	}
}

// GetNewReleases fetches the new releases and upserts them.
func (uc *CatalogProviderUseCase) GetNewReleases(
	ctx context.Context,
	opts domain.ProviderOptions,
) (int, error) {
	return uc.run(ctx, "new", opts, uc.provider.GetNewReleases)
}

// GetAllReleases fetches every release and upserts it.
func (uc *CatalogProviderUseCase) GetAllReleases(
	ctx context.Context,
	opts domain.ProviderOptions,
) (int, error) {
	return uc.run(ctx, "all", opts, uc.provider.GetAllReleases)
}

type fetchFunc func(ctx context.Context, opts domain.ProviderOptions) ([]domain.Item, error)

func (uc *CatalogProviderUseCase) run(
	ctx context.Context,
	mode string,
	opts domain.ProviderOptions,
	fetch fetchFunc,
) (int, error) {
	if uc.provider == nil {
		return 0, fmt.Errorf("catalog provider is not configured")
	}
	if uc.catalog == nil {
		return 0, fmt.Errorf("catalog use case is not configured")
	}

	opts.Mode = mode

	if uc.persistence != nil && opts.Resume {
		log.Printf("%s: loading latest unfinished %s run", uc.provider.Name(), mode)
		run, found, err := uc.persistence.LoadLatest(ctx, uc.provider.Name(), mode)
		switch {
		case errors.Is(err, domain.ErrPersistenceDisabled):
			// Persistence is optional; crawl without resuming.
		case err != nil:
			return 0, fmt.Errorf("load latest retrier run: %w", err)
		case found:
			log.Printf("%s: resuming run %s", uc.provider.Name(), run.ID)
			opts.ResumeState = &run
		default:
			log.Printf("%s: no unfinished run; starting fresh", uc.provider.Name())
		}
	}

	items, fetchErr := fetch(ctx, opts)
	log.Printf("%s: fetched %d items", uc.provider.Name(), len(items))

	count := 0
	var upsertErr error
	if len(items) > 0 {
		upsertCtx := ctx
		if fetchErr != nil && (errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded)) {
			// Persist the partial crawl even when the user interrupted it.
			upsertCtx = context.WithoutCancel(ctx)
		}
		count, upsertErr = uc.catalog.Upsert(upsertCtx, items)
	}

	switch {
	case fetchErr != nil && upsertErr != nil:
		return count, errors.Join(fetchErr, upsertErr)
	case fetchErr != nil:
		return count, fetchErr
	default:
		return count, upsertErr
	}
}
