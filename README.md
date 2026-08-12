# Flowkit

Flowkit is a domain-neutral Go library for bounded, recoverable, policy-controlled work over collections. It was extracted from ragkit so caching, budgets, retries, batching, and typed pipelines can be reused without importing RAG or provider types.

> **Stability:** Flowkit is a `v0.x` library. Its cache format is compatibility-sensitive, but public APIs may evolve before `v1`.

## Packages

- [`execution`](./execution) provides ordered bounded map, content-addressed caches, cache-aware map and batch map, finite budgets, token-bucket rates, transactional limiter chains, and cost preflight.
- [`flow`](./flow) provides typed steps and pipelines, retries and error classification, per-item failure policy, cache-aware bulk calls, batching with repair, metering, reports, ledgers, and shared run resources.

Flowkit is not a workflow server or DAG scheduler. It keeps no persisted control state. Resume means replaying inputs against durable per-item cache entries.

## Install

```bash
go get github.com/go-go-golems/flowkit@latest
```

## Quick start

```go
package main

import (
    "context"
    "fmt"

    "github.com/go-go-golems/flowkit/execution"
)

func main() {
    values, err := execution.Map(
        context.Background(),
        []int{1, 2, 3},
        execution.MapOptions[int]{Workers: 2},
        func(_ context.Context, value int) (int, error) { return value * value, nil },
    )
    if err != nil {
        panic(err)
    }
    fmt.Println(values) // [1 4 9]
}
```

Run it from this repository:

```bash
go run ./examples/bounded-map
```

## Cached typed work

A `flow.Step[I,O]` keeps semantic identity separate from execution policy. Identity controls reuse; workers, retries, rates, and budgets do not alter the key.

```go
step := flow.Step[Input, Output]{
    Name: "transform",
    Identity: flow.Identity[Input]{
        Kind: "transform", Version: "v1",
        Key: func(input Input) ([]byte, error) { return json.Marshal(input) },
    },
    Policy: flow.Policy{Workers: 4},
    Do: transform,
}
results, report, err := flow.Run(ctx, step, inputs, flow.Options{Store: cache})
```

Flow preserves input order, executes duplicate keys once per process, loads hits before admission, and atomically commits successful misses.

## Documentation

- [Developer guide](./docs/flowkit-developer-guide.md) — Glazed help-entry formatted concepts, API guidance, invariants, and troubleshooting.
- [Bounded map example](./examples/bounded-map)
- [Cached step example](./examples/cached-step)
- [Pipeline example](./examples/pipeline)
- [Extraction design and implementation guide](./ttmp/2026/08/12/FLOWKIT-001--extract-flow-and-execution-from-ragkit/design-doc/01-flowkit-extraction-architecture-and-implementation-guide.md)

## Compatibility contract

The durable cache retains the pre-extraction `rag-ttc-execution-cache/v1` schema so moving module paths does not invalidate expensive work. Existing entries validate their full key, value digest, strict JSON shape, and size. Corrupt existing entries fail closed rather than recomputing silently.

Flowkit must not import ragkit. A repository boundary test enforces this direction:

```text
ragkit adapters -> flowkit/flow -> flowkit/execution -> flowkit/internal/*
```

## Development

```bash
gofmt -w ./execution ./flow ./internal ./examples
GOWORK=off go test ./... -count=1
GOWORK=off go test -race ./... -count=1
GOWORK=off go vet ./...
make ci-check
```

When changing cache identity or persistence, update compatibility fixtures deliberately. When changing concurrent execution, run race tests and verify order, admission rollback, duplicate suppression, and commit-after-cancellation behavior.

## License

See [LICENSE](./LICENSE).
