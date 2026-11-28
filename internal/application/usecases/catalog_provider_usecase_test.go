package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
)

type fakeCatalogProvider struct {
	newItems []domain.Item
	allItems []domain.Item
	newErr   error
	allErr   error
	newCalls int
	allCalls int
	newOpts  domain.ProviderOptions
	allOpts  domain.ProviderOptions
}

func (p *fakeCatalogProvider) Name() string {
	return "fake"
}

func (p *fakeCatalogProvider) GetNewReleases(
	_ context.Context,
	opts domain.ProviderOptions,
) ([]domain.Item, error) {
	p.newCalls++
	p.newOpts = opts
	return p.newItems, p.newErr
}

func (p *fakeCatalogProvider) GetAllReleases(
	_ context.Context,
	opts domain.ProviderOptions,
) ([]domain.Item, error) {
	p.allCalls++
	p.allOpts = opts
	return p.allItems, p.allErr
}

var _ ports.CatalogProvider = (*fakeCatalogProvider)(nil)

type countingCatalogRepository struct {
	upserts []domain.Item
}

func (r *countingCatalogRepository) Upsert(_ context.Context, items []domain.Item) error {
	r.upserts = append(r.upserts, items...)
	return nil
}

func (r *countingCatalogRepository) Delete(_ context.Context, _ []string) error {
	return nil
}

type fakePersistence struct {
	run   domain.RetrierRun
	found bool
	err   error
	saves []domain.RetrierRun
}

func (p *fakePersistence) RunStarted(context.Context, domain.RetrierRun) (domain.RetrierRunID, error) {
	return p.run.ID, nil
}

func (p *fakePersistence) RunStateChanged(context.Context, domain.RetrierRunID, domain.URLState) error {
	return nil
}

func (p *fakePersistence) RunFinished(context.Context, domain.RetrierRun) error {
	return nil
}

func (p *fakePersistence) Save(_ context.Context, run domain.RetrierRun) error {
	if p.err != nil {
		return p.err
	}
	if run.ProviderData == nil {
		run.ProviderData = p.run.ProviderData
	}
	p.run = run
	p.saves = append(p.saves, run)
	return nil
}

func (p *fakePersistence) Load(context.Context, domain.RetrierRunID) (domain.RetrierRun, error) {
	return p.run, p.err
}

func (p *fakePersistence) LoadLatest(context.Context, string, string) (domain.RetrierRun, bool, error) {
	return p.run, p.found, p.err
}

func (p *fakePersistence) List(context.Context, domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.run.ID == "" {
		return nil, nil
	}
	return []domain.RetrierRunSummary{{
		ID:       p.run.ID,
		Provider: p.run.Provider,
		Mode:     p.run.Mode,
		State:    p.run.State,
	}}, nil
}

func (p *fakePersistence) Delete(context.Context, domain.RetrierRunID) error {
	return p.err
}

func (p *fakePersistence) SaveProviderData(_ context.Context, _, _ string, data json.RawMessage) error {
	if p.err != nil {
		return p.err
	}
	p.run.ProviderData = data
	return nil
}

var _ ports.RetrierPersistence = (*fakePersistence)(nil)

func TestCatalogProviderUseCaseCountsAndUpserts(t *testing.T) {
	provider := &fakeCatalogProvider{
		newItems: []domain.Item{
			{Kind: domain.KindSet, Source: "https://example.com/set"},
			{
				Kind:   domain.KindCard,
				Source: "https://example.com/card",
				Properties: map[string]any{
					domain.PropertyCardTypes: "Monster Normal",
				},
			},
		},
		allItems: []domain.Item{{Kind: domain.KindSleeve, Source: "https://example.com/sleeve"}},
	}
	repo := &countingCatalogRepository{}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), nil)

	count, err := uc.GetNewReleases(context.Background(), domain.ProviderOptions{})
	if err != nil {
		t.Fatalf("GetNewReleases: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if provider.newCalls != 1 || provider.allCalls != 0 {
		t.Errorf("calls = new %d all %d", provider.newCalls, provider.allCalls)
	}

	count, err = uc.GetAllReleases(context.Background(), domain.ProviderOptions{})
	if err != nil {
		t.Fatalf("GetAllReleases: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if provider.allCalls != 1 {
		t.Errorf("allCalls = %d", provider.allCalls)
	}
	if len(repo.upserts) != 3 {
		t.Errorf("upserts = %d, want 3", len(repo.upserts))
	}
}

func TestCatalogProviderUseCasePropagatesErrors(t *testing.T) {
	provider := &fakeCatalogProvider{newErr: errors.New("provider failed")}
	repo := &countingCatalogRepository{}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), nil)

	if _, err := uc.GetNewReleases(context.Background(), domain.ProviderOptions{}); err == nil {
		t.Fatal("expected provider error")
	}
	if len(repo.upserts) != 0 {
		t.Fatal("repository must not be called when the provider fails")
	}
}

