package flow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingReporter struct {
	mutex     sync.Mutex
	snapshots []Snapshot
	fail      func(Snapshot) error
}

func (reporter *recordingReporter) Report(_ context.Context, snapshot Snapshot) error {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()
	reporter.snapshots = append(reporter.snapshots, snapshot)
	if reporter.fail != nil {
		return reporter.fail(snapshot)
	}
	return nil
}

func (reporter *recordingReporter) values() []Snapshot {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()
	return append([]Snapshot(nil), reporter.snapshots...)
}

func TestRunWithoutObserversDoesNotInitializeObservationClock(t *testing.T) {
	clockCalls := 0
	results, report, err := Run(t.Context(), doubler("plain", Policy{}), []int{1}, Options{
		Clock: func() time.Time {
			clockCalls++
			return time.Unix(1, 0)
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, results[0].Value)
	require.Equal(t, 1, report.Step("plain").Items)
	require.Zero(t, clockCalls)
}

func TestRunReporterReceivesInitialAndTerminalSnapshots(t *testing.T) {
	reporter := &recordingReporter{}
	clock := func() time.Time { return time.Unix(100, 0) }
	results, report, err := Run(t.Context(), doubler("observed", Policy{}), []int{1, 2}, Options{
		Reporter: reporter,
		Clock:    clock,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	snapshots := reporter.values()
	require.Len(t, snapshots, 2)
	require.Equal(t, uint64(1), snapshots[0].Sequence)
	require.False(t, snapshots[0].Terminal)
	require.Empty(t, snapshots[0].Report.Steps)
	require.Equal(t, uint64(2), snapshots[1].Sequence)
	require.True(t, snapshots[1].Terminal)
	require.Equal(t, 2, snapshots[1].Total)
	require.Equal(t, report, snapshots[1].Report)
	require.Equal(t, time.Unix(100, 0).UTC(), snapshots[1].StartedAt)
	require.Equal(t, time.Unix(100, 0).UTC(), snapshots[1].UpdatedAt)
}

func TestRunReporterReceivesPeriodicSnapshots(t *testing.T) {
	reporter := &recordingReporter{}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	step := Step[int, int]{
		Name: "slow",
		Do: func(ctx context.Context, value int) (int, error) {
			once.Do(func() { close(started) })
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-release:
				return value, nil
			}
		},
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := Run(t.Context(), step, []int{1}, Options{
			Reporter:       reporter,
			ReportInterval: time.Millisecond,
		})
		done <- err
	}()
	<-started
	require.Eventually(t, func() bool {
		for _, snapshot := range reporter.values() {
			if !snapshot.Terminal && snapshot.Report.Step("slow").Items == 1 {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	close(release)
	require.NoError(t, <-done)

	snapshots := reporter.values()
	require.GreaterOrEqual(t, len(snapshots), 3)
	for index := 1; index < len(snapshots); index++ {
		require.Greater(t, snapshots[index].Sequence, snapshots[index-1].Sequence)
	}
	require.True(t, snapshots[len(snapshots)-1].Terminal)
}

func TestPeriodicReporterFailureCancelsRun(t *testing.T) {
	reporter := &recordingReporter{fail: func(snapshot Snapshot) error {
		if snapshot.Sequence >= 2 && !snapshot.Terminal {
			return errors.New("progress disk full")
		}
		return nil
	}}
	step := Step[int, int]{
		Name: "blocked",
		Do: func(ctx context.Context, value int) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}
	_, _, err := Run(t.Context(), step, []int{1}, Options{
		Reporter:       reporter,
		ReportInterval: time.Millisecond,
	})
	require.ErrorContains(t, err, "progress reporter")
	require.ErrorContains(t, err, "progress disk full")
}

func TestTerminalReporterFailureFailsRun(t *testing.T) {
	reporter := &recordingReporter{fail: func(snapshot Snapshot) error {
		if snapshot.Terminal {
			return errors.New("terminal write failed")
		}
		return nil
	}}
	_, report, err := Run(t.Context(), doubler("terminal", Policy{}), []int{2}, Options{Reporter: reporter})
	require.ErrorContains(t, err, "terminal write failed")
	require.Equal(t, 1, report.Step("terminal").Items)
}

func TestLifecycleLedgerOrderAndTimestamps(t *testing.T) {
	ledger := &recordingLedger{}
	clock := func() time.Time { return time.Unix(200, 0) }
	_, _, err := Run(t.Context(), doubler("lifecycle", Policy{}), []int{1}, Options{Ledger: ledger, Clock: clock})
	require.NoError(t, err)

	ledger.mutex.Lock()
	events := append([]Event(nil), ledger.events...)
	ledger.mutex.Unlock()
	require.Equal(t, EventRunStarted, events[0].Type)
	require.Equal(t, EventStepStarted, events[1].Type)
	require.Equal(t, EventStepCompleted, events[len(events)-2].Type)
	require.Equal(t, EventRunCompleted, events[len(events)-1].Type)
	for _, event := range events {
		require.Equal(t, time.Unix(200, 0).UTC(), event.At)
	}
}

func TestBulkReporterTerminalParity(t *testing.T) {
	reporter := &recordingReporter{}
	ledger := &recordingLedger{}
	base := doubler("bulk-observed", Policy{Workers: 2})
	step := Bulk(base, func(_ context.Context, values []int) ([]int, error) {
		out := make([]int, len(values))
		for index, value := range values {
			out[index] = value * 2
		}
		return out, nil
	}, 2)
	_, report, err := Run(t.Context(), step, []int{1, 2, 3}, Options{Reporter: reporter, Ledger: ledger})
	require.NoError(t, err)
	snapshots := reporter.values()
	require.Equal(t, report, snapshots[len(snapshots)-1].Report)
	require.True(t, snapshots[len(snapshots)-1].Terminal)
	require.Len(t, ledger.byType(EventRunStarted), 1)
	require.Len(t, ledger.byType(EventRunCompleted), 1)
	require.Len(t, ledger.byType(EventStepStarted), 1)
	require.Len(t, ledger.byType(EventStepCompleted), 1)
}

func TestBatchedNestedRunsEmitOneRunLifecycle(t *testing.T) {
	ledger := &recordingLedger{}
	repair := doubler("repair", Policy{})
	step := Batched(repair, BatchSpec[int, int]{
		Name:  "groups",
		Group: func(values []int) [][]int { return [][]int{{0, 1}} },
		DoAll: func(context.Context, []int) (string, error) { return "partial", nil },
		Split: func(_ string, _ []int) (map[int]int, error) {
			return map[int]int{0: 2}, nil // index 1 is repaired
		},
	})
	results, _, err := Run(t.Context(), step, []int{1, 2}, Options{Ledger: ledger})
	require.NoError(t, err)
	require.Equal(t, 2, results[0].Value)
	require.Equal(t, 4, results[1].Value)
	require.Len(t, ledger.byType(EventRunStarted), 1)
	require.Len(t, ledger.byType(EventRunCompleted), 1)
	require.GreaterOrEqual(t, len(ledger.byType(EventStepStarted)), 3) // outer, groups, repair
}

func TestFailedRunEmitsFailedLifecycleAndTerminalSnapshot(t *testing.T) {
	ledger := &recordingLedger{}
	reporter := &recordingReporter{}
	step := Step[int, int]{Name: "fails", Do: func(context.Context, int) (int, error) {
		return 0, errors.New("boom")
	}}
	_, _, err := Run(t.Context(), step, []int{1}, Options{Ledger: ledger, Reporter: reporter})
	require.ErrorContains(t, err, "boom")
	require.Len(t, ledger.byType(EventRunFailed), 1)
	require.Empty(t, ledger.byType(EventRunCompleted))
	snapshots := reporter.values()
	require.True(t, snapshots[len(snapshots)-1].Terminal)
}
