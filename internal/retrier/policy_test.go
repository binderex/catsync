package retrier_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/retrier"
)

func TestPolicyAttempts(t *testing.T) {
	policy := retrier.DefaultPolicy

	tests := []struct {
		status int
		want   int
	}{
		{0, 3},
		{http.StatusInternalServerError, 3},
		{http.StatusTooManyRequests, 3},
		{http.StatusForbidden, 3},
		{http.StatusUnauthorized, 3},
	}

	for _, test := range tests {
		if got := policy.Attempts(test.status); got != test.want {
			t.Errorf("Attempts(%d) = %d, want %d", test.status, got, test.want)
		}
	}

	custom := retrier.Policy{MaxAttempts: 7}
	if got := custom.Attempts(http.StatusInternalServerError); got != 7 {
		t.Errorf("custom Attempts = %d, want 7", got)
	}
}

func TestPolicyWait(t *testing.T) {
	policy := retrier.DefaultPolicy

	tests := []struct {
		name       string
		status     int
		attempt    int
		retryAfter time.Duration
		want       time.Duration
	}{
		{"default", http.StatusInternalServerError, 1, 0, 2 * time.Second},
		{"backoff", http.StatusInternalServerError, 3, 0, 8 * time.Second},
		{"too many", http.StatusTooManyRequests, 1, 0, 5 * time.Second},
		{"blocked", http.StatusForbidden, 1, 0, 30 * time.Second},
		{"retry after", http.StatusInternalServerError, 1, 3 * time.Second, 3 * time.Second},
		{"retry after cap", http.StatusInternalServerError, 1, time.Hour, 2 * time.Minute},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := policy.Wait(test.status, test.attempt, test.retryAfter); got != test.want {
				t.Errorf("Wait(%d, %d, %v) = %v, want %v",
					test.status, test.attempt, test.retryAfter, got, test.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := retrier.ParseRetryAfter("120"); got != 2*time.Minute {
		t.Errorf("seconds = %v, want 2m", got)
	}
	if got := retrier.ParseRetryAfter(""); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
	if got := retrier.ParseRetryAfter("not-a-date"); got != 0 {
		t.Errorf("invalid = %v, want 0", got)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{
		0,
		http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		520,
		521,
		522,
		523,
		524,
		525,
		526,
		527,
		530,
	}
	for _, status := range retryable {
		if !retrier.IsRetryableStatus(status) {
			t.Errorf("status %d should be retryable", status)
		}
	}

	notRetryable := []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusNotAcceptable,
	}
	for _, status := range notRetryable {
		if retrier.IsRetryableStatus(status) {
			t.Errorf("status %d should not be retryable", status)
		}
	}
}
