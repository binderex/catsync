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

	"github.com/binderex/catsync/internal/domain"
)

// WithExecPath sets the browser executable. Empty uses the detected browser.
func WithExecPath(path string) Option {
	return func(r *Retrier) {
		r.execPath = path
	}
}

// WithHeadless controls whether the browser runs without a visible window.
func WithHeadless(headless bool) Option {
	return func(r *Retrier) {
		r.headless = headless
	}
}

// WithTimeout sets the maximum time for one page load.
func WithTimeout(timeout time.Duration) Option {
	return func(r *Retrier) {
		if timeout > 0 {
			r.timeout = timeout
		}
	}
}

// WithChallengeTimeout sets how long to wait for a Cloudflare challenge.
func WithChallengeTimeout(timeout time.Duration) Option {
	return func(r *Retrier) {
		if timeout > 0 {
			r.challengeTimeout = timeout
		}
	}
}

type chromedpTransport struct {
	execPath         string
	headless         bool
	timeout          time.Duration
	challengeTimeout time.Duration

	browserCtx    context.Context
	cancelBrowser context.CancelFunc
	cancelAlloc   context.CancelFunc
	profileDir    string
}

// NewChromedp creates a browser-backed retrier. The browser starts lazily on
// the first Run and closes when the run ends.
func NewChromedp(opts ...Option) *Retrier {
	r := newRetrier(domain.DefaultPolicy, opts...)
	if r.challengeTimeout == 0 {
		r.challengeTimeout = 2 * time.Minute
	}
	if r.execPath == "" {
		r.execPath = DefaultBrowserPath()
	}

	transport := &chromedpTransport{
		execPath:         r.execPath,
		headless:         r.headless,
		timeout:          r.timeout,
		challengeTimeout: r.challengeTimeout,
	}

	r.fetch = transport.fetch
	r.open = transport.open
	r.close = transport.close

	return r
}

func (t *chromedpTransport) open(ctx context.Context) error {
	if t.browserCtx != nil {
		return nil
	}
	if t.execPath == "" {
		return fmt.Errorf("no browser found; set DRAGONSHIELD_BROWSER or install Brave, Chrome or Edge")
	}

	profileDir, err := os.MkdirTemp("", "catsync-chromedp-")
	if err != nil {
		return fmt.Errorf("create browser profile: %w", err)
	}
	t.profileDir = profileDir

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if t.headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.VisibleWindow)
	}
	opts = append(
		opts,
		chromedp.ExecPath(t.execPath),
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

	t.browserCtx = browserCtx
	t.cancelBrowser = cancelBrowser
	t.cancelAlloc = cancelAlloc

	return nil
}

func (t *chromedpTransport) close() error {
	if t.browserCtx != nil {
		_ = chromedp.Cancel(t.browserCtx)
	}
	if t.cancelBrowser != nil {
		t.cancelBrowser()
	}
	if t.cancelAlloc != nil {
		t.cancelAlloc()
	}
	if t.profileDir != "" {
		_ = os.RemoveAll(t.profileDir)
	}

	t.browserCtx = nil
	t.cancelBrowser = nil
	t.cancelAlloc = nil
	t.profileDir = ""

	return nil
}

func (t *chromedpTransport) fetch(ctx context.Context, url string) (domain.Response, error) {
	if err := ctx.Err(); err != nil {
		return domain.Response{URL: url}, err
	}
	if t.browserCtx == nil {
		return domain.Response{URL: url}, fmt.Errorf("browser is not running")
	}

	fetchCtx, cancel := context.WithTimeout(t.browserCtx, t.timeout)
	defer cancel()

	response, err := chromedp.Run(fetchCtx, chromedp.NavigateResponse(url))
	if err != nil {
		return domain.Response{URL: url}, err
	}

	status := http.StatusOK
	header := make(http.Header)
	if response != nil {
		status = int(response.Status)
		for key, value := range response.Headers {
			header.Set(key, fmt.Sprint(value))
		}
	}

	deadline := time.Now().Add(t.challengeTimeout)
	var html string

	for {
		html, err = chromedp.Run(fetchCtx, chromedp.OuterHTML(chromedp.CSS("html")))
		if err == nil && challengeCleared(html) {
			break
		}
		if time.Now().After(deadline) {
			return domain.Response{
				URL:        url,
				StatusCode: http.StatusForbidden,
				Header:     header,
			}, fmt.Errorf("cloudflare challenge did not finish")
		}

		select {
		case <-time.After(500 * time.Millisecond):
		case <-fetchCtx.Done():
			return domain.Response{
				URL:        url,
				StatusCode: http.StatusForbidden,
				Header:     header,
			}, fetchCtx.Err()
		}
	}

	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		status = http.StatusOK
	}

	return domain.Response{
		URL:        url,
		StatusCode: status,
		Header:     header,
		Body:       []byte(html),
	}, nil
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
