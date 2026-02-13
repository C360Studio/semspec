package llm

import "time"

// RetryConfig holds retry configuration for LLM requests.
type RetryConfig struct {
	// MaxAttempts is the maximum number of attempts per endpoint.
	MaxAttempts int

	// BackoffBase is the initial backoff duration.
	BackoffBase time.Duration

	// BackoffMultiplier is applied to backoff on each retry.
	BackoffMultiplier float64

	// MaxBackoff caps the maximum backoff duration.
	MaxBackoff time.Duration
}

// DefaultRetryConfig returns sensible retry defaults for LLM requests.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       2 * time.Second,
		BackoffMultiplier: 2.0,
		MaxBackoff:        30 * time.Second,
	}
}
