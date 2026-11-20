package retrier_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/retrier"
)

func TestHTTPRunReport(t *testing.T) {
	server, _ := newTestServer(t)
	reportDir := t.TempDir()

	client := retrier.NewHTTP(
		server.Client(),
		retrier.WithPolicy(fastPolicy()),
		retrier.WithProvider("Test Provider"),
		retrier.WithReportDir(reportDir),
	)
	client.Add(server.URL+"/ok", nil)
	client.Add(server.URL+"/fail", nil)

	if err := client.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reportPath := client.LastReport()
	if reportPath == "" {
		t.Fatal("LastReport is empty")
	}
	if !strings.Contains(filepath.Base(reportPath), "test-provider_success_") {
		t.Errorf("report file = %q", reportPath)
	}

	report := readRunReport(t, reportPath)
	if report.Provider != "Test Provider" || report.State != retrier.ReportSuccess {
		t.Errorf("report header = %q / %q", report.Provider, report.State)
	}
	if report.Progress.Total != 2 || report.Progress.Completed != 1 || report.Progress.Failed != 1 {
		t.Errorf("progress = %+v", report.Progress)
	}
	if len(report.Failures) != 1 || report.Failures[0].URL != server.URL+"/fail" {
		t.Errorf("failures = %+v", report.Failures)
	}
	if len(report.Order) != 2 {
		t.Errorf("order = %+v", report.Order)
	}
}

func TestHTTPRunReportCancelledAndContinue(t *testing.T) {
	reportDir := t.TempDir()

	var (
		once    sync.Once
		started = make(chan struct{})
		release = make(chan struct{})
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = w.Write([]byte("ok"))
	})
	slowServer := httptest.NewServer(mux)
	t.Cleanup(slowServer.Close)

	client := retrier.NewHTTP(
		slowServer.Client(),
		retrier.WithPolicy(fastPolicy()),
		retrier.WithProvider("test"),
		retrier.WithReportDir(reportDir),
	)
	client.Add(slowServer.URL+"/slow", nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx)
	}()

	<-started
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}

	firstReport := readRunReport(t, client.LastReport())
	if firstReport.State != retrier.ReportCancelled {
		t.Fatalf("state = %q", firstReport.State)
	}
	slowState := firstReport.States[slowServer.URL+"/slow"]
	if slowState.Terminal() || !slowState.Visited {
		t.Fatalf("pending state = %+v", slowState)
	}

	continued := retrier.NewHTTP(
		slowServer.Client(),
		retrier.WithPolicy(fastPolicy()),
		retrier.WithReportDir(reportDir),
	)
	continueCtx, cancelContinue := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelContinue()

	resolverCalled := false
	if err := continued.Continue(continueCtx, client.LastReport(), func(state retrier.URLState) retrier.Filter {
		resolverCalled = true
		return nil
	}); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if !resolverCalled {
		t.Error("filter resolver was not called")
	}

	if state, _ := continued.State(slowServer.URL + "/slow"); !state.Completed {
		t.Errorf("continued state = %+v", state)
	}

	secondReport := readRunReport(t, continued.LastReport())
	if secondReport.State != retrier.ReportSuccess {
		t.Errorf("continued report state = %q", secondReport.State)
	}
}

func TestHTTPRunReportFailed(t *testing.T) {
	server, _ := newTestServer(t)
	reportDir := t.TempDir()

	client := retrier.NewHTTP(
		server.Client(),
		retrier.WithProvider("failed-provider"),
		retrier.WithReportDir(reportDir),
	)
	client.Add(server.URL+"/ok", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	err := client.Run(ctx)
	if err == nil {
		t.Fatal("Run should fail with an expired context")
	}

	report := readRunReport(t, client.LastReport())
	if report.State != retrier.ReportFailed {
		t.Errorf("state = %q", report.State)
	}
	if report.Error == "" {
		t.Error("report error is empty")
	}
}

func TestContinueRejectsNonEmptyQueue(t *testing.T) {
	server, _ := newTestServer(t)
	reportDir := t.TempDir()

	first := retrier.NewHTTP(
		server.Client(),
		retrier.WithProvider("test"),
		retrier.WithReportDir(reportDir),
	)
	first.Add(server.URL+"/ok", nil)
	if err := first.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	second := retrier.NewHTTP(server.Client(), retrier.WithReportDir(reportDir))
	second.Add(server.URL+"/missing", nil)
	if err := second.Continue(context.Background(), first.LastReport(), nil); !errors.Is(err, retrier.ErrQueueNotEmpty) {
		t.Fatalf("Continue error = %v, want ErrQueueNotEmpty", err)
	}
}

func readRunReport(t *testing.T, path string) retrier.RunReport {
	t.Helper()

	if path == "" {
		t.Fatal("empty report path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}

	var report retrier.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return report
}

func TestReportFileNameIsUnique(t *testing.T) {
	dir := t.TempDir()
	server, _ := newTestServer(t)

	client := retrier.NewHTTP(
		server.Client(),
		retrier.WithProvider("same"),
		retrier.WithReportDir(dir),
	)
	client.Add(server.URL+"/ok", nil)

	paths := make(map[string]bool)
	for i := 0; i < 2; i++ {
		if err := client.Run(context.Background()); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		path := client.LastReport()
		if paths[path] {
			t.Fatalf("duplicated report path %q", path)
		}
		paths[path] = true
		if matches, _ := filepath.Glob(filepath.Join(dir, "same_success_*.json")); len(matches) < i+1 {
			t.Fatalf("expected report file, got %v", matches)
		}
	}
}
