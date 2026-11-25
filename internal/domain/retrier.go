package domain

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Response is the result of a transport fetch.
type Response struct {
	URL        string
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Filter reports whether a successful response is invalid. Returning true
// marks the URL as invalid and stops retrying it.
type Filter func(Response) bool

// FilterResolver returns the filter for a pending URL loaded from persistence.
type FilterResolver func(URLState) Filter

// RejectWhen adapts a predicate to a Filter.
func RejectWhen(predicate func(Response) bool) Filter {
	return Filter(predicate)
}

// URLState is the retry state of a single URL.
type URLState struct {
	URL       string `json:"url"`
	Visited   bool   `json:"visited"`
	Scraped   bool   `json:"scraped"`
	Completed bool   `json:"completed"`
	NotFound  bool   `json:"not_found"`
	Invalid   bool   `json:"invalid"`
	Failed    bool   `json:"failed"`
	Attempts  int    `json:"attempts"`
	Error     string `json:"error,omitempty"`

	// Filter decides whether a successful response is invalid. It is not
	// serialized and is kept in memory only.
	Filter Filter `json:"-"`
}

// Terminal reports whether the URL no longer needs more attempts.
func (s URLState) Terminal() bool {
	return s.Completed || s.NotFound || s.Invalid || s.Failed
}

// Progress is a snapshot of the retry queue counters.
type Progress struct {
	Total          int    `json:"total"`
	Done           int    `json:"done"`
	Pending        int    `json:"pending"`
	Completed      int    `json:"completed"`
	NotFound       int    `json:"not_found"`
	Invalid        int    `json:"invalid"`
	Failed         int    `json:"failed"`
	Current        string `json:"current,omitempty"`
	CurrentAttempt int    `json:"current_attempt,omitempty"`
}

// ProgressFromStates builds the global counters from states.
func ProgressFromStates(states map[string]URLState) Progress {
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

// FailuresFromStates returns the failed, not-found and invalid states sorted by
// URL.
func FailuresFromStates(states map[string]URLState) []URLState {
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

// Policy controls how failed requests are retried. Zero values fall back to the
// defaults in DefaultPolicy.
type Policy struct {
	// MaxAttempts is the number of attempts for ordinary retryable errors.
	MaxAttempts int
	// TooManyAttempts is the number of attempts for HTTP 429 responses.
	TooManyAttempts int
	// BlockedAttempts is the number of attempts for HTTP 401 and 403 responses.
	BlockedAttempts int
	// RetryDelay is the base delay for ordinary retryable errors.
	RetryDelay time.Duration
	// TooManyBaseDelay is the base delay for HTTP 429 responses.
	TooManyBaseDelay time.Duration
	// BlockedBaseDelay is the base delay for HTTP 401 and 403 responses.
	BlockedBaseDelay time.Duration
	// MaxDelay caps any retry delay, including Retry-After values.
	MaxDelay time.Duration
}

// DefaultPolicy keeps the historical retry behaviour.
var DefaultPolicy = Policy{
	MaxAttempts:      3,
	TooManyAttempts:  3,
	BlockedAttempts:  3,
	RetryDelay:       2 * time.Second,
	TooManyBaseDelay: 5 * time.Second,
	BlockedBaseDelay: 30 * time.Second,
	MaxDelay:         2 * time.Minute,
}

// Attempts returns the maximum number of attempts allowed for a status.
func (p Policy) Attempts(status int) int {
	switch status {
	case http.StatusTooManyRequests:
		return positiveOr(p.TooManyAttempts, DefaultPolicy.TooManyAttempts)
	case http.StatusUnauthorized, http.StatusForbidden:
		return positiveOr(p.BlockedAttempts, DefaultPolicy.BlockedAttempts)
	default:
		return positiveOr(p.MaxAttempts, DefaultPolicy.MaxAttempts)
	}
}

// Wait returns how long to wait before retrying a failed request.
func (p Policy) Wait(status, attempt int, retryAfter time.Duration) time.Duration {
	maxDelay := positiveDuration(p.MaxDelay, DefaultPolicy.MaxDelay)
	if retryAfter > 0 {
		return min(retryAfter, maxDelay)
	}

	var base time.Duration
	switch status {
	case http.StatusTooManyRequests:
		base = positiveDuration(p.TooManyBaseDelay, DefaultPolicy.TooManyBaseDelay)
	case http.StatusUnauthorized, http.StatusForbidden:
		base = positiveDuration(p.BlockedBaseDelay, DefaultPolicy.BlockedBaseDelay)
	default:
		base = positiveDuration(p.RetryDelay, DefaultPolicy.RetryDelay)
	}

	if attempt < 1 {
		attempt = 1
	}

	delay := base
	for i := 1; i < attempt && delay < maxDelay; i++ {
		delay *= 2
	}

	return min(delay, maxDelay)
}

// IsRetryableStatus reports whether a failed request may succeed if retried.
func IsRetryableStatus(status int) bool {
	switch status {
	case 0, // network/timeout error
		http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		// Cloudflare-specific gateway errors.
		520, 521, 522, 523, 524, 525, 526, 527, 530:
		return true
	default:
		return false
	}
}

// ParseRetryAfter reads a Retry-After header expressed either in seconds or as
// an HTTP date. It returns zero when the header is absent or invalid.
func ParseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if t, err := http.ParseTime(value); err == nil {
		if wait := time.Until(t); wait > 0 {
			return wait
		}
	}

	return 0
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// Retrier run states. A run in "running" or "interrupted" state can be resumed.
type RetrierRunID string

// RetrierRunState is the lifecycle state of a retrier run.
type RetrierRunState string

const (
	RetrierRunRunning     RetrierRunState = "running"
	RetrierRunSuccess     RetrierRunState = "success"
	RetrierRunCancelled   RetrierRunState = "cancelled"
	RetrierRunFailed      RetrierRunState = "failed"
	RetrierRunInterrupted RetrierRunState = "interrupted"
)

// Report state values kept for compatibility with the existing report files.
const (
	ReportRunning   = "running"
	ReportSuccess   = "success"
	ReportCancelled = "cancelled"
	ReportFailed    = "failed"
)

// RetrierRun is the full snapshot of a retrier execution. It is the unit that
// the persistence adapters save and restore.
type RetrierRun struct {
	Version    int             `json:"version"`
	ID         RetrierRunID    `json:"run_id"`
	Provider   string          `json:"provider"`
	Mode       string          `json:"mode,omitempty"`
	State      RetrierRunState `json:"state"`
	StartedAt  time.Time       `json:"started_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Duration   string          `json:"duration,omitempty"`
	Error      string          `json:"error,omitempty"`

	Progress Progress            `json:"progress"`
	Order    []string            `json:"order"`
	States   map[string]URLState `json:"states"`
	Failures []URLState          `json:"failures,omitempty"`

	// ProviderData is the provider-specific crawl snapshot (for example the
	// parsed YGO sets and prints). It travels with the run so the retrier JSON
	// document is the only state file.
	ProviderData json.RawMessage `json:"provider_data,omitempty"`
}

// RunReport is the report representation of a run.
type RunReport = RetrierRun

// RetrierRunSummary is the lightweight representation used by List.
type RetrierRunSummary struct {
	Version    int             `json:"version"`
	ID         RetrierRunID    `json:"run_id"`
	Provider   string          `json:"provider"`
	Mode       string          `json:"mode,omitempty"`
	State      RetrierRunState `json:"state"`
	StartedAt  time.Time       `json:"started_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Duration   string          `json:"duration,omitempty"`
	Error      string          `json:"error,omitempty"`
	Progress   Progress        `json:"progress"`
}

// RetrierRunFilter filters the runs returned by List.
type RetrierRunFilter struct {
	Provider string
	Mode     string
	State    RetrierRunState
	Limit    int
	Offset   int
}

// Errors returned by the retrier and persistence layers.
var (
	// ErrQueueNotEmpty is returned by Continue when the retrier already has URLs.
	ErrQueueNotEmpty = errors.New("retrier queue is not empty")
	// ErrRunNotFound is returned when a persisted run does not exist.
	ErrRunNotFound = errors.New("retrier run not found")
	// ErrRunNotResumable is returned when a completed run is resumed.
	ErrRunNotResumable = errors.New("retrier run is not resumable")
	// ErrPersistenceDisabled is returned when persistence is required but not
	// configured.
	ErrPersistenceDisabled = errors.New("retrier persistence is disabled")
	// ErrUnexpectedStatus is returned by Fetch for a terminal non-2xx response.
	ErrUnexpectedStatus = errors.New("unexpected HTTP status")
	// ErrNotFound is returned by Fetch for a 404 response.
	ErrNotFound = errors.New("resource not found")
)