func TestCatalogProviderUseCaseSkipsPartialNewUpsert(t *testing.T) {
	provider := &fakeCatalogProvider{
		newItems: []domain.Item{{
			Kind:   domain.KindSleeve,
			Brand:  domain.BrandDS,
			Lang:   domain.LangEN,
			Code:   "SKU-1",
			Source: "https://example.com/sleeve",
		}},
		newErr: context.Canceled,
	}
	repo := &countingCatalogRepository{}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), nil)

	_, err := uc.GetNewReleases(context.Background(), domain.ProviderOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("partial new items were upserted: %d", len(repo.upserts))
	}
}

func TestCatalogProviderUseCaseMarksIncompleteRunResumable(t *testing.T) {
	provider := &fakeCatalogProvider{
		newItems: []domain.Item{{
			Kind:   domain.KindSet,
			Source: "https://example.com/set",
		}},
		newErr: domain.ErrCrawlIncomplete,
	}
	repo := &countingCatalogRepository{}
	persistence := &fakePersistence{
		found: true,
		run: domain.RetrierRun{
			Version:      2,
			ID:           "run-1",
			Provider:     "fake",
			Mode:         "new",
			State:        domain.RetrierRunSuccess,
			ProviderData: json.RawMessage(`{"complete":false}`),
		},
	}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), persistence)

	_, err := uc.GetNewReleases(context.Background(), domain.ProviderOptions{Resume: true})
	if !errors.Is(err, domain.ErrCrawlIncomplete) {
		t.Fatalf("error = %v, want ErrCrawlIncomplete", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("incomplete items were upserted: %d", len(repo.upserts))
	}
	if persistence.run.State != domain.RetrierRunInterrupted {
		t.Fatalf("run state = %q, want interrupted", persistence.run.State)
	}
	if string(persistence.run.ProviderData) != `{"complete":false}` {
		t.Fatalf("provider data = %s", persistence.run.ProviderData)
	}
}

func TestCatalogProviderUseCaseSkipsPartialAllUpsert(t *testing.T) {
	provider := &fakeCatalogProvider{
		allItems: []domain.Item{{
			Kind:   domain.KindSleeve,
			Brand:  domain.BrandDS,
			Lang:   domain.LangEN,
			Code:   "SKU-1",
			Source: "https://example.com/sleeve",
		}},
		allErr: context.Canceled,
	}
	repo := &countingCatalogRepository{}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), nil)

	_, err := uc.GetAllReleases(context.Background(), domain.ProviderOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("partial all items were upserted: %d", len(repo.upserts))
	}
}

func TestCatalogProviderUseCaseInjectsResumeState(t *testing.T) {
	provider := &fakeCatalogProvider{newItems: []domain.Item{{Kind: domain.KindSet, Source: "https://example.com/set"}}}
	repo := &countingCatalogRepository{}
	persistence := &fakePersistence{
		found: true,
		run: domain.RetrierRun{
			ID:    "run-1",
			State: domain.RetrierRunCancelled,
			Order: []string{"http://pending"},
			States: map[string]domain.URLState{
				"http://pending": {URL: "http://pending"},
			},
		},
	}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), persistence)

	if _, err := uc.GetNewReleases(context.Background(), domain.ProviderOptions{Resume: true}); err != nil {
		t.Fatalf("GetNewReleases: %v", err)
	}

	if provider.newOpts.Mode != "new" {
		t.Errorf("mode = %q, want new", provider.newOpts.Mode)
	}
	if provider.newOpts.ResumeState == nil {
		t.Fatal("resume state was not injected")
	}
	if provider.newOpts.ResumeState.ID != "run-1" {
		t.Errorf("resume run = %q, want run-1", provider.newOpts.ResumeState.ID)
	}
}

func TestCatalogProviderUseCaseSkipsResumeWhenNoRun(t *testing.T) {
	provider := &fakeCatalogProvider{allItems: []domain.Item{{Kind: domain.KindSet, Source: "https://example.com/set"}}}
	repo := &countingCatalogRepository{}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), &fakePersistence{})

	if _, err := uc.GetAllReleases(context.Background(), domain.ProviderOptions{Resume: true}); err != nil {
		t.Fatalf("GetAllReleases: %v", err)
	}

	if provider.allOpts.Mode != "all" {
		t.Errorf("mode = %q, want all", provider.allOpts.Mode)
	}
	if provider.allOpts.ResumeState != nil {
		t.Errorf("resume state = %+v, want nil", provider.allOpts.ResumeState)
	}
}

func TestCatalogProviderUseCaseResumeFalseSkipsPersistence(t *testing.T) {
	provider := &fakeCatalogProvider{allItems: []domain.Item{{Kind: domain.KindSet, Source: "https://example.com/set"}}}
	repo := &countingCatalogRepository{}
	persistence := &fakePersistence{
		found: true,
		run:   domain.RetrierRun{ID: "run-1"},
	}
	uc := NewCatalogProviderUseCase(provider, NewCatalogUseCase(repo), persistence)

	if _, err := uc.GetAllReleases(context.Background(), domain.ProviderOptions{}); err != nil {
		t.Fatalf("GetAllReleases: %v", err)
	}

	if provider.allOpts.ResumeState != nil {
		t.Errorf("resume state = %+v, want nil when Resume is false", provider.allOpts.ResumeState)
	}
}
