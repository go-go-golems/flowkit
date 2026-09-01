package examples_test

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-go-golems/flowkit/flow"
)

// ExamplePipe2 composes two typed steps into one. Items stream per-item: as
// soon as item i finishes stage 1, it enters stage 2 — no barrier unless a
// stage declares one. The final results are position-aligned with the input.
func ExamplePipe2() {
	double := flow.Step[int, int]{
		Name:   "double",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v * 2, nil },
	}
	format := flow.Step[int, string]{
		Name:   "format",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (string, error) { return fmt.Sprintf("value=%d", v), nil },
	}

	results, report, err := flow.Run(context.Background(), flow.Pipe2(double, format),
		[]int{3, 1, 2}, flow.Options{})
	if err != nil {
		panic(err)
	}
	for _, r := range results {
		fmt.Println(r.Value)
	}
	fmt.Printf("double items=%d format items=%d\n",
		report.Step("double").Items, report.Step("format").Items)
	// Output:
	// value=6
	// value=2
	// value=4
	// double items=3 format items=3
}

// ExamplePipe3 chains three stages. Each stage keeps its own Policy and its
// own entry in the report. Pipelines beyond arity four should prefer nesting
// (Pipe2(Pipe2(a,b), c)) — stage lists flatten, so nesting is free at runtime.
func ExamplePipe3() {
	toInt := flow.Step[string, int]{
		Name:   "parse",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, s string) (int, error) { return strconv.Atoi(s) },
	}
	double := flow.Step[int, int]{
		Name:   "double",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (int, error) { return v * 2, nil },
	}
	format := flow.Step[int, string]{
		Name:   "format",
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, v int) (string, error) { return fmt.Sprintf("=> %d", v), nil },
	}

	results, _, err := flow.Run(context.Background(),
		flow.Pipe3(toInt, double, format),
		[]string{"5", "10"}, flow.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println(results[0].Value)
	fmt.Println(results[1].Value)
	// Output:
	// => 10
	// => 20
}

// ExamplePipe_quarantineBypass shows that an item quarantined (or skipped) by a
// stage bypasses every later stage and surfaces at its original position. The
// run still succeeds under Quarantine.
func ExamplePipe_quarantineBypass() {
	parse := flow.Step[string, int]{
		Name:   "parse",
		Policy: flow.Policy{Workers: 1, OnError: flow.Quarantine},
		Do: func(_ context.Context, s string) (int, error) {
			if s == "bad" {
				return 0, flow.AsDataError(fmt.Errorf("unparseable"))
			}
			return strconv.Atoi(s)
		},
	}
	inc := flow.Step[int, int]{
		Name:   "inc",
		Policy: flow.Policy{Workers: 1},
		Do:     func(_ context.Context, v int) (int, error) { return v + 1, nil },
	}

	results, report, err := flow.Run(context.Background(), flow.Pipe2(parse, inc),
		[]string{"1", "bad", "2"}, flow.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("item0=%d item2=%d quarantined_at=%d\n",
		results[0].Value, results[2].Value, results[1].Quarantined.Index)
	fmt.Printf("parse quarantined=%d inc items=%d\n",
		report.Step("parse").Quarantined, report.Step("inc").Items)
	// Output:
	// item0=2 item2=3 quarantined_at=1
	// parse quarantined=1 inc items=2
}
