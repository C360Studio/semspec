package llm

import (
	"errors"
)

// Error types for classifying LLM errors.

// TransientError represents a temporary error that may succeed on retry.
type TransientError struct {
	err error
}

func (e *TransientError) Error() string {
	return e.err.Error()
}

func (e *TransientError) Unwrap() error {
	return e.err
}

// NewTransientError wraps an error as transient (retryable).
func NewTransientError(err error) error {
	return &TransientError{err: err}
}

// FatalError represents a permanent error that should not be retried.
type FatalError struct {
	err error
}

func (e *FatalError) Error() string {
	return e.err.Error()
}

func (e *FatalError) Unwrap() error {
	return e.err
}

// NewFatalError wraps an error as fatal (non-retryable).
func NewFatalError(err error) error {
	return &FatalError{err: err}
}

// IsTransient returns true if the error is transient and should be retried.
func IsTransient(err error) bool {
	var transient *TransientError
	return errors.As(err, &transient)
}

// IsFatal returns true if the error is fatal and should not be retried.
func IsFatal(err error) bool {
	var fatal *FatalError
	return errors.As(err, &fatal)
}
