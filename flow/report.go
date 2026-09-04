package flow

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-go-golems/flowkit/execution"
)

// Meters aggregates named usage counters (tokens, cost) generically; domain
// adapters translate their provider usage into meter names. Only fresh work
// is metered — cache hits are free and stay free in the accounting.
type Meters map[string]float64

// Add merges other into meters, summing shared names.
func (meters Meters) Add(other Meters) {
	for name, value := range other {
		meters[name] += value
	}
}

// AddChecked merges only finite meter values and refuses an overflowing sum.
// Provider-derived cost and usage must remain serializable at the execution
// boundary rather than failing later while a durable report is encoded.
func (meters Meters) AddChecked(other Meters) error {
	for name, value := range other {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("meter %q is not finite", name)
		}
		combined := meters[name] + value
		if math.IsNaN(combined) || math.IsInf(combined, 0) {
			return fmt.Errorf("meter %q total is not finite", name)
		}
	}
	meters.Add(other)
	return nil
}

// StepReport counts one step's execution: items seen, cache traffic, retry
// pressure, quarantine and skip decisions, admission spend, and meters.
type StepReport struct {
	Items            int                                 `json:"items"`
	Hits             int                                 `json:"hits"`
	Misses           int                                 `json:"misses"`
	Stored           int                                 `json:"stored"`
	WorkCalls        int                                 `json:"work_calls"`
	StartedSequences int                                 `json:"started_sequences"`
	Retries          int                                 `json:"retries"`
	Quarantined      int                                 `json:"quarantined"`
	Skipped          int                                 `json:"skipped"`
	RetriesByClass   map[string]int                      `json:"retries_by_class,omitempty"`
	Spend            map[string]execution.BudgetSnapshot `json:"spend,omitempty"`
	Meters           Meters                              `json:"meters,omitempty"`
}

// merge adds other's counts into the report (same step name run twice).
func (report *StepReport) merge(other StepReport) {
	report.Items += other.Items
	report.Hits += other.Hits
	report.Misses += other.Misses
	report.Stored += other.Stored
	report.WorkCalls += other.WorkCalls
	report.StartedSequences += other.StartedSequences
	report.Retries += other.Retries
	report.Quarantined += other.Quarantined
	report.Skipped += other.Skipped
	for class, count := range other.RetriesByClass {
		if report.RetriesByClass == nil {
			report.RetriesByClass = map[string]int{}
		}
		report.RetriesByClass[class] += count
	}
	for name, snapshot := range other.Spend {
		if report.Spend == nil {
			report.Spend = map[string]execution.BudgetSnapshot{}
		}
		report.Spend[name] = snapshot
	}
	if len(other.Meters) > 0 {
		if report.Meters == nil {
			report.Meters = Meters{}
		}
		report.Meters.Add(other.Meters)
	}
}

// Report is the unified execution report: one StepReport per step name.
type Report struct {
	Steps map[string]StepReport `json:"steps"`
}

// merge folds other's steps into the report.
func (report *Report) merge(other Report) {
	for name, step := range other.Steps {
		if report.Steps == nil {
			report.Steps = map[string]StepReport{}
		}
		merged := report.Steps[name]
		merged.merge(step)
		report.Steps[name] = merged
	}
}

// Clone returns a fully independent report snapshot. Every map is copied so
// a reporter can retain the value while runners continue updating counters.
func (report Report) Clone() Report {
	if report.Steps == nil {
		return Report{}
	}
	cloned := Report{Steps: make(map[string]StepReport, len(report.Steps))}
	for name, step := range report.Steps {
		cloned.Steps[name] = step.Clone()
	}
	return cloned
}

