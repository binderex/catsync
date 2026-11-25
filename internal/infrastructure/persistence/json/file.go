package jsonstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/domain"
)

const fileDocumentVersion = 2

// FileRepository stores every retrier run in one JSON document. The document
// is small enough for the catalog sync and avoids the per-state writes of a
// relational store.
type FileRepository struct {
	path string
	mu   sync.Mutex
}

type fileDocument struct {
	Version int                                       `json:"version"`
	Runs    map[domain.RetrierRunID]domain.RetrierRun `json:"runs"`
}

// NewFile creates a single-file JSON repository. An empty path uses
// "data/retrier.json".
func NewFile(path string) *FileRepository {
	if strings.TrimSpace(path) == "" {
		path = filepath.Join("data", "retrier.json")
	}
	return &FileRepository{path: path}
}

// Path returns the JSON document location.
func (r *FileRepository) Path() string {
	return r.path
}

// Save writes a run snapshot. changed is merged into run.States; the full
// document is rewritten atomically because JSON has a single file.
func (r *FileRepository) Save(ctx context.Context, run domain.RetrierRun, changed []domain.URLState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run.ID == "" {
		return fmt.Errorf("save retrier run: empty run id")
	}

	if run.Version == 0 {
		run.Version = fileDocumentVersion
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now().UTC()
	}
	if run.States == nil {
		run.States = make(map[string]domain.URLState, len(changed))
	}
	for _, state := range changed {
		run.States[state.URL] = state
	}
	if run.Progress.Total == 0 && len(run.States) > 0 {
		run.Progress = domain.ProgressFromStates(run.States)
	}
	if len(run.Failures) == 0 && len(run.States) > 0 {
		run.Failures = domain.FailuresFromStates(run.States)
	}
	if len(run.Order) == 0 && len(run.States) > 0 {
		run.Order = sortedURLs(run.States)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	document, err := r.read()
	if err != nil {
		return err
	}
	document.Runs[run.ID] = run

	return r.write(document)
}

// Replace rewrites the document with exactly the provided runs. It is used by
// maintenance tools that rebuild the state from another source.
func (r *FileRepository) Replace(ctx context.Context, runs []domain.RetrierRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	document := fileDocument{
		Version: fileDocumentVersion,
		Runs:    make(map[domain.RetrierRunID]domain.RetrierRun, len(runs)),
	}

	for _, run := range runs {
		if run.ID == "" {
			return fmt.Errorf("replace retrier runs: empty run id")
		}
		if run.Version == 0 {
			run.Version = fileDocumentVersion
		}
		if run.StartedAt.IsZero() {
			run.StartedAt = time.Now().UTC()
		}
		if run.UpdatedAt.IsZero() {
			run.UpdatedAt = time.Now().UTC()
		}
		document.Runs[run.ID] = run
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.write(document)
}

// Load reads a run by id.
func (r *FileRepository) Load(ctx context.Context, id domain.RetrierRunID) (domain.RetrierRun, error) {
	if err := ctx.Err(); err != nil {
		return domain.RetrierRun{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	document, err := r.read()
	if err != nil {
		return domain.RetrierRun{}, err
	}

	run, ok := document.Runs[id]
	if !ok {
		return domain.RetrierRun{}, fmt.Errorf("%w: %s", domain.ErrRunNotFound, id)
	}
	return run, nil
}

// List returns the run summaries that match filter.
func (r *FileRepository) List(ctx context.Context, filter domain.RetrierRunFilter) ([]domain.RetrierRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	document, err := r.read()
	if err != nil {
		return nil, err
	}

	summaries := make([]domain.RetrierRunSummary, 0, len(document.Runs))
	for _, run := range document.Runs {
		if filter.Provider != "" && run.Provider != filter.Provider {
			continue
		}
		if filter.Mode != "" && run.Mode != filter.Mode {
			continue
		}
		if filter.State != "" && run.State != filter.State {
			continue
		}

		progress := run.Progress
		if progress.Total == 0 && len(run.States) > 0 {
			progress = domain.ProgressFromStates(run.States)
		}

		summaries = append(summaries, domain.RetrierRunSummary{
			Version:    run.Version,
			ID:         run.ID,
			Provider:   run.Provider,
			Mode:       run.Mode,
			State:      run.State,
			StartedAt:  run.StartedAt,
			UpdatedAt:  run.UpdatedAt,
			FinishedAt: run.FinishedAt,
			Duration:   run.Duration,
			Error:      run.Error,
			Progress:   progress,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	start := filter.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(summaries) {
		return nil, nil
	}
	end := len(summaries)
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}

	return summaries[start:end], nil
}

// Delete removes a run from the document.
func (r *FileRepository) Delete(ctx context.Context, id domain.RetrierRunID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	document, err := r.read()
	if err != nil {
		return err
	}
	if _, ok := document.Runs[id]; !ok {
		return fmt.Errorf("%w: %s", domain.ErrRunNotFound, id)
	}
	delete(document.Runs, id)

	return r.write(document)
}

func (r *FileRepository) read() (fileDocument, error) {
	document := fileDocument{
		Version: fileDocumentVersion,
		Runs:    make(map[domain.RetrierRunID]domain.RetrierRun),
	}

	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return fileDocument{}, fmt.Errorf("read retrier json: %w", err)
	}
	if len(data) == 0 {
		return document, nil
	}

	if err := json.Unmarshal(data, &document); err != nil {
		return fileDocument{}, fmt.Errorf("decode retrier json %s: %w", r.path, err)
	}
	if document.Runs == nil {
		document.Runs = make(map[domain.RetrierRunID]domain.RetrierRun)
	}
	if document.Version == 0 {
		document.Version = fileDocumentVersion
	}

	return document, nil
}

func (r *FileRepository) write(document fileDocument) error {
	document.Version = fileDocumentVersion
	if document.Runs == nil {
		document.Runs = make(map[domain.RetrierRunID]domain.RetrierRun)
	}

	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode retrier json: %w", err)
	}

	return writeAtomic(r.path, data)
}

func sortedURLs(states map[string]domain.URLState) []string {
	urls := make([]string, 0, len(states))
	for url := range states {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	return urls
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create retrier directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".retrier-json-*.tmp")
	if err != nil {
		return fmt.Errorf("create retrier temporary file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write retrier file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close retrier file: %w", err)
	}

	if err := os.Rename(tempName, path); err != nil {
		// Windows does not replace an existing file with Rename.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace retrier file: %w", err)
		}
		if renameErr := os.Rename(tempName, path); renameErr != nil {
			return fmt.Errorf("replace retrier file: %w", renameErr)
		}
	}

	return nil
}

// compile-time check.
var _ ports.RetrierRunRepository = (*FileRepository)(nil)
