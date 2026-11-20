package retrier

// Option customizes a retrier created by NewHTTP or NewColly.
type Option func(*options)

type options struct {
	policy    Policy
	provider  string
	reportDir string
}

// WithPolicy replaces the retry policy used by the retrier.
func WithPolicy(policy Policy) Option {
	return func(opts *options) {
		opts.policy = policy
	}
}

// WithProvider enables final run reports and sets the provider name used in
// the report file name.
func WithProvider(provider string) Option {
	return func(opts *options) {
		opts.provider = provider
	}
}

// WithReportDir changes the directory where run reports are written. The
// default is "reports".
func WithReportDir(directory string) Option {
	return func(opts *options) {
		opts.reportDir = directory
	}
}

func newBase(opts ...Option) base {
	cfg := options{
		policy:    DefaultPolicy,
		reportDir: defaultReportDir,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.reportDir == "" {
		cfg.reportDir = defaultReportDir
	}
	return base{
		policy:    cfg.policy,
		provider:  cfg.provider,
		reportDir: cfg.reportDir,
		states:    make(map[string]*URLState),
	}
}
