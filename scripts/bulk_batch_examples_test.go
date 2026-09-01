package examples_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/go-go-golems/flowkit/flow"
)

// ExampleBulk shows one provider call for many cache misses while every item's
// result stays individually durable (the embeddings shape: one request carries
// a batch of inputs, one result comes back per input, each caches under its own
// key). Cache hits never enter a bulk call; unique misses are grouped into
// batches of at most batchSize.
func ExampleBulk() {
	store := flow.NewMemoryStore()
	var calls atomic.Int64

	base := flow.Step[int, int]{
		Name: "embed",
		Identity: flow.Identity[int]{
			Kind:    "ex-embed",
			Version: "v1",
			Key:     func(v int) ([]byte, error) { return []byte(strconv.Itoa(v)), nil },
		},
		Policy: flow.Policy{Workers: 2},
	}
	doBulk := func(_ context.Context, batch []int) ([]int, error) {
		calls.Add(1)
		out := make([]int, len(batch))
		for i, v := range batch {
			out[i] = v * 2
		}
		return out, nil
	}
	step := flow.Bulk(base, doBulk, 3) // batches of at most 3

	items := []int{1, 2, 3, 4, 5, 6, 7}
	results, report, err := flow.Run(context.Background(), step, items, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	// 7 unique misses in ceil(7/3)=3 batches -> 3 bulk calls.
	fmt.Printf("first: calls=%d misses=%d stored=%d\n",
		calls.Load(), report.Step("embed").Misses, report.Step("embed").Stored)
	for i, r := range results {
		fmt.Printf("  item %d -> %d\n", items[i], r.Value)
	}

	// Replay: every item is a hit; no bulk call is made.
	calls.Store(0)
	_, report, err = flow.Run(context.Background(), step, items, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	fmt.Printf("replay: calls=%d hits=%d\n", calls.Load(), report.Step("embed").Hits)
	// Output:
	// first: calls=3 misses=7 stored=7
	//   item 1 -> 2
	//   item 2 -> 4
	//   item 3 -> 6
	//   item 4 -> 8
	//   item 5 -> 10
	//   item 6 -> 12
	//   item 7 -> 14
	// replay: calls=0 hits=7
}

// ExampleBatched shows group calls with per-item repair. Many items share one
// provider call; whatever the group response misses (missing, unparseable, or
// failed members) is repaired one item at a time through the repair step. The
// repair step's Identity must byte-match the standalone per-item step so both
// paths share cache entries.
func ExampleBatched() {
	store := flow.NewMemoryStore()
	var repairs atomic.Int64

	// The repair step: runs one item at a time for anything the group call
	// did not yield. Its Identity is the cache namespace shared with a
	// standalone per-item run.
	repair := flow.Step[string, string]{
		Name: "chunk",
		Identity: flow.Identity[string]{
			Kind:    "ex-chunk",
			Version: "v1",
			Key:     func(s string) ([]byte, error) { return []byte(s), nil },
		},
		Policy: flow.Policy{Workers: 2},
		Do: func(_ context.Context, s string) (string, error) {
			repairs.Add(1)
			return "repaired:" + s, nil
		},
	}

	// Group: split indexes into fixed-size groups.
	group := func(items []string) [][]int {
		groups := [][]int{}
		for start := 0; start < len(items); start += 2 {
			end := start + 2
			if end > len(items) {
				end = len(items)
			}
			g := make([]int, 0, end-start)
			for i := start; i < end; i++ {
				g = append(g, i)
			}
			groups = append(groups, g)
		}
		return groups
	}

	// DoAll: one provider call per group returns a JSON map of position->result.
	// We deliberately OMIT position 1 of the first group to force a repair.
	doAll := func(_ context.Context, g []string) (string, error) {
		obj := map[string]string{}
		for i := range g {
			if g[i] == "b" { // simulate a missing member in the first group only
				continue
			}
			obj[strconv.Itoa(i)] = "group:" + g[i]
		}
		raw, _ := json.Marshal(obj)
		return string(raw), nil
	}

	// Split: parse the raw group response into position->result. A Split error
	// (wrapped with AsDataError) sends the whole group to repair.
	split := func(raw string, _ []string) (map[int]string, error) {
		parsed := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, flow.AsDataError(err)
		}
		out := map[int]string{}
		for k, v := range parsed {
			pos, err := strconv.Atoi(k)
			if err != nil {
				continue
			}
			out[pos] = v
		}
		return out, nil
	}

	step := flow.Batched(repair, flow.BatchSpec[string, string]{
		Name:   "chunk-groups",
		Policy: flow.Policy{Workers: 1},
		Group:  group,
		DoAll:  doAll,
		Split:  split,
	})

	results, _, err := flow.Run(context.Background(), step,
		[]string{"a", "b", "c", "d"}, flow.Options{Store: store})
	if err != nil {
		panic(err)
	}
	// Group ["a","b"]: position 0 -> "group:a", position 1 missing -> repaired:"b".
	// Group ["c","d"]: both present -> "group:c", "group:d".
	fmt.Println(results[0].Value)
	fmt.Println(results[1].Value)
	fmt.Println(results[2].Value)
	fmt.Println(results[3].Value)
	fmt.Printf("repairs=%d\n", repairs.Load())
	// Output:
	// group:a
	// repaired:b
	// group:c
	// group:d
	// repairs=1
}
