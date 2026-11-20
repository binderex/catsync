package retrier_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/binderex/catsync/internal/retrier"
)

func TestChromedpNavigatesDragonShield(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live browser test in short mode")
	}
	if os.Getenv("DRAGONSHIELD_LIVE") == "" {
		t.Skip("set DRAGONSHIELD_LIVE=1 to run the live browser test")
	}
	if retrier.DefaultBrowserPath() == "" {
		t.Skip("no Brave, Chrome or Edge installation found")
	}

	const collectionURL = "https://www.dragonshield.com/collections/card-sleeves"

	browser := retrier.NewChromedp()

	var body []byte
	browser.Add(collectionURL, func(response retrier.Response) bool {
		body = append([]byte(nil), response.Body...)
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	start := time.Now()
	if err := browser.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Logf("navigation took %s", time.Since(start).Round(time.Second))

	state, ok := browser.State(collectionURL)
	if !ok {
		t.Fatalf("missing state for %s", collectionURL)
	}
	if !state.Completed {
		t.Fatalf("URL not completed: %+v", state)
	}
	if !strings.Contains(string(body), "js-collection-grid") {
		t.Fatalf("collection page does not contain js-collection-grid (body length %d)", len(body))
	}
}
