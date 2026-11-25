package retrier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/binderex/catsync/internal/domain"
)

type recordingObserver struct {
	mu      sync.Mutex
	started []domain.RetrierRun
	changes []domain.URLState
	finish  []domain.RetrierRun
	runID   domain.RetrierRunID
}

func (o *recordingObserver) RunStarted(_ context.Context, run domain.RetrierRun) (domain.RetrierRunID, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.started = append(o.started, run)
	if o.runID == "" {
		o.runID = run.ID
	}
	return o.runID, nil
}

func (o *recordingObserver) RunStateChanged(_ context.Context, _ domain.RetrierRunID, state domain.URLState) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.changes = append(o.changes, state)
	return nil
}

func (o *recordingObserver) RunFinished(_ context.Context, run domain.RetrierRun) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.finish = append(o.finish, run)
	return nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/invalid":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<div class=\"noarticletext\"></div>"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>ok</html>"))
		}
	}))
}

func TestRunCompletesQueueAndNotifiesObserver(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	observer := &recordingObserver{}
	r := NewHTTP(server.Client(), WithProvider("test"), WithMode("all"), WithObserver(observer))
	r.Add(server.URL+"/a", nil)
	r.Add(server.URL+"/b", nil)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	states := r.States()
	if len(states) != 2 {
		t.Fatalf("states = %d, want 2", len(states))
	}
	for url, state := range states {
		if !state.Completed {
			t.Errorf("%s not completed: %+v", url, state)
		}
	}

	if len(observer.started) != 1 || observer.started[0].Provider != "test" {
		t.Fatalf("started = %+v", observer.started)
	}
	if len(observer.finish) != 1 || observer.finish[0].State != domain.RetrierRunSuccess {
		t.Fatalf("finish = %+v", observer.finish)
	}
	if len(observer.changes) < 2 {
		t.Errorf("changes = %d, want at least 2", len(observer.changes))
	}
}

func TestRestoreFetchesOnlyPendingURLs(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	done := server.URL + "/done"
	pending := server.URL + "/pending"

	r := NewHTTP(server.Client())
	r.Restore(
		map[string]domain.URLState{
			done:    {URL: done, Visited: true, Completed: true},
			pending: {URL: pending},
		},
		[]string{done, pending},
		nil,
	)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	states := r.States()
	if !states[done].Completed {
		t.Errorf("restored completed state was lost: %+v", states[done])
	}
	if !states[pending].Completed {
		t.Errorf("pending state was not fetched: %+v", states[pending])
	}
}

func TestRunMarksInvalidAndNotFound(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	invalid := server.URL + "/invalid"
	missing := server.URL + "/missing"

	r := NewHTTP(server.Client())
	r.Add(invalid, RejectSelector(".noarticletext"))
	r.Add(missing, nil)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	states := r.States()
	if !states[invalid].Invalid {
		t.Errorf("invalid state = %+v", states[invalid])
	}
	if !states[missing].NotFound {
		t.Errorf("missing state = %+v", states[missing])
	}
}

func TestWithRunIDReusesPersistedRun(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	observer := &recordingObserver{runID: "run-42"}
	r := NewHTTP(server.Client(), WithObserver(observer), WithRunID("run-42"))
	r.Add(server.URL+"/a", nil)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(observer.started) != 1 {
		t.Fatalf("started = %d, want 1", len(observer.started))
	}
	if observer.started[0].ID != "run-42" {
		t.Errorf("run id = %q, want run-42", observer.started[0].ID)
	}
	if len(observer.finish) != 1 || observer.finish[0].ID != "run-42" {
		t.Errorf("finish = %+v", observer.finish)
	}
}
