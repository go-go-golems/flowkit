package examples_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-go-golems/flowkit/execution"
)

// ExampleMap shows the simplest building block: run a function over every item
// with bounded concurrency, preserving input order. The first error cancels
// pending work.
func ExampleMap() {
	ctx := context.Background()
	results, err := execution.Map(ctx, []int{1, 2, 3, 4}, execution.MapOptions[int]{
		Workers: 2,
	}, func(_ context.Context, v int) (int, error) {
		return v * v, nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(results)
	// Output:
	// [1 4 9 16]
}

// ExampleMap_withLimiter shows a finite Budget chained with a TokenBucket rate.
// Chain puts the Budget first so unaffordable work is rejected before waiting
// for a rate token. Cost lets each item consume a different number of units.
func ExampleMap_withLimiter() {
	ctx := context.Background()

	budget, err := execution.NewBudget(6) // total 6 units
	if err != nil {
		panic(err)
	}
	rate, err := execution.NewTokenBucket(execution.Rate{
		Units: 100, Per: time.Second, Burst: 3, // 100/s, burst 3
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = rate.Close() }()

	// Costs: items 1,2,3 consume 1,2,3 units (sum 6, exactly the budget).
	results, err := execution.Map(ctx, []int{1, 2, 3}, execution.MapOptions[int]{
		Workers: 3,
		Limiter: execution.Chain(budget, rate),
		Cost:    func(v int) int { return v },
	}, func(_ context.Context, v int) (int, error) {
		return v * v, nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(results)
	fmt.Printf("spent=%d remaining=%d\n", budget.Spent(), budget.Remaining())
	// Output:
	// [1 4 9]
	// spent=6 remaining=0
}

// ExampleMapCached shows durable, content-addressed memoization: load hits
// before admitting misses, compute misses, and store each result immediately.
// Re-running the same inputs against the same cache serves everything from
// disk — "resume = replay".
func ExampleMapCached() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "flowkit-example-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	cache, err := execution.NewFileCache(execution.FileCacheOptions{Directory: dir})
	if err != nil {
		panic(err)
	}

	keyFor := func(v int) (execution.Key, error) {
		return execution.NewKey("square", "v1", v)
	}

	// First run: all misses, all stored.
	_, report1, err := execution.MapCached(ctx, []int{1, 2, 2, 3},
		execution.CachedMapOptions[int]{
			Map:   execution.MapOptions[int]{Workers: 2},
			Cache: cache,
			Key:   keyFor,
		},
		func(_ context.Context, v int) (int, error) { return v * v, nil },
	)
	if err != nil {
		panic(err)
	}

	// Second run: everything is a hit (the duplicate key 2 ran once and was
	// stored, so all four positions replay from disk).
	_, report2, err := execution.MapCached(ctx, []int{1, 2, 2, 3},
		execution.CachedMapOptions[int]{
			Map:   execution.MapOptions[int]{Workers: 2},
			Cache: cache,
			Key:   keyFor,
		},
		func(_ context.Context, v int) (int, error) { return v * v, nil },
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("run1: hits=%d misses=%d writes=%d work=%d\n",
		report1.Hits, report1.Misses, report1.Writes, report1.WorkCalls)
	fmt.Printf("run2: hits=%d misses=%d writes=%d work=%d\n",
		report2.Hits, report2.Misses, report2.Writes, report2.WorkCalls)
	// Output:
	// run1: hits=0 misses=4 writes=3 work=3
	// run2: hits=4 misses=0 writes=0 work=0
}

// ExampleMapCached_resume simulates a crash mid-run: the first run is canceled
// after some items store, and the second run picks up exactly where the durable
// cache left off. Completed items become hits; only the unfinished one reruns.
func ExampleMapCached_resume() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "flowkit-resume-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	cache, err := execution.NewFileCache(execution.FileCacheOptions{Directory: dir})
	if err != nil {
		panic(err)
	}

	keyFor := func(v int) (execution.Key, error) {
		return execution.NewKey("resume-square", "v1", v)
	}
	work := func(_ context.Context, v int) (int, error) { return v * v, nil }

	// Run 1 over a few items; all store successfully.
	_, _, err = execution.MapCached(ctx, []int{1, 2, 3}, execution.CachedMapOptions[int]{
		Map: execution.MapOptions[int]{Workers: 1}, Cache: cache, Key: keyFor,
	}, work)
	if err != nil {
		panic(err)
	}

	// "Crash": the process is gone. A new process re-runs the SAME inputs plus a
	// new one. The three completed items are hits; only item 4 is computed.
	_, report, err := execution.MapCached(ctx, []int{1, 2, 3, 4}, execution.CachedMapOptions[int]{
		Map: execution.MapOptions[int]{Workers: 1}, Cache: cache, Key: keyFor,
	}, work)
	if err != nil {
		panic(err)
	}
	fmt.Printf("resume: hits=%d misses=%d work=%d\n", report.Hits, report.Misses, report.WorkCalls)
	// Output:
	// resume: hits=3 misses=1 work=1
}

// ExampleFileCache_corruptionFailsClosed shows that a tampered or invalid
// existing entry is NOT silently recomputed: Load returns ErrCorruptCache so
// corruption is visible rather than a quiet, expensive redo.
func ExampleFileCache_corruptionFailsClosed() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "flowkit-corrupt-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	cache, err := execution.NewFileCache(execution.FileCacheOptions{Directory: dir})
	if err != nil {
		panic(err)
	}
	key, err := execution.NewKey("tamper", "v1", 1)
	if err != nil {
		panic(err)
	}
	if err := cache.Store(ctx, key, 42); err != nil {
		panic(err)
	}

	// Corrupt the on-disk envelope by overwriting the value bytes.
	digest, err := key.Digest()
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, digest[:2], digest+".json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		panic(err)
	}

	_, err = cache.Load(ctx, key, new(int))
	fmt.Println(errors.Is(err, execution.ErrCorruptCache))
	// Output:
	// true
}
