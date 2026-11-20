package retrier

// Option customizes a retrier created by NewHTTP or NewColly.
type Option func(*options)

type options struct {
	policy Policy
}

// WithPolicy replaces the retry policy used by the retrier.
func WithPolicy(policy Policy) Option {
	return func(opts *options) {
		opts.policy = policy
	}
}

func newBase(opts ...Option) base {
	cfg := options{policy: DefaultPolicy}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return base{
		policy: cfg.policy,
		states: make(map[string]*URLState),
	}
}