// Clone returns a fully independent step report.
func (report StepReport) Clone() StepReport {
	cloned := report
	if report.RetriesByClass != nil {
		cloned.RetriesByClass = make(map[string]int, len(report.RetriesByClass))
		for class, count := range report.RetriesByClass {
			cloned.RetriesByClass[class] = count
		}
	}
	if report.Spend != nil {
		cloned.Spend = make(map[string]execution.BudgetSnapshot, len(report.Spend))
		for name, snapshot := range report.Spend {
			cloned.Spend[name] = snapshot
		}
	}
	if report.Meters != nil {
		cloned.Meters = make(Meters, len(report.Meters))
		cloned.Meters.Add(report.Meters)
	}
	return cloned
}

// Step returns the named step's report (zero value when absent).
func (report Report) Step(name string) StepReport {
	return report.Steps[name]
}

// Snapshot is one immutable aggregate observation of a running flow. Sequence
// increases within one top-level Run call. Report is a deep clone and may be
// retained by the receiver after Report returns.
type Snapshot struct {
	Sequence  uint64    `json:"sequence"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Total     int       `json:"total"`
	Terminal  bool      `json:"terminal"`
	Report    Report    `json:"report"`
}

// Reporter receives periodic and terminal aggregate snapshots. A reporter
// error fails the run: callers that want best-effort telemetry should wrap
// their reporter and deliberately swallow its errors.
type Reporter interface {
	Report(context.Context, Snapshot) error
}

// ReporterFunc adapts a function to Reporter.
type ReporterFunc func(context.Context, Snapshot) error

// Report implements Reporter.
func (f ReporterFunc) Report(ctx context.Context, snapshot Snapshot) error {
	return f(ctx, snapshot)
}

// Result carries one item's outcome, position-aligned with the input:
// Results[i] always corresponds to items[i] — evaluation joins depend on it.
type Result[O any] struct {
	Value O `json:"value"`
	// Cache records the durable state of this item (hit/stored/pending).
	// Zero for uncached steps.
	Cache execution.CacheOutcome `json:"cache,omitempty"`
	// Quarantined is set iff the step quarantined this item; Value is the
	// zero value then.
	Quarantined *ItemError `json:"quarantined,omitempty"`
	// Skipped marks an item dropped by FailureMode Skip.
	Skipped bool `json:"skipped,omitempty"`
}

// EventType names one ledger event.
type EventType string

const (
	// EventRunStarted records the boundary after validation and preflight and
	// before the first item is admitted.
	EventRunStarted EventType = "run_started"
	// EventStepStarted records one pipeline stage runner starting. Streaming
	// downstream stages may start before their first item arrives.
	EventStepStarted EventType = "step_started"
	// EventStepCompleted records one stage draining successfully.
	EventStepCompleted EventType = "step_completed"
	// EventRunCompleted records a successful terminal run boundary.
	EventRunCompleted EventType = "run_completed"
	// EventRunFailed records a failed or canceled terminal run boundary.
	EventRunFailed EventType = "run_failed"
	// EventHit records a cache hit (free).
	EventHit EventType = "hit"
	// EventStored records fresh work committed to the store.
	EventStored EventType = "stored"
	// EventDone records fresh uncached work.
	EventDone EventType = "done"
	// EventRetry records one retry attempt about to back off.
	EventRetry EventType = "retry"
	// EventQuarantined records an item error kept as data.
	EventQuarantined EventType = "quarantined"
	// EventSkipped records an item dropped by policy.
	EventSkipped EventType = "skipped"
)

// Event is one observable moment of a run, suitable for appending to an
// experiment run's JSONL journal.
type Event struct {
	At      time.Time `json:"at"`
	Step    string    `json:"step,omitempty"`
	Index   int       `json:"index"`
	Type    EventType `json:"type"`
	Class   string    `json:"class,omitempty"`
	Attempt int       `json:"attempt,omitempty"`
	Total   int       `json:"total,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// Ledger consumes run events. Implementations typically adapt
// experiment.Run.AppendJSONL; a ledger error fails the run (journals are
// part of the record, not best-effort).
type Ledger interface {
	Event(ctx context.Context, event Event) error
}
