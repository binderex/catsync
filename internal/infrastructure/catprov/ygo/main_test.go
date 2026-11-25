package ygo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain sends generated crawl reports to a temporary directory so tests
// never leave files inside the package.
func TestMain(m *testing.M) {
	previous := crawlReportPath

	dir, err := os.MkdirTemp("", "catsync-ygo-tests-")
	if err != nil {
		panic(err)
	}
	crawlReportPath = filepath.Join(dir, "ygo-crawl-report.json")

	code := m.Run()

	_ = os.RemoveAll(dir)
	crawlReportPath = previous
	os.Exit(code)
}
