package retrier_test

import (
	"context"
	"testing"

	"github.com/binderex/catsync/internal/retrier"
)

func TestHTTPProgress(t *testing.T) {
	server, _ := newTestServer(t)
	client := retrier.NewHTTP(server.Client(), retrier.WithPolicy(fastPolicy()))

	client.Add(server.URL+"/ok", nil)
	client.Add(server.URL+"/missing", nil)
	client.Add(server.URL+"/invalid", retrier.RejectSelector(".invalid"))
	client.Add(server.URL+"/fail", nil)

	initial := client.Progress()
	if initial.Total != 4 || initial.Done != 0 || initial.Pending != 4 {
		t.Fatalf("initial progress = %+v", initial)
	}
	if initial.Current != "" {
		t.Fatalf("initial Current = %q, want empty", initial.Current)
	}
	if initial.CurrentAttempt != 0 {
		t.Fatalf("initial CurrentAttempt = %d, want 0", initial.CurrentAttempt)
	}

	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	final := client.Progress()
	if final.Total != 4 || final.Done != 4 || final.Pending != 0 {
		t.Fatalf("final progress = %+v", final)
	}
	if final.Completed != 1 || final.NotFound != 1 || final.Invalid != 1 || final.Failed != 1 {
		t.Fatalf("final counters = %+v", final)
	}
	if final.Current != server.URL+"/fail" {
		t.Errorf("final Current = %q, want %q", final.Current, server.URL+"/fail")
	}
	if final.CurrentAttempt != 3 {
		t.Errorf("final CurrentAttempt = %d, want 3", final.CurrentAttempt)
	}
}
