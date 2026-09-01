package examples_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-go-golems/flowkit/execution"
	"github.com/go-go-golems/flowkit/flow"
)

// ExampleMeter shows metering fresh work only. Meters convert a fresh result
// into named counters aggregated in the step's report. Cache hits are never
// metered as current-run spend. Set either Meter OR AttemptMeter, not both.
func ExampleMeters() {
	store := flow.NewMemoryStore()
	step := flow.Step[int, int]{
		Name: "metered",
		Identity: flow.Identity[int]{
			Kind: "ex-metered", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Meter:  func(v int) flow.Meters { return flow.Meters{"tokens": float64(v)} },
		Do:     func(_ context.Context, v int) (int, error) { return v, nil },
	}

	// First run: fresh work is metered (tokens = 1 + 2 = 3).
	_, rep1, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	// Second run: all hits -> no metering.
	_, rep2, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	fmt.Printf("fresh meters=%v\n", rep1.Step("metered").Meters)
	fmt.Printf("hit meters=%v\n", rep2.Step("metered").Meters)
	// Output:
	// fresh meters=map[tokens:3]
	// hit meters=map[]
}

// recordingLedger is a minimal Ledger that collects run events for inspection.
type recordingLedger struct {
	mu     sync.Mutex
	events []flow.Event
}

func (l *recordingLedger) Event(_ context.Context, e flow.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return nil
}

func (l *recordingLedger) byType(t flow.EventType) []flow.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []flow.Event
	for _, e := range l.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// ExampleLedger shows the event ledger: every observable moment of a run
// (hit, stored, retry, quarantined, skipped, done) is journaled. A ledger
// error fails the run — journals are part of the record, not best-effort.
func ExampleLedger() {
	store := flow.NewMemoryStore()
	ledger := &recordingLedger{}
	step := flow.Step[int, int]{
		Name: "journaled",
		Identity: flow.Identity[int]{
			Kind: "ex-journaled", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v, nil },
	}

	// Fresh run emits one "stored" event per item.
	if _, _, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store, Ledger: ledger}); err != nil {
		panic(err)
	}
	// Replay emits one "hit" event per item.
	if _, _, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store, Ledger: ledger}); err != nil {
		panic(err)
	}
	fmt.Printf("stored=%d hit=%d\n", len(ledger.byType(flow.EventStored)), len(ledger.byType(flow.EventHit)))
	// Output:
	// stored=2 hit=2
}

// ExampleOnResult shows the streaming result hook: OnResult observes every
// successful item (hits and fresh work, never quarantined or skipped) in
// completion order, with its input index and cache outcome. Streaming artifact
// writers hang here; an error from the hook fails the run.
func ExampleStep_onResult() {
	store := flow.NewMemoryStore()
	var seen []string
	step := flow.Step[int, int]{
		Name: "stream",
		Identity: flow.Identity[int]{
			Kind: "ex-stream", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{Workers: 1}, // deterministic order for the example
		Do:     func(_ context.Context, v int) (int, error) { return v * 10, nil },
		OnResult: func(_ context.Context, index int, value int, outcome execution.CacheOutcome) error {
			seen = append(seen, fmt.Sprintf("#%d=%d(%s)", index, value, outcome.State))
			return nil
		},
	}
	if _, _, err := flow.Run(context.Background(), step, []int{1, 2, 3}, flow.Options{Store: store}); err != nil {
		panic(err)
	}
	for _, s := range seen {
		fmt.Println(s)
	}
	// Output:
	// #0=10(stored)
	// #1=20(stored)
	// #2=30(stored)
}

// ExampleStore_swap shows the durability seam: the same Step runs unchanged
// against any Store. Here a custom in-process Store (beyond MemoryStore) proves
// the two-method contract is the whole swap.
type myStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMyStore() *myStore { return &myStore{data: map[string][]byte{}} }

func (s *myStore) Load(_ context.Context, k execution.Key, target any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := k.Digest()
	if err != nil {
		return false, err
	}
	raw, ok := s.data[d]
	if !ok {
		return false, nil
	}
	return true, jsonUnmarshal(raw, target)
}
func (s *myStore) Store(_ context.Context, k execution.Key, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := k.Digest()
	if err != nil {
		return err
	}
	raw, err := jsonMarshal(value)
	if err != nil {
		return err
	}
	s.data[d] = raw
	return nil
}

func ExampleStore_swap() {
	store := newMyStore()
	step := flow.Step[int, int]{
		Name: "swapped",
		Identity: flow.Identity[int]{
			Kind: "ex-swap", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v * 3, nil },
	}
	r1, rep1, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	_, rep2, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	fmt.Printf("run1 %d %d work=%d\n", r1[0].Value, r1[1].Value, rep1.Step("swapped").WorkCalls)
	fmt.Printf("run2 hits=%d\n", rep2.Step("swapped").Hits)
	// Output:
	// run1 3 6 work=2
	// run2 hits=2
}
