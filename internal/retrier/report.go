package retrier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultReportDir = "reports"
	reportVersion    = 1
)

const (
	// ReportSuccess means Run finished without an error.
	ReportSuccess = "success"
	// ReportCancelled means Run was stopped by context cancellation.
	ReportCancelled = "cancelled"
	// ReportFailed means Run returned a non-cancellation error.
	ReportFailed = "failed"
)

// ErrQueueNotEmpty is returned by Continue when the retrier already has URLs.
var ErrQueueNotEmpty = errors.New("retrier queue is not empty")

// FilterResolver returns the filter for a pending URL loaded from a report.
// Completed, failed, invalid and not-found URLs are not resolved again.
type FilterResolver func(URLState) Filter

// RunReport is the final result of one Run. States and Order are enough to
// rebuild the queue with Continue.
type RunReport struct {
	Version  int    `json:"version"`
	Provider string `json:"provider"`
	State    string `json:"state"`

	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Duration   string    `json:"duration"`
	Error      string    `json:"error,omitempty"`

	Progress Progress            `json:"progress"`
	Order    []string            `json:"order"`
	States   map[string]URLState `json:"states"`
	Failures []URLState          `json:"failures,omitempty"`
}

func (b *base) finishRun(started time.Time, ctx context.Context, runErr error) error {
	if b.provider == "" {
		return runErr
	}

	reportErr := b.writeRunReport(started, ctx, runErr)
	if reportErr != nil {
		return errors.Join(runErr, fmt.Errorf("write run report: %w", reportErr))
	}

	return runErr
}

func (b *base) writeRunReport(started time.Time, ctx context.Context, runErr error) error {
	b.mu.Lock()
	states := make(map[string]URLState, len(b.states))
	for url, state := range b.states {
		states[url] = *state
	}
	order := append([]string(nil), b.order...)
	provider := b.provider
	reportDir := b.reportDir
	current := b.current
	currentAttempt := 0
	if currentState := b.states[current]; currentState != nil {
		currentAttempt = currentState.Attempts
	}
	b.mu.Unlock()

	state := reportState(ctx, runErr)
	finished := time.Now().UTC()
	progress := progressFromStates(states)
	progress.Current = current
	progress.CurrentAttempt = currentAttempt

	report := RunReport{
		Version:    reportVersion,
		Provider:   provider,
		State:      state,
		StartedAt:  started.UTC(),
		FinishedAt: finished,
		Duration:   finished.Sub(started).Round(time.Millisecond).String(),
		Progress:   progress,
		Order:      order,
		States:     states,
		Failures:   reportFailures(states),
	}
	if runErr != nil {
		report.Error = runErr.Error()
	} else if ctx.Err() != nil {
		report.Error = ctx.Err().Error()
	}

	path, err := writeRunReport(reportDir, provider, state, report)
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.lastReport = path
	b.mu.Unlock()

	return nil
}

func reportState(ctx context.Context, runErr error) string {
	if errors.Is(runErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return ReportCancelled
	}
	if runErr != nil || ctx.Err() != nil {
		return ReportFailed
	}
	return ReportSuccess
}

func progressFromStates(states map[string]URLState) Progress {
	progress := Progress{Total: len(states)}

	for _, state := range states {
		switch {
		case state.Completed:
			progress.Completed++
			progress.Done++
		case state.NotFound:
			progress.NotFound++
			progress.Done++
		case state.Invalid:
			progress.Invalid++
			progress.Done++
		case state.Failed:
			progress.Failed++
			progress.Done++
		}
	}

	progress.Pending = progress.Total - progress.Done
	return progress
}

func reportFailures(states map[string]URLState) []URLState {
	failures := make([]URLState, 0)

	for _, state := range states {
		if state.Failed || state.NotFound || state.Invalid {
			failures = append(failures, state)
		}
	}

	sort.Slice(failures, func(i, j int) bool {
		return failures[i].URL < failures[j].URL
	})

	return failures
}

func writeRunReport(directory, provider, state string, report RunReport) (string, error) {
	if directory == "" {
		directory = defaultReportDir
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create report directory: %w", err)
	}

	provider = sanitizeReportName(provider)
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(directory, fmt.Sprintf("%s_%s_%s.json", provider, state, timestamp))
	for suffix := 2; ; suffix++ {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("check report path: %w", err)
		}
		path = filepath.Join(directory, fmt.Sprintf("%s_%s_%s_%d.json", provider, state, timestamp, suffix))
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode report: %w", err)
	}

	temp, err := os.CreateTemp(directory, ".retrier-report-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary report: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return "", fmt.Errorf("write report: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close report: %w", err)
	}

	if err := os.Rename(tempName, path); err != nil {
		// Windows does not replace an existing file with Rename.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return "", fmt.Errorf("replace report: %w", err)
		}
		if renameErr := os.Rename(tempName, path); renameErr != nil {
			return "", fmt.Errorf("replace report: %w", renameErr)
		}
	}

	return path, nil
}

func sanitizeReportName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '_':
			builder.WriteRune(char)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "retrier"
	}
	return result
}

func loadRunReport(path string) (RunReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunReport{}, fmt.Errorf("read report: %w", err)
	}

	var report RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return RunReport{}, fmt.Errorf("decode report: %w", err)
	}
	if report.Version != 0 && report.Version != reportVersion {
		return RunReport{}, fmt.Errorf("unsupported report version %d", report.Version)
	}

	return report, nil
}

func (b *base) continueFromReport(path string, resolve FilterResolver) error {
	report, err := loadRunReport(path)
	if err != nil {
		return err
	}

	b.mu.Lock()
	if len(b.states) > 0 {
		b.mu.Unlock()
		return ErrQueueNotEmpty
	}
	if b.provider == "" {
		b.provider = report.Provider
	}
	b.mu.Unlock()

	added := make(map[string]bool, len(report.States))
	prepared := make([]URLState, 0, len(report.States))

	addState := func(state URLState) {
		if state.URL == "" || added[state.URL] {
			return
		}
		copyState := state
		copyState.Filter = nil
		if !copyState.Terminal() && resolve != nil {
			copyState.Filter = resolve(state)
		}
		prepared = append(prepared, copyState)
		added[state.URL] = true
	}

	for _, url := range report.Order {
		if state, ok := report.States[url]; ok {
			addState(state)
		}
	}
	for url, state := range report.States {
		if state.URL == "" {
			state.URL = url
		}
		addState(state)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.states) > 0 {
		return ErrQueueNotEmpty
	}
	for index := range prepared {
		state := prepared[index]
		b.states[state.URL] = &state
		b.order = append(b.order, state.URL)
	}

	return nil
}
