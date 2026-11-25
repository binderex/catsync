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

func (p *fakePersistence) Save(context.Context, domain.RetrierRun) error {
	return nil
}

func (p *fakePersistence) Load(context.Context, domain.RetrierRunID) (domain.RetrierRun, error) {
	return p.run, p.err
}

func (p *fakePersistence) LoadLatest(context.Context, string, string) (domain.RetrierRun, bool, error) {
	return p.run, p.found, p.err
}

func (p *fakePersistence) List(context.Context, domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error) {
	return nil, p.err
}

func (p *fakePersistence) Delete(context.Context, domain.RetrierRunID) error {
	return p.err
}

func (p *fakePersistence) SaveProviderData(context.Context, string, string, json.RawMessage) error {
	return p.err
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
