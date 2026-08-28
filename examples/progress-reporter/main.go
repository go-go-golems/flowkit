package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/go-go-golems/flowkit/flow"
)

func main() {
	step := flow.Step[int, int]{
		Name:   "double",
		Policy: flow.Policy{Workers: 2},
		Do: func(ctx context.Context, value int) (int, error) {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(15 * time.Millisecond):
				return value * 2, nil
			}
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	reporter := flow.ReporterFunc(func(_ context.Context, snapshot flow.Snapshot) error {
		return encoder.Encode(snapshot)
	})
	results, _, err := flow.Run(context.Background(), step, []int{1, 2, 3}, flow.Options{
		Reporter:       reporter,
		ReportInterval: 10 * time.Millisecond,
	})
	if err != nil {
		panic(err)
	}
	for _, result := range results {
		fmt.Println(result.Value)
	}
}
