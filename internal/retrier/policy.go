package retrier

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

// DefaultPolicy keeps the historical retry behaviour: fast retries with
// exponential backoff and special handling for 429 and 401/403 responses.
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
		// Cloudflare-specific gateway errors. They are transient origin
		// failures, not missing pages, and should get the same retry treatment
		// as the standard 5xx statuses.
		520, // unknown error from the origin
		521, // origin web server is down
		522, // connection to the origin timed out
		523, // origin is unreachable
		524, // origin timed out
		525, // SSL handshake failed
		526, // invalid SSL certificate
		527, // Railgun error
		530: // origin DNS error
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
