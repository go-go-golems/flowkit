# Flowkit

Flowkit is a Go library for running **expensive, repeatable work over a list
of inputs** — safely. If you have a batch of items and you need to call a
slow function on each one (an LLM, an embeddings API, a parser, a
transformer), Flowkit gives you the boring-but-critical production concerns
for free:

- **Bounded concurrency** — run N items at a time, not all at once.
- **Preserved input order** — `results[i]` always matches `inputs[i]`.
- **Durable caching** — completed items are saved to disk (or MySQL, or
  memory); re-running the same inputs is instant and free. "Resume" just
  means re-running and getting cache hits.
- **Budgets and rate limits** — cap total spend and calls/sec; retries pay
  admission too, so a flaky endpoint can't silently escape your ceiling.
- **Retries with error classification** — retry transient failures (HTTP
  429/5xx), treat malformed responses as data, and fail fast on fatal errors.
- **Per-item failure policy** — kill the run, quarantine the bad item as
  data, or drop it with a visible count.
- **Typed pipelines** — compose steps that stream item-by-item.
- **Reports** — cache hits/misses, work calls, retries, spend, and usage
  meters per step.

> **Stability:** Flowkit is a `v0.x` library. Its on-disk cache format is
> compatibility-sensitive (it preserves the pre-extraction
> `rag-ttc-execution-cache/v1` schema), but public APIs may evolve before
> `v1`.

## What Flowkit is *not*

Flowkit is **not** a workflow engine, a DAG scheduler, or a distributed
coordinator. It keeps **no persisted control state**. There is no database of
"which step is running." Durability means *memoization*: each completed item is
saved under a content-addressed key, so if a process dies you lose at most the
one item each worker was mid-flight on, and everything else replays from
cache.

## The two-layer mental model

Flowkit is split into two packages you can adopt independently:

```
                 your application / provider adapters
                          |
          +---------------+---------------+
          |                               |
   flow (typed steps)            execution (mechanics)
   - Step[I,O] + Run             - Map (bounded parallelism)
   - Identity + Policy            - FileCache / MySQLCache
   - Pipe2/3/4 pipelines          - Budget / TokenBucket / Chain
   - Bulk / Batched              - MapCached / MapCachedBatches
   - Retry + Classifier           - ResourcePlan preflight
   - Report / Meter / Ledger
          |                               |
          +---------------+---------------+
                          |
                 internal/* (digest, json, fs)
```

- **`execution`** is a toolbox of small, focused primitives. You can grab just
  `Map` for bounded parallelism, or just `FileCache` for memoization, without
  buying into any opinionated model.
- **`flow`** is an opinionated layer that combines those mechanics behind one
  typed `Step[I, O]`. A step bundles *what identifies the work* (its cache
  key), *how it runs* (workers, retry, admission, failure mode), and *what it
  does* (the function). Use this when a unit of work needs caching + retry +
  policy together.

## Install

```bash
go get github.com/go-go-golems/flowkit@latest
```

## 30-second tour

**Bounded parallelism** (`execution.Map`) — run a function over items, at most
`Workers` at a time, results in input order:

```go
results, err := execution.Map(ctx, []int{1, 2, 3, 4},
    execution.MapOptions[int]{Workers: 2},
    func(_ context.Context, v int) (int, error) { return v * v, nil })
// results = [1 4 9 16]
```

**A cached, retryable step** (`flow.Step` + `flow.Run`) — identity controls
reuse, policy controls execution:

```go
step := flow.Step[Request, Response]{
    Name: "summarize",
    Identity: flow.Identity[Request]{
        Kind: "summary", Version: "v2",
        Key: func(r Request) ([]byte, error) { return json.Marshal(r) },
    },
    Policy: flow.Policy{
        Workers: 4,
        Retry:   flow.RetrySpec{Attempts: 3},
        OnError: flow.Quarantine,
    },
    Do: summarize,
}
results, report, err := flow.Run(ctx, step, requests, flow.Options{Store: cache})
```

The first run computes and stores each result; the second run is all cache
hits. Duplicate inputs in one run execute once and fill every matching
position. A cache hit costs **zero** admission budget.

## Run the examples

This repository ships **runnable, verified examples** in [`scripts/`](./scripts)
— each is a Go `Example` function with a checked `// Output:` block:

```bash
go test ./scripts/ -v          # run and verify every example
go test ./scripts/ -run ExampleMapCached -v   # one example
```

