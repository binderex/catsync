package retrier_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gocolly/colly/v2"

	"github.com/binderex/catsync/internal/retrier"
)

func newCollyClient() *retrier.Colly {
	collector := colly.NewCollector(colly.IgnoreRobotsTxt())
	return retrier.NewColly(collector, retrier.WithPolicy(fastPolicy()))
}

func TestCollyStates(t *testing.T) {
	server, counters := newTestServer(t)
	invalidFilter := retrier.RejectSelector(".invalid")
	client := newCollyClient()

	client.Add(server.URL+"/ok", nil)
	client.Add(server.URL+"/missing", nil)
	client.Add(server.URL+"/invalid", invalidFilter)
	client.Add(server.URL+"/fail", nil)
	client.Add(server.URL+"/retry", nil)

	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error for failed URLs: %v", err)
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

func TestCollyAddWhileRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("slow"))
	})
	mux.HandleFunc("/added", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("added"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := newCollyClient()
	client.Add(server.URL+"/slow", nil)

	done := make(chan error, 1)
	go func() {
		done <- client.Run(context.Background())
	}()

	<-started
	client.Add(server.URL+"/added", nil)
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, path := range []string{"/slow", "/added"} {
		state, ok := client.State(server.URL + path)
		if !ok {
			t.Fatalf("missing state for %s", path)
		}
		if !state.Completed {
			t.Fatalf("%s not completed: %+v", path, state)
		}
	}
}
