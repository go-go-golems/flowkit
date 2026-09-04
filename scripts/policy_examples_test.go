package examples_test

import (
	"context"
	"fmt"

	"github.com/go-go-golems/flowkit/execution"
	"github.com/go-go-golems/flowkit/flow"
)

// ExamplePolicy_failFast shows the default FailureMode: the first non-retryable
// item error cancels the whole run. Results are not returned; only the error.
func ExamplePolicy_failFast() {
	step := flow.Step[int, int]{
		Name:   "failfast",
		Policy: flow.Policy{Workers: 1}, // FailFast is the zero value
		Do: func(_ context.Context, v int) (int, error) {
			if v == 0 {
				return 0, fmt.Errorf("deterministic failure")
			}
			return v, nil
		},
	}
	_, _, err := flow.Run(context.Background(), step, []int{0, 1, 2}, flow.Options{})
	fmt.Println(err != nil)
	// Output:
	// true
}

// ExamplePolicy_skip shows FailureMode Skip: a bad item is dropped but a count
// and a Skipped marker remain in its result slot. The run still succeeds.
func ExamplePolicy_skip() {
	step := flow.Step[int, int]{
		Name:   "skipper",
		Policy: flow.Policy{Workers: 2, OnError: flow.Skip},
		Do: func(_ context.Context, v int) (int, error) {
			if v == 1 {
				return 0, flow.AsDataError(fmt.Errorf("drop me"))
			}
			return v, nil
		},
	}
	results, report, err := flow.Run(context.Background(), step, []int{0, 1, 2}, flow.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("item0 ok=%v item1 skipped=%v skipped_count=%d\n",
		!results[0].Skipped, results[1].Skipped, report.Step("skipper").Skipped)
	// Output:
	// item0 ok=true item1 skipped=true skipped_count=1
}

// ExamplePolicy_admission shows a finite resource budget. Every fresh work call
// (cache hits are free) consumes one unit. The report's Spend snapshot records
// how much of each declared resource was used.
func ExamplePolicy_admission() {
	step := flow.Step[int, int]{
		Name: "budgeted",
		Identity: flow.Identity[int]{
			Kind: "ex-budgeted", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{
			Workers: 2,
			Admission: []flow.Resource{{
				Name: "calls", Ceiling: 3, Budget: 5,
			}},
		},
		Do: func(_ context.Context, v int) (int, error) { return v * v, nil },
	}
	_, report, err := flow.Run(context.Background(), step, []int{1, 2, 3}, flow.Options{})
	if err != nil {
		panic(err)
	}
	snap := report.Step("budgeted").Spend["calls"]
	fmt.Printf("calls limit=%d spent=%d remaining=%d\n", snap.Limit, snap.Spent, snap.Remaining)
	// Output:
	// calls limit=5 spent=3 remaining=2
}

// ExamplePolicy_preflight shows the monetary gate: before item one runs, the
// preflight estimates the worst-case cost from each resource's Ceiling and
// UnitUSD and refuses if it exceeds the configured maximum. This fails closed
// BEFORE any provider work happens.
func ExamplePolicy_preflight() {
	unitUSD := 0.02
	step := flow.Step[int, int]{
		Name: "priced",
		Identity: flow.Identity[int]{
			Kind: "ex-priced", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{
			Admission: []flow.Resource{{
				Name: "priced-calls", Ceiling: 1000, Budget: 1000, UnitUSD: &unitUSD,
			}},
		},
		Do: func(_ context.Context, v int) (int, error) { return v, nil },
	}
	_, _, err := flow.Run(context.Background(), step, []int{1}, flow.Options{
		Preflight: &flow.Preflight{MaxEstimatedUSD: 5}, // 1000 * 0.02 = 20 > 5
	})
	fmt.Println(err != nil)
	// Output:
	// true
}

// ExamplePolicy_sharedBudgets shows Options.Share(): two Run calls made with
// the same shared Options draw from ONE budget, exactly like harness-wide
// budgets used to be. Without Share, each Run owns its budgets.
func ExamplePolicy_sharedBudgets() {
	step := flow.Step[int, int]{
		Name: "shared",
		Identity: flow.Identity[int]{
			Kind: "ex-shared", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{
			Workers: 1,
			Admission: []flow.Resource{{
				Name: "pool", Ceiling: 4, Budget: 4,
			}},
		},
		Do: func(_ context.Context, v int) (int, error) { return v, nil },
	}

	opts := flow.Options{Store: flow.NewMemoryStore()}.Share()
	if _, _, err := flow.Run(context.Background(), step, []int{1, 2}, opts); err != nil {
		panic(err)
	}
	if _, _, err := flow.Run(context.Background(), step, []int{3, 4}, opts); err != nil {
		panic(err)
	}
	// The second run's items are cache misses under a different key, so they
	// also draw from the shared "pool" budget: 2 + 2 = 4 spent total.
	snap := opts.Snapshots()["pool"]
	fmt.Printf("pool spent=%d remaining=%d\n", snap.Spent, snap.Remaining)
	// Output:
	// pool spent=4 remaining=0
}

// ExamplePolicy_rateComposedAfterBudget shows that an Options.Rates limiter is
// composed AFTER the finite budget for the named resource. Budget refusal is
// fatal and never retried; rate tokens are refunded on cancellation.
func ExamplePolicy_rateComposedAfterBudget() {
	step := flow.Step[int, int]{
		Name: "rated",
		Identity: flow.Identity[int]{
			Kind: "ex-rated", Version: "v1",
			Key: func(v int) ([]byte, error) { return []byte(fmt.Sprintf("%d", v)), nil },
		},
		Policy: flow.Policy{
			Workers: 2,
			Admission: []flow.Resource{{
				Name: "calls", Ceiling: 2, Budget: 2,
			}},
		},
		Do: func(_ context.Context, v int) (int, error) { return v, nil },
	}

	rate, err := execution.NewTokenBucket(execution.Rate{Units: 1000, Per: 1e9, Burst: 2})
	if err != nil {
		panic(err)
	}
	defer func() { _ = rate.Close() }()

	_, report, err := flow.Run(context.Background(), step, []int{1, 2}, flow.Options{
		Store: flow.NewMemoryStore(),
		Rates: map[string]execution.Limiter{"calls": rate},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("work=%d spend=%d\n",
		report.Step("rated").WorkCalls, report.Step("rated").Spend["calls"].Spent)
	// Output:
	// work=2 spend=2
}
