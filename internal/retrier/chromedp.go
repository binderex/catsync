package retrier

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// Chromedp retries URLs in a real browser through the Chrome DevTools
// Protocol. It is intended for sites protected by JavaScript challenges that
// plain HTTP or Colly cannot pass.
type Chromedp struct {
	base

	execPath         string
	headless         bool
	timeout          time.Duration
	challengeTimeout time.Duration

	browserCtx    context.Context
	cancelBrowser context.CancelFunc
	cancelAlloc   context.CancelFunc
	profileDir    string
}

// ChromedpOption customizes NewChromedp.
type ChromedpOption func(*chromedpConfig)

type chromedpConfig struct {
	execPath         string
	headless         bool
	timeout          time.Duration
	challengeTimeout time.Duration
	provider         string
	reportDir        string
}

// NewChromedp returns a browser-backed retrier. The browser is started on the
// first Run and closed when Run ends.
func NewChromedp(opts ...ChromedpOption) *Chromedp {
	cfg := chromedpConfig{
		execPath:         defaultBrowserPath(),
		headless:         false,
		timeout:          3 * time.Minute,
		challengeTimeout: 2 * time.Minute,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &Chromedp{
		base: newBase(
			WithProvider(cfg.provider),
			WithReportDir(cfg.reportDir),
		),
		execPath:         cfg.execPath,
		headless:         cfg.headless,
		timeout:          cfg.timeout,
		challengeTimeout: cfg.challengeTimeout,
	}
}

// WithChromedpExecPath sets the browser executable. Empty uses the detected
// Brave, Chrome or Edge installation.
func WithChromedpExecPath(path string) ChromedpOption {
	return func(cfg *chromedpConfig) {
		cfg.execPath = path
	}
}

// WithChromedpHeadless controls whether the browser runs without a visible
// window. Visible mode is the default because Cloudflare often challenges
// headless browsers.
func WithChromedpHeadless(headless bool) ChromedpOption {
	return func(cfg *chromedpConfig) {
		cfg.headless = headless
	}
}

// WithChromedpTimeout sets the maximum time for one page load, including the
// challenge.
func WithChromedpTimeout(timeout time.Duration) ChromedpOption {
	return func(cfg *chromedpConfig) {
		if timeout > 0 {
			cfg.timeout = timeout
		}
	}
}

// WithChromedpChallengeTimeout sets how long the retrier waits for a
// Cloudflare challenge to clear before giving up.
func WithChromedpChallengeTimeout(timeout time.Duration) ChromedpOption {
	return func(cfg *chromedpConfig) {
		if timeout > 0 {
			cfg.challengeTimeout = timeout
		}
	}
}

// WithChromedpProvider enables run reports for this retrier.
func WithChromedpProvider(provider string) ChromedpOption {
	return func(cfg *chromedpConfig) {
		cfg.provider = provider
	}
}

// WithChromedpReportDir changes the run report directory.
func WithChromedpReportDir(directory string) ChromedpOption {
	return func(cfg *chromedpConfig) {
		cfg.reportDir = directory
	}
}

// Add queues a URL with its optional filter.
func (c *Chromedp) Add(url string, filter Filter) {
	c.add(url, filter)
}

// Run starts the browser, visits every pending URL and closes the browser.
func (c *Chromedp) Run(ctx context.Context) (err error) {
	started := time.Now().UTC()
	defer func() {
		err = c.finishRun(started, ctx, err)
	}()

	if err := c.start(ctx); err != nil {
		return err
	}
	defer c.stop()

	return c.run(ctx, c.fetch)
}

// Continue loads a report and resumes its pending URLs.
func (c *Chromedp) Continue(ctx context.Context, reportPath string, resolve FilterResolver) error {
	if err := c.continueFromReport(reportPath, resolve); err != nil {
		return err
	}
	return c.Run(ctx)
}

// States returns a snapshot of every tracked URL.
func (c *Chromedp) States() map[string]URLState {
	return c.statesSnapshot()
}

// State returns the state of a single URL.
func (c *Chromedp) State(url string) (URLState, bool) {
	return c.state(url)
}

// Progress returns a snapshot of the queue counters.
func (c *Chromedp) Progress() Progress {
	return c.progress()
}

func (c *Chromedp) start(ctx context.Context) error {
	if c.execPath == "" {
		return fmt.Errorf("no browser found; set DRAGONSHIELD_BROWSER or install Brave, Chrome or Edge")
	}

	profileDir, err := os.MkdirTemp("", "catsync-chromedp-")
	if err != nil {
		return fmt.Errorf("create browser profile: %w", err)
	}
	c.profileDir = profileDir

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if c.headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.VisibleWindow)
	}

	opts = append(
		opts,
		chromedp.ExecPath(c.execPath),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)

	if err := chromedp.Do(browserCtx); err != nil {
		cancelBrowser()
		cancelAlloc()
		_ = os.RemoveAll(profileDir)
		return fmt.Errorf("start browser: %w", err)
	}

	c.browserCtx = browserCtx
	c.cancelBrowser = cancelBrowser
	c.cancelAlloc = cancelAlloc

	return nil
}

