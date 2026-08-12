package flow

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-go-golems/flowkit/execution"
)

// ErrorClass is the destiny of one item error.
type ErrorClass int

const (
	// Transient errors retry with backoff.
	Transient ErrorClass = iota
	// DataError is a well-transported but malformed response: never retried,
	// honored per the step's FailureMode. Providers cannot produce it — the
	// step's parser does, by wrapping with AsDataError.
	DataError
	// Fatal errors always fail the run, regardless of FailureMode.
	Fatal
)

func (class ErrorClass) valid() bool { return class >= Transient && class <= Fatal }

// String names the class for reports and logs.
func (class ErrorClass) String() string {
	switch class {
	case Transient:
		return "transient"
	case DataError:
		return "data"
	case Fatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// MarshalText serializes the class name into quarantine records.
func (class ErrorClass) MarshalText() ([]byte, error) {
	if class < Transient || class > Fatal {
		return nil, fmt.Errorf("unknown error class %d", class)
	}
	return []byte(class.String()), nil
}

// UnmarshalText restores the stable names emitted by MarshalText.
func (class *ErrorClass) UnmarshalText(text []byte) error {
	if class == nil {
		return errors.New("error class destination is nil")
	}
	switch string(text) {
	case "transient":
		*class = Transient
	case "data":
		*class = DataError
	case "fatal":
		*class = Fatal
	default:
		return fmt.Errorf("unknown error class %q", text)
	}
	return nil
}

// Classifier assigns one item error its class.
type Classifier interface {
	Classify(error) ErrorClass
}

// ClassifierFunc adapts a function to Classifier.
type ClassifierFunc func(error) ErrorClass

// Classify implements Classifier.
func (f ClassifierFunc) Classify(err error) ErrorClass { return f(err) }

// dataError marks a parser verdict on a well-transported response.
type dataError struct{ err error }

func (e *dataError) Error() string { return e.err.Error() }
func (e *dataError) Unwrap() error { return e.err }

// AsDataError wraps a step parser's error so the classifier files it as
// DataError instead of Fatal. A nil error stays nil.
func AsDataError(err error) error {
	if err == nil {
		return nil
	}
	return &dataError{err: err}
}

// IsDataError reports whether err carries the DataError marker.
func IsDataError(err error) bool {
	var marker *dataError
	return errors.As(err, &marker)
}

// StatusError is a typed provider error carrying an HTTP status. Adapters
// that know their transport status should wrap with it so classification
// stops depending on substrings (tier one of DefaultClassifier).
type StatusError struct {
	Status int
	Err    error
}

// Error implements error.
func (e *StatusError) Error() string {
	return fmt.Sprintf("status=%d: %v", e.Status, e.Err)
}

// Unwrap exposes the wrapped error.
func (e *StatusError) Unwrap() error { return e.Err }

// HTTPStatus implements the typed-status interface tier one matches on.
func (e *StatusError) HTTPStatus() int { return e.Status }

// defaultClassifier applies only domain-neutral, typed rules. Applications may
// provide a classifier through RetrySpec when they need provider-specific or
// legacy string matching.
type defaultClassifier struct{}

// DefaultClassifier handles explicit data markers, context cancellation, budget
// exhaustion, and typed HTTP statuses. Anything unrecognized is fatal.
var DefaultClassifier Classifier = defaultClassifier{}

// Classify implements Classifier.
func (defaultClassifier) Classify(err error) ErrorClass {
	if err == nil {
		return Fatal
	}
	if IsDataError(err) {
		return DataError
	}
	// Tier two first for typed sentinels: cancellation is a caller verdict
	// and budget exhaustion is the experiment ceiling — retrying either
	// would fight the operator.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Fatal
	}
	if errors.Is(err, execution.ErrBudgetExceeded) {
		return Fatal
	}
	// Tier one: typed provider statuses.
	var status interface{ HTTPStatus() int }
	if errors.As(err, &status) {
		code := status.HTTPStatus()
		switch {
		case code == 429 || code == 408 || (code >= 500 && code < 600):
			return Transient
		default:
			return Fatal
		}
	}
	return Fatal
}
