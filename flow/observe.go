package flow

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const terminalReportTimeout = 5 * time.Second

type observationState struct {
	mutex     sync.Mutex
	sequence  uint64
	startedAt time.Time
}

func (o Options) now() time.Time {
	if o.Clock != nil {
		return o.Clock().UTC()
	}
	return time.Now().UTC()
}

func (o Options) withObservation() Options {
	if o.observation == nil {
		o.observation = &observationState{startedAt: o.now()}
	}
	return o
}

func (o Options) snapshot(report Report, total int, terminal bool) Snapshot {
	state := o.observation
	if state == nil {
		state = &observationState{startedAt: o.now()}
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.sequence++
	return Snapshot{
		Sequence:  state.sequence,
		StartedAt: state.startedAt,
		UpdatedAt: o.now(),
		Total:     total,
		Terminal:  terminal,
		Report:    report.Clone(),
	}
}

func (o Options) publish(ctx context.Context, report Report, total int, terminal bool) error {
	if o.Reporter == nil {
		return nil
	}
	if err := o.Reporter.Report(ctx, o.snapshot(report, total, terminal)); err != nil {
		return fmt.Errorf("progress reporter: %w", err)
	}
	return nil
}

func (o Options) publishTerminal(ctx context.Context, report Report, total int) error {
	if o.Reporter == nil {
		return nil
	}
	terminalContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalReportTimeout)
	defer cancel()
	return o.publish(terminalContext, report, total, true)
}

func emitLedger(ctx context.Context, o Options, event Event) error {
	if o.Ledger == nil {
		return nil
	}
	if event.At.IsZero() {
		event.At = o.now()
	}
	if err := o.Ledger.Event(ctx, event); err != nil {
		return fmt.Errorf("ledger event: %w", err)
	}
	return nil
}

// startPeriodicReporter returns a context canceled when the reporter fails and
// a stop function that joins the reporter goroutine and returns its error.
func startPeriodicReporter(
	ctx context.Context,
	o Options,
	total int,
	current func() Report,
) (context.Context, func() error) {
	if o.Reporter == nil || o.ReportInterval <= 0 {
		return ctx, func() error { return nil }
	}
	runContext, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	go func() {
		ticker := time.NewTicker(o.ReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-runContext.Done():
				done <- nil
				return
			case <-ticker.C:
				if err := o.publish(runContext, current(), total, false); err != nil {
					done <- err
					cancel()
					return
				}
			}
		}
	}()
	return runContext, func() error {
		once.Do(func() { close(stop) })
		err := <-done
		cancel()
		return err
	}
}
