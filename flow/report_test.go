package flow

import (
	"context"
	"testing"
	"time"

	"github.com/go-go-golems/flowkit/execution"
	"github.com/stretchr/testify/require"
)

func TestReportCloneDoesNotAliasMaps(t *testing.T) {
	report := Report{Steps: map[string]StepReport{
		"generate": {
			Items:          2,
			RetriesByClass: map[string]int{"transient": 1},
			Spend: map[string]execution.BudgetSnapshot{
				"generation": {Limit: 4, Spent: 2, Remaining: 2},
			},
			Meters: Meters{"tokens": 12},
		},
	}}

	cloned := report.Clone()
	original := report.Steps["generate"]
	original.RetriesByClass["transient"] = 9
	original.Spend["generation"] = execution.BudgetSnapshot{Limit: 4, Spent: 4}
	original.Meters["tokens"] = 99
	report.Steps["generate"] = StepReport{Items: 100}

	got := cloned.Step("generate")
	require.Equal(t, 2, got.Items)
	require.Equal(t, 1, got.RetriesByClass["transient"])
	require.Equal(t, execution.BudgetSnapshot{Limit: 4, Spent: 2, Remaining: 2}, got.Spend["generation"])
	require.Equal(t, float64(12), got.Meters["tokens"])
}

func TestReportClonePreservesNilMaps(t *testing.T) {
	cloned := (Report{}).Clone()
	require.Nil(t, cloned.Steps)

	step := (StepReport{}).Clone()
	require.Nil(t, step.RetriesByClass)
	require.Nil(t, step.Spend)
	require.Nil(t, step.Meters)
}

func TestReporterFunc(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	want := Snapshot{Sequence: 2, UpdatedAt: at, Terminal: true}
	var got Snapshot
	reporter := ReporterFunc(func(_ context.Context, snapshot Snapshot) error {
		got = snapshot
		return nil
	})
	require.NoError(t, reporter.Report(t.Context(), want))
	require.Equal(t, want, got)
}
