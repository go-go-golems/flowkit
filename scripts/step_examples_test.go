package examples_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/go-go-golems/flowkit/flow"
)

// ExampleStep_cached is the canonical flow example: a typed Step with an
// Identity (cache key) and a Policy (how it runs). The first run computes;
// the second run is all cache hits. Duplicate input keys execute once per
// process and populate every matching position.
func ExampleStep_cached() {
	store := flow.NewMemoryStore()
	step := flow.Step[int, int]{
		Name: "double",
		Identity: flow.Identity[int]{
			Kind:    "example-double",
			Version: "v1",
			Key:     func(v int) ([]byte, error) { return []byte(strconv.Itoa(v)), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v * 2, nil },
	}

	// Inputs [1,2,2]: key 2 is duplicate, so Do runs only twice (work=2).
	r1, rep1, err := flow.Run(context.Background(), step, []int{1, 2, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}

	// Second run: all three positions are hits (work=0).
	r2, rep2, err := flow.Run(context.Background(), step, []int{1, 2, 2}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}

	s := func(r []flow.Result[int]) string {
		return fmt.Sprintf("%d %d %d", r[0].Value, r[1].Value, r[2].Value)
	}
	fmt.Printf("run1 values=[%s] hits=%d misses=%d work=%d\n",
		s(r1), rep1.Step("double").Hits, rep1.Step("double").Misses, rep1.Step("double").WorkCalls)
	fmt.Printf("run2 values=[%s] hits=%d misses=%d work=%d\n",
		s(r2), rep2.Step("double").Hits, rep2.Step("double").Misses, rep2.Step("double").WorkCalls)
	// Output:
	// run1 values=[2 4 4] hits=0 misses=3 work=2
	// run2 values=[2 4 4] hits=3 misses=0 work=0
}

// ExampleStep_uncached shows that an empty Identity.Kind makes a step pure
// compute: no store, no cache, just bounded work over the inputs.
func ExampleStep_uncached() {
	step := flow.Step[int, int]{
		Name:   "pure-double",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v * 2, nil },
	}
	results, report, err := flow.Run(context.Background(), step, []int{1, 2, 3}, flow.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println(results[0].Value, results[1].Value, results[2].Value)
	fmt.Println("work=", report.Step("pure-double").WorkCalls)
	// Output:
	// 2 4 6
	// work= 3
}

// ExampleStep_retry shows transient-error retry with exponential backoff. The
// default classifier retries typed HTTP 408/429/5xx; here we use a custom
// Classifier that treats a sentinel error as transient. Each attempt pays
// admission separately (retries cannot escape a budget).
func ExampleStep_retry() {
	var attempts int
	step := flow.Step[int, int]{
		Name: "flaky",
		Policy: flow.Policy{
			Workers: 1,
			Retry: flow.RetrySpec{
				Attempts: 3,
				Backoff:  flow.Backoff{Base: 1, Cap: 2}, // fast for the example
				Class: flow.ClassifierFunc(func(err error) flow.ErrorClass {
					if errors.Is(err, errFlaky) {
						return flow.Transient
					}
					return flow.Fatal
				}),
			},
		},
		Do: func(_ context.Context, v int) (int, error) {
			attempts++
			if attempts < 3 {
				return 0, errFlaky
			}
			return v * 10, nil
		},
	}
	results, report, err := flow.Run(context.Background(), step, []int{7}, flow.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("value=%d attempts=%d retries=%d work=%d\n",
		results[0].Value, attempts, report.Step("flaky").Retries, report.Step("flaky").WorkCalls)
	// Output:
	// value=70 attempts=3 retries=2 work=3
}

// ExampleStep_dataError shows AsDataError: a well-transported but malformed
// response is classified as DataError (never retried) and handled by the
// step's FailureMode. Under Quarantine the bad item becomes a record and the
// run succeeds.
func ExampleStep_dataError() {
	step := flow.Step[int, int]{
		Name:   "parse",
		Policy: flow.Policy{Workers: 2, OnError: flow.Quarantine, Retry: flow.RetrySpec{Attempts: 2}},
		Do: func(_ context.Context, v int) (int, error) {
			if v == 2 {
				return 0, flow.AsDataError(errors.New("malformed response"))
			}
			return v * 10, nil
		},
	}
	results, report, err := flow.Run(context.Background(), step, []int{1, 2, 3}, flow.Options{})
	if err != nil {
		panic(err) // quarantine keeps the run green
	}
	fmt.Printf("ok=%d %d quarantined=%d\n", results[0].Value, results[2].Value, report.Step("parse").Quarantined)
	fmt.Printf("bad item class=%s message=%q\n", results[1].Quarantined.Class, results[1].Quarantined.Message)
	// Output:
	// ok=10 30 quarantined=1
	// bad item class=data message="malformed response"
}

// errFlaky is a sentinel transient error used by the retry example.
var errFlaky = errors.New("transient provider failure")
