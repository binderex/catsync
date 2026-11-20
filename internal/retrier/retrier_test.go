package retrier_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/retrier"
)

func fastPolicy() retrier.Policy {
	return retrier.Policy{
		MaxAttempts:      3,
		TooManyAttempts:  3,
		BlockedAttempts:  3,
		RetryDelay:       time.Millisecond,
		TooManyBaseDelay: time.Millisecond,
		BlockedBaseDelay: time.Millisecond,
		MaxDelay:         5 * time.Millisecond,
	}
}

func newTestServer(t *testing.T) (*httptest.Server, map[string]*atomic.Int32) {
	t.Helper()

	var failures atomic.Int32
	counters := map[string]*atomic.Int32{
		"/retry": &failures,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/invalid", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div class="invalid"></div></body></html>`))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/retry", func(w http.ResponseWriter, r *http.Request) {
		if counters["/retry"].Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server, counters
}

func TestHTTPStates(t *testing.T) {
	server, counters := newTestServer(t)
	invalidFilter := retrier.RejectSelector(".invalid")
	client := retrier.NewHTTP(server.Client(), retrier.WithPolicy(fastPolicy()))

	client.Add(server.URL+"/ok", nil)
	client.Add(server.URL+"/missing", nil)
	client.Add(server.URL+"/invalid", invalidFilter)
	client.Add(server.URL+"/fail", nil)
	client.Add(server.URL+"/retry", nil)

	started := time.Now()
	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error for failed URLs: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run took too long: %v", elapsed)
	}

	states := client.States()
	if len(states) != 5 {
		t.Fatalf("expected 5 states, got %d", len(states))
	}

	assertState(t, client, server.URL+"/ok", stateExpectation{
		visited: true, scraped: true, completed: true, attempts: 1,
	})
	assertState(t, client, server.URL+"/missing", stateExpectation{
		visited: true, notFound: true, attempts: 1,
	})
	assertState(t, client, server.URL+"/invalid", stateExpectation{
		visited: true, scraped: true, invalid: true, attempts: 1,
	})
	assertState(t, client, server.URL+"/fail", stateExpectation{
		visited: true, failed: true, attempts: 3,
	})
	assertState(t, client, server.URL+"/retry", stateExpectation{
		visited: true, scraped: true, completed: true, attempts: 3,
	})

	if got := counters["/retry"].Load(); got != 3 {
		t.Fatalf("retry endpoint received %d requests, want 3", got)
	}
}

func TestHTTPAddWhileRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	slowMux := http.NewServeMux()
	slowMux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("slow"))
	})
	slowMux.HandleFunc("/added", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("added"))
	})
	slowServer := httptest.NewServer(slowMux)
	t.Cleanup(slowServer.Close)

	client := retrier.NewHTTP(slowServer.Client(), retrier.WithPolicy(fastPolicy()))
	client.Add(slowServer.URL+"/slow", nil)

	done := make(chan error, 1)
	go func() {
		done <- client.Run(context.Background())
	}()

	<-started
	client.Add(slowServer.URL+"/added", nil)
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, path := range []string{"/slow", "/added"} {
		state, ok := client.State(slowServer.URL + path)
		if !ok {
			t.Fatalf("missing state for %s", path)
		}
		if !state.Completed {
			t.Fatalf("%s not completed: %+v", path, state)
		}
	}
}

func TestHTTPRunContinuesAfterCompletion(t *testing.T) {
	server, _ := newTestServer(t)
	client := retrier.NewHTTP(server.Client(), retrier.WithPolicy(fastPolicy()))

	client.Add(server.URL+"/ok", nil)
	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	client.Add(server.URL+"/missing", nil)
	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	assertState(t, client, server.URL+"/missing", stateExpectation{
		visited: true, notFound: true, attempts: 1,
	})
}

func TestHTTPFilterSeesBody(t *testing.T) {
	server, _ := newTestServer(t)
	client := retrier.NewHTTP(server.Client(), retrier.WithPolicy(fastPolicy()))

	var sawBody string
	filter := retrier.RejectWhen(func(resp retrier.Response) bool {
		sawBody = string(resp.Body)
		return strings.Contains(sawBody, "do-not-reject")
	})

	client.Add(server.URL+"/ok", filter)
	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(sawBody, "ok") {
		t.Fatalf("filter did not receive the body: %q", sawBody)
	}
}

type stateExpectation struct {
	visited   bool
	scraped   bool
	completed bool
	notFound  bool
	invalid   bool
	failed    bool
	attempts  int
}

func assertState(t *testing.T, client retrier.Retrier, url string, want stateExpectation) {
	t.Helper()

	state, ok := client.State(url)
	if !ok {
		t.Fatalf("missing state for %s", url)
	}

	if state.Visited != want.visited {
		t.Errorf("%s Visited = %v, want %v", url, state.Visited, want.visited)
	}
	if state.Scraped != want.scraped {
		t.Errorf("%s Scraped = %v, want %v", url, state.Scraped, want.scraped)
	}
	if state.Completed != want.completed {
		t.Errorf("%s Completed = %v, want %v", url, state.Completed, want.completed)
	}
	if state.NotFound != want.notFound {
		t.Errorf("%s NotFound = %v, want %v", url, state.NotFound, want.notFound)
	}
	if state.Invalid != want.invalid {
		t.Errorf("%s Invalid = %v, want %v", url, state.Invalid, want.invalid)
	}
	if state.Failed != want.failed {
		t.Errorf("%s Failed = %v, want %v", url, state.Failed, want.failed)
	}
	if state.Attempts != want.attempts {
		t.Errorf("%s Attempts = %d, want %d", url, state.Attempts, want.attempts)
	}
	if want.failed && state.Error == "" {
		t.Errorf("%s Failed without an error message", url)
	}
	if !want.failed && state.Error != "" {
		t.Errorf("%s has unexpected error: %s", url, state.Error)
	}
}
