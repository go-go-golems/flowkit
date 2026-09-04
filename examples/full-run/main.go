// Command full-run is a single, end-to-end Flowkit program: it builds a
// cached, retryable, budgeted step over a durable on-disk FileCache, runs it
// twice, and prints the report so you can see "resume = replay" happen.
//
// Run it with:
//
//	go run ./examples/full-run
//
// For the exhaustive, verified per-API reference, see the Example functions in
// ../../scripts (run with `go test ./scripts/ -v`).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-go-golems/flowkit/execution"
	"github.com/go-go-golems/flowkit/flow"
)

// fakeProvider is a stand-in for an expensive, occasionally-flaky API call.
// It counts how many times it is actually invoked so the report can prove
// caching works. failedOnce records which items have already burned their
// one transient failure, so the retry path is deterministic. Because the
// step runs with Workers > 1, every access to this shared state is guarded by
// a mutex so the example stays race-free and deterministic under concurrent
// execution.
type fakeProvider struct {
	mu         sync.Mutex
	calls      int
	failedOnce map[int]bool
}

func (p *fakeProvider) summarize(_ context.Context, n int) (int, error) {
	p.mu.Lock()
	if p.failedOnce == nil {
		p.failedOnce = map[int]bool{}
	}
	p.calls++
	current := p.calls
	// Item 3 fails once with a transient 503, then succeeds on retry.
	// A typed *flow.StatusError lets DefaultClassifier classify 5xx as Transient.
	if n == 3 && !p.failedOnce[n] {
		p.failedOnce[n] = true
		p.mu.Unlock()
		return 0, &flow.StatusError{Status: 503, Err: fmt.Errorf("transient (attempt %d)", current)}
	}
	p.mu.Unlock()
	return n * 10, nil
}

// reset clears the call counter between runs (the cache makes the provider a
// no-op on replay, so this only affects reporting).
func (p *fakeProvider) reset() {
	p.mu.Lock()
	p.calls = 0
	p.mu.Unlock()
}

func main() {
	ctx := context.Background()

	// The on-disk cache makes completed work durable. By default this uses a
	// throwaway temp directory cleaned up at exit, which demonstrates
	// same-process replay (run 2 reuses run 1's entries). To see true
	// cross-process resume, set FLOWKIT_FULL_RUN_CACHE to a stable path and run
	// the program twice: the second invocation replays the first's cache hits.
	// (This mirrors how a real application points FileCache at a fixed dir.)
	dir := os.Getenv("FLOWKIT_FULL_RUN_CACHE")
	cleanup := func() {}
	if dir == "" {
		tmp, err := os.MkdirTemp("", "flowkit-full-run-*")
		if err != nil {
			panic(err)
		}
		dir = tmp
		cleanup = func() { _ = os.RemoveAll(tmp) }
	}
	defer cleanup()

	store, err := execution.NewFileCache(execution.FileCacheOptions{Directory: dir})
	if err != nil {
		panic(err)
	}

	provider := &fakeProvider{}
	unitUSD := 0.001
	step := flow.Step[int, int]{
		Name: "summarize",
		Identity: flow.Identity[int]{
			Kind:    "full-run-summarize",
			Version: "v1",
			// The exact bytes the result depends on. Change the model/prompt
			// here and you get a new key; change workers/retry and you don't.
			Key: func(n int) ([]byte, error) { return []byte(strconv.Itoa(n)), nil },
		},
		Policy: flow.Policy{
			Workers: 3,
			Admission: []flow.Resource{{
				Name: "provider-calls", Ceiling: 10, Budget: 20, UnitUSD: &unitUSD,
			}},
			Retry:   flow.RetrySpec{Attempts: 3, Backoff: flow.Backoff{Base: 10 * time.Millisecond, Cap: 50 * time.Millisecond}},
			OnError: flow.Quarantine,
		},
		Do: provider.summarize,
	}

	items := []int{1, 2, 3, 4, 5}

	// Run 1: every item is a miss (item 3 retries once then succeeds).
	r1, rep1, err := flow.Run(ctx, step, items, flow.Options{
		Store: store,
		Preflight: &flow.Preflight{
			MaxEstimatedUSD: 1,
			AllowUnpriced:   false,
		},
	})
	if err != nil {
		panic(err)
	}
	printRun("run 1 (fresh)", r1, rep1, provider.calls)

	// Run 2: same inputs, same on-disk cache -> everything is a hit, the
	// provider is never called. This is same-process replay; with
	// FLOWKIT_FULL_RUN_CACHE set, a second process would see the same hits.
	provider.reset()
	r2, rep2, err := flow.Run(ctx, step, items, flow.Options{
		Store: store,
		Preflight: &flow.Preflight{
			MaxEstimatedUSD: 1,
			AllowUnpriced:   false,
		},
	})
	if err != nil {
		panic(err)
	}
	printRun("run 2 (replay)", r2, rep2, provider.calls)

	// Demonstrate quarantine on a brand-new bad item in a third run.
	quarantineStep := step
	quarantineStep.Do = func(ctx context.Context, n int) (int, error) {
		if n == 6 {
			return 0, flow.AsDataError(errors.New("malformed response"))
		}
		return provider.summarize(ctx, n)
	}
	provider.reset()
	r3, rep3, err := flow.Run(ctx, quarantineStep, []int{1, 6}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	printRun("run 3 (quarantine)", r3, rep3, provider.calls)
	if r3[1].Quarantined != nil {
		fmt.Printf("  item #6 quarantined: class=%s msg=%q (not stored)\n",
			r3[1].Quarantined.Class, r3[1].Quarantined.Message)
	}
}

func printRun(label string, results []flow.Result[int], rep flow.Report, calls int) {
	s := rep.Step("summarize")
	values := make([]string, len(results))
	for i, r := range results {
		if r.Quarantined != nil {
			values[i] = "QUARANTINED"
		} else if r.Skipped {
			values[i] = "SKIPPED"
		} else {
			values[i] = strconv.Itoa(r.Value)
		}
	}
	fmt.Printf("%s: values=%v hits=%d misses=%d work=%d retries=%d quarantined=%d provider_calls=%d spend=%d/%d\n",
		label, values, s.Hits, s.Misses, s.WorkCalls, s.Retries, s.Quarantined, calls,
		s.Spend["provider-calls"].Spent, s.Spend["provider-calls"].Limit)
}