They cover: bounded map, budget+rate chains, durable cache-and-resume,
fail-closed corruption, cached/uncached steps, duplicate suppression, retry +
classification, all three failure modes, admission budgets, the monetary
preflight, shared budgets, streaming pipelines, `Bulk`, `Batched` (group +
repair), metering, the event ledger, and a custom `Store` swap.

There is also one complete, runnable end-to-end program in
[`examples/full-run`](./examples/full-run) — it wires a cached, retryable,
budgeted step against an on-disk `FileCache` and runs it twice so you can watch
"resume = replay", retry, and quarantine happen:

```bash
go run ./examples/full-run
```

The relationship between the two example homes is deliberate:

- [`examples/`](./examples) — **one** complete, runnable `main()` program that
  ties the whole story together (`go run ./examples/full-run`).
- [`scripts/`](./scripts) — the exhaustive, **verified** per-API reference
  (`go test ./scripts/ -v`), with checked `// Output:` blocks.

## The one invariant that makes everything work

A step's **Identity** (cache key) is kept strictly separate from its
**Policy** (how it runs). `Workers`, `Retry`, `Admission`, and `Budget` never
affect the cache key — only `Kind`, `Version`, and the exact semantic input
bytes do. That discipline is what makes replays **byte-exact**: change the
model or prompt and you get a new key (correct); change the worker count and
you get cache hits (also correct).

## Choosing the right API

| You want… | Use |
|---|---|
| Bounded ordered parallelism | `execution.Map` |
| Per-item durable memoization | `execution.MapCached` |
| Batched misses, per-item cache entries | `execution.MapCachedBatches` |
| Finite spend / rate admission | `execution.Budget`, `TokenBucket`, `Chain` |
| Cached work + retries + item policy | `flow.Step` + `flow.Run` |
| Typed streaming stages | `flow.Pipe2` / `Pipe3` / `Pipe4` |
| One provider call for many cache misses | `flow.Bulk` |
| Group response with missing-item repair | `flow.Batched` |
| Swap the durable storage | implement `flow.Store` (2 methods) |

## Live progress

Configure a `flow.Reporter` to receive deep-cloned initial, periodic, and terminal snapshots without parsing logs. Exact item and lifecycle events remain available through `flow.Ledger`; configured sink failures fail closed.

```go
options := flow.Options{
    Reporter: flow.ReporterFunc(writeSnapshot),
    ReportInterval: 2 * time.Second,
}
results, report, err := flow.Run(ctx, step, inputs, options)
```

Run the complete example with `go run ./examples/progress-reporter`.

## Documentation

- **[Intern onboarding & implementation guide](./docs/guides/01-flowkit-intern-onboarding-and-implementation-guide.md)** — start here if you're new. A full narrative tour with diagrams, pseudocode, API references, and file references.
- **[Developer guide](./docs/flowkit-developer-guide.md)** — reference-style concepts, API guidance, invariants, and a troubleshooting table (Glazed help entry, also available via `flowkit help`).
- **[Runnable examples](./scripts/)** — verified, copy-paste recipes for every public API.
- **[Extraction design & implementation guide](./ttmp/2026/08/12/FLOWKIT-001--extract-flow-and-execution-from-ragkit/design-doc/01-flowkit-extraction-architecture-and-implementation-guide.md)** — how Flowkit was extracted from ragkit and the compatibility contract.
- [Progress reporter example](./examples/progress-reporter)

## Compatibility contract

The durable cache retains the pre-extraction `rag-ttc-execution-cache/v1`
schema so moving module paths does not invalidate expensive work. Existing
entries validate their full key, value digest, strict JSON shape, and size.
**Corrupt existing entries fail closed** (return `ErrCorruptCache`) rather
than recomputing silently.

Flowkit must not import ragkit. A repository boundary test enforces this
direction:

```text
ragkit adapters -> flowkit/flow -> flowkit/execution -> flowkit/internal/*
```

## Development

```bash
gofmt -w ./execution ./flow ./internal ./examples ./scripts
GOWORK=off go test ./... -count=1
GOWORK=off go test -race ./... -count=1
GOWORK=off go vet ./...
make ci-check
```

When changing cache identity or persistence, update compatibility fixtures
deliberately. When changing concurrent execution, run race tests and verify
order, admission rollback, duplicate suppression, and commit-after-cancellation
behavior.

## License

See [LICENSE](./LICENSE).
