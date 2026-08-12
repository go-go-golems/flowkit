package flow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/go-go-golems/flowkit/execution"
)

func TestErrorClassJSONRoundTrip(t *testing.T) {
	for _, class := range []ErrorClass{Transient, DataError, Fatal} {
		encoded, err := json.Marshal(class)
		require.NoError(t, err)
		var decoded ErrorClass
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.Equal(t, class, decoded)
	}

	var decoded ErrorClass
	require.Error(t, json.Unmarshal([]byte(`"unknown"`), &decoded))
}

func TestDefaultClassifierTypedStatuses(t *testing.T) {
	cases := []struct {
		status int
		class  ErrorClass
	}{
		{429, Transient},
		{408, Transient},
		{500, Transient},
		{503, Transient},
		{599, Transient},
		{600, Fatal},
		{999, Fatal},
		{400, Fatal},
		{404, Fatal},
		{401, Fatal},
	}
	for _, testCase := range cases {
		err := errors.Wrap(&StatusError{Status: testCase.status, Err: errors.New("nope")}, "call")
		require.Equal(t, testCase.class, DefaultClassifier.Classify(err), "status %d", testCase.status)
	}
}

func TestDefaultClassifierNeverRetriesCancellation(t *testing.T) {
	require.Equal(t, Fatal, DefaultClassifier.Classify(context.Canceled))
	require.Equal(t, Fatal, DefaultClassifier.Classify(context.DeadlineExceeded))
	require.Equal(t, Fatal, DefaultClassifier.Classify(errors.Wrap(context.Canceled, "generate")))
}

func TestDefaultClassifierBudgetExhaustionIsFatal(t *testing.T) {
	err := errors.Wrap(execution.ErrBudgetExceeded, "wait for resources")
	require.Equal(t, Fatal, DefaultClassifier.Classify(err))
}

func TestDefaultClassifierDataErrorMarker(t *testing.T) {
	parseErr := AsDataError(errors.New("judge verdict JSON missing 'verdicts' array"))
	require.Equal(t, DataError, DefaultClassifier.Classify(parseErr))
	require.Equal(t, DataError, DefaultClassifier.Classify(errors.Wrap(parseErr, "statements pass")))
	require.True(t, IsDataError(parseErr))
	require.Nil(t, AsDataError(nil))
	require.Equal(t, DataError, DefaultClassifier.Classify(AsDataError(errors.New("unexpected EOF while parsing"))))
}

func TestDefaultClassifierUnknownErrorsAreFatal(t *testing.T) {
	require.Equal(t, Fatal, DefaultClassifier.Classify(errors.New("something entirely new")))
}