func (c *Chromedp) stop() {
	if c.browserCtx != nil {
		_ = chromedp.Cancel(c.browserCtx)
	}
	if c.cancelBrowser != nil {
		c.cancelBrowser()
	}
	if c.cancelAlloc != nil {
		c.cancelAlloc()
	}
	if c.profileDir != "" {
		_ = os.RemoveAll(c.profileDir)
	}

	c.browserCtx = nil
	c.cancelBrowser = nil
	c.cancelAlloc = nil
	c.profileDir = ""
}

func (c *Chromedp) fetch(ctx context.Context, url string) fetchResult {
	if err := ctx.Err(); err != nil {
		return fetchResult{Err: err}
	}
	if c.browserCtx == nil {
		return fetchResult{Err: fmt.Errorf("browser is not running")}
	}

	fetchCtx, cancel := context.WithTimeout(c.browserCtx, c.timeout)
	defer cancel()

	response, err := chromedp.Run(fetchCtx, chromedp.NavigateResponse(url))
	if err != nil {
		return fetchResult{Err: err}
	}

	status := http.StatusOK
	header := make(http.Header)
	if response != nil {
		status = int(response.Status)
		for key, value := range response.Headers {
			header.Set(key, fmt.Sprint(value))
		}
	}

	deadline := time.Now().Add(c.challengeTimeout)
	var html string

	for {
		html, err = chromedp.Run(fetchCtx, chromedp.OuterHTML(chromedp.CSS("html")))
		if err == nil && challengeCleared(html) {
			break
		}
		if time.Now().After(deadline) {
			return fetchResult{
				StatusCode: http.StatusForbidden,
				Header:     header,
				Err:        fmt.Errorf("cloudflare challenge did not finish"),
			}
		}

		select {
		case <-time.After(500 * time.Millisecond):
		case <-fetchCtx.Done():
			return fetchResult{
				StatusCode: http.StatusForbidden,
				Header:     header,
				Err:        fetchCtx.Err(),
			}
		}
	}

	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		status = http.StatusOK
	}

	return fetchResult{
		StatusCode: status,
		Header:     header,
		Body:       []byte(html),
	}
}

func challengeCleared(html string) bool {
	for _, marker := range []string{
		"Just a moment",
		"cf-chl",
		"Checking your browser",
		"Enable JavaScript and cookies",
		"challenges.cloudflare.com",
	} {
		if strings.Contains(html, marker) {
			return false
		}
	}
	return true
}

// DefaultBrowserPath returns the detected browser executable, or an empty
// string when no supported browser is installed.
func DefaultBrowserPath() string {
	return defaultBrowserPath()
}

func defaultBrowserPath() string {
	if path := strings.TrimSpace(os.Getenv("DRAGONSHIELD_BROWSER")); path != "" {
		return path
	}

	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}

	for _, name := range []string{"brave", "brave-browser", "google-chrome", "chromium", "chromium-browser", "microsoft-edge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	return ""
}
