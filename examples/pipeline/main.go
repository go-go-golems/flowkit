package main

import (
	"context"
	"fmt"

	"github.com/go-go-golems/flowkit/flow"
)

func main() {
	double := flow.Step[int, int]{Name: "double", Policy: flow.Policy{Workers: 2}, Do: func(_ context.Context, value int) (int, error) { return value * 2, nil }}
	format := flow.Step[int, string]{Name: "format", Policy: flow.Policy{Workers: 2}, Do: func(_ context.Context, value int) (string, error) { return fmt.Sprintf("value=%d", value), nil }}

	results, report, err := flow.Run(context.Background(), flow.Pipe2(double, format), []int{3, 1, 2}, flow.Options{})
	if err != nil {
		panic(err)
	}
	for _, result := range results {
		fmt.Println(result.Value)
	}
	fmt.Printf("double items=%d; format items=%d\n", report.Step("double").Items, report.Step("format").Items)
}
