package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-go-golems/flowkit/flow"
)

func main() {
	store := flow.NewMemoryStore()
	step := flow.Step[int, int]{
		Name: "double",
		Identity: flow.Identity[int]{
			Kind: "example-double", Version: "v1",
			Key: func(value int) ([]byte, error) { return []byte(strconv.Itoa(value)), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Do:     func(_ context.Context, value int) (int, error) { return value * 2, nil },
	}

	for run := 1; run <= 2; run++ {
		results, report, err := flow.Run(context.Background(), step, []int{1, 2, 2}, flow.Options{Store: store})
		if err != nil {
			panic(err)
		}
		fmt.Printf("run %d: values=[%d %d %d] hits=%d misses=%d work=%d\n",
			run, results[0].Value, results[1].Value, results[2].Value,
			report.Step("double").Hits, report.Step("double").Misses, report.Step("double").WorkCalls)
	}
}
