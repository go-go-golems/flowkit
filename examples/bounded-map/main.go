package main

import (
	"context"
	"fmt"

	"github.com/go-go-golems/flowkit/execution"
)

func main() {
	results, err := execution.Map(context.Background(), []int{1, 2, 3, 4}, execution.MapOptions[int]{Workers: 2}, func(_ context.Context, value int) (int, error) {
		return value * value, nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(results)
}
