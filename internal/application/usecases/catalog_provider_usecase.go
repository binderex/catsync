package usecases

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

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
			log.Printf("%s: no unfinished run found", uc.provider.Name())
		}
	}

	items, fetchErr := fetch(ctx, opts)
	log.Printf("%s: fetched %d items", uc.provider.Name(), len(items))

	if errors.Is(fetchErr, domain.ErrCrawlIncomplete) {
		uc.markRunIncomplete(ctx, uc.provider.Name(), mode, opts.ResumeState)
	}

	shouldUpsert := len(items) > 0 && fetchErr == nil
	if len(items) > 0 && fetchErr != nil {
		// Partial progress stays in the retrier snapshot. Only a complete
		// crawl may write to the catalog.
		log.Printf("%s: crawl incomplete; skipping upsert", uc.provider.Name())
	}

	count := 0
	var upsertErr error
	if shouldUpsert {
		count, upsertErr = uc.catalog.Upsert(ctx, items)
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

// markRunIncomplete makes an otherwise successful retrier run resumable when
// the provider still has pending sets or prints. It runs outside the cancelled
// crawl context because the provider has already finished.
func (uc *CatalogProviderUseCase) markRunIncomplete(
	ctx context.Context,
	provider, mode string,
	resume *domain.RetrierRun,
) {
	if uc.persistence == nil {
		return
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()

	var (
		run domain.RetrierRun
		err error
	)
	if resume != nil && resume.ID != "" {
		run, err = uc.persistence.Load(saveCtx, resume.ID)
	} else {
		summaries, listErr := uc.persistence.List(saveCtx, domain.RetrierRunFilter{
			Provider: provider,
			Mode:     mode,
		})
		if listErr != nil {
			log.Printf("%s: locate incomplete run: %v", provider, listErr)
			return
		}
		if len(summaries) == 0 {
			return
		}
		run, err = uc.persistence.Load(saveCtx, summaries[0].ID)
	}
	if err != nil {
		log.Printf("%s: load incomplete run: %v", provider, err)
		return
	}

	switch run.State {
	case domain.RetrierRunRunning, domain.RetrierRunSuccess, domain.RetrierRunFailed:
		run.State = domain.RetrierRunInterrupted
		run.FinishedAt = nil
		run.UpdatedAt = time.Now().UTC()
		// A nil provider snapshot means "keep the stored one"; this update only
		// changes the run state and must not rewrite the large snapshot.
		run.ProviderData = nil
		if err := uc.persistence.Save(saveCtx, run); err != nil {
			log.Printf("%s: mark run incomplete: %v", provider, err)
		}
	}
}
