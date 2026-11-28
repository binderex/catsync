package domain

import "errors"

// ErrCrawlIncomplete is returned when a provider crawl finishes without every
// set and card print parsed. Incomplete results stay in retrier persistence so
// the next execution can resume them; they are never upserted into the catalog.
var ErrCrawlIncomplete = errors.New("catalog crawl incomplete")

// ProviderOptions configures one catalog provider run.
type ProviderOptions struct {
	// Mediums filters the set index by medium (TCG or OCG). An empty slice
	// keeps every medium.
	Mediums []string
	// Resume continues from the provider checkpoint when true.
	Resume bool
	// Mode tags the retrier run in persistence ("new" or "all").
	Mode string
	// ResumeState is the previous unfinished retrier run loaded by the use
	// case. When set, the provider restores it and only fetches pending URLs.
	ResumeState *RetrierRun
}
