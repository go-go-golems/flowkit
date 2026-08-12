---
Title: Flowkit Developer Guide
Slug: flowkit-developer-guide
Short: Build bounded, cache-aware, policy-controlled Go data flows with Flowkit.
Topics:
- flowkit
- execution
- caching
- pipelines
Commands: []
Flags: []
IsTopLevel: true
IsTemplate: false
ShowPerDefault: true
SectionType: GeneralTopic
---

Flowkit executes deterministic work over collections while preserving input order. Use the low-level `execution` package for bounded mapping, caches, budgets, and rate limiters; use the `flow` package when a unit of work also needs identity, retries, failure policy, metering, and typed pipeline composition.

## Choose the right layer

The two packages deliberately serve different adoption levels. An application can use an execution primitive without accepting Flow's policy model, while a provider-backed pipeline can compose the same primitives through typed steps.

| Need | API |
|---|---|
| Bounded ordered parallelism | `execution.Map` |
| Per-item durable memoization | `execution.MapCached` |
| Batched misses with per-item cache entries | `execution.MapCachedBatches` |
| Finite spend and rate admission | `execution.Budget`, `TokenBucket`, `Chain` |
| Cached work with retries and item policy | `flow.Step`, `flow.Run` |
| Typed streaming stages | `flow.Pipe2`, `Pipe3`, `Pipe4` |
| One provider call for many cache misses | `flow.Bulk` |
| Group response with missing-item repair | `flow.Batched` |

Flowkit is not a DAG scheduler. It does not persist control state or distribute work. Resume means replay: completed deterministic items become cache hits on the next invocation.

## Start with bounded execution

`execution.Map` starts at most `Workers` work functions concurrently and writes each value back to its original index. Completion can occur out of order without changing returned order.

```go
results, err := execution.Map(ctx, inputs, execution.MapOptions[Input]{
    Workers: 4,
}, func(ctx context.Context, input Input) (Output, error) {
    return transform(ctx, input)
})
```

The first error cancels pending work. A configured limiter runs before each work call, and a custom cost must always return a positive integer.

## Define deterministic cache identity

An `execution.Key` contains a step namespace, a semantic version, and a SHA-256 input digest. Include every input that can change the output—model, prompt, provider behavior version, and normalization policy—but exclude worker counts, retry limits, and budgets.

```go
key, err := execution.NewKey("summarize", "v2", struct {
    Model string `json:"model"`
    Text  string `json:"text"`
}{Model: model, Text: text})
```

`FileCache` validates the complete key, schema, value digest, strict JSON shape, and entry size. An absent file is a miss; an invalid existing file returns `execution.ErrCorruptCache`. Corruption fails closed instead of silently repeating expensive work.

## Build a typed step

A step combines what identifies work, how it executes, and what function performs it. An empty identity makes the step uncached.

```go
step := flow.Step[Request, Response]{
    Name: "summarize",
    Identity: flow.Identity[Request]{
        Kind: "summary", Version: "v2",
        Key: func(request Request) ([]byte, error) {
            return json.Marshal(request.CacheIdentity())
        },
    },
    Policy: flow.Policy{
        Workers: 4,
        Retry: flow.RetrySpec{Attempts: 3},
        OnError: flow.Quarantine,
    },
    Do: summarize,
}

results, report, err := flow.Run(ctx, step, requests, flow.Options{Store: cache})
```

`Results[i]` always corresponds to `requests[i]`. A cache hit skips admission and provider work. Duplicate cache keys execute once in process and populate every matching input position.

## Classify errors deliberately

A classifier maps each error to `Transient`, `DataError`, or `Fatal`. Flowkit's default classifier retries typed HTTP 408, 429, and 5xx errors; treats explicit data markers with `flow.AsDataError`; and fails closed on cancellation, exhausted budgets, and unknown errors.

Provider-specific string matching belongs in the calling application:

```go
retry := flow.RetrySpec{
    Attempts: 3,
    Class: flow.ClassifierFunc(func(err error) flow.ErrorClass {
        if errors.Is(err, ErrTemporaryProviderFailure) {
            return flow.Transient
        }
        return flow.Fatal
    }),
}
```

Every retry receives fresh admission. A retry cannot bypass a finite spend ceiling.

## Admit resources before work

A `flow.Resource` declares the worst-case ceiling and admitted budget for a named integer resource. A preflight can reject incomplete coverage, missing prices, or an estimated monetary cost over the configured maximum before item zero runs.

```go
step.Policy.Admission = []flow.Resource{{
    Name: "provider-calls", Ceiling: len(items) * attempts, Budget: budget,
}}
options := flow.Options{
    Preflight: &flow.Preflight{MaxEstimatedUSD: 10},
    Rates: map[string]execution.Limiter{"provider-calls": tokenBucket},
}
```

Call `options.Share()` when separate `Run` calls must draw from one budget. Same-name declarations share only when all fields agree; conflicting declarations fail before provider work.

## Compose streaming pipelines

`Pipe2` through `Pipe4` connect typed stages through channels. An item enters the next stage as soon as its current stage finishes, while the final collector restores input order. Quarantined and skipped items bypass later stages.

```go
pipeline := flow.Pipe2(parseStep, enrichStep)
results, report, err := flow.Run(ctx, pipeline, inputs, options)
```

Set `Barrier` only when a stage truly requires all upstream results. Barriers reduce streaming and increase retained memory.

## Use bulk and repair execution

`flow.Bulk` is appropriate when a provider accepts many inputs and returns exactly one ordered result for each. Flow loads hits first, batches unique misses, admits the number of missed items, and stores every returned item independently.

`flow.Batched` serves less structured group responses. Its `Split` function maps group-local positions to results; uncovered, unparseable, missing, or nonfatal failed members run through the repair step. Keep the repair identity byte-compatible with standalone execution so both paths share cache entries.

## Observe runs

`StepReport` records items, cache traffic, physical work calls, retries, quarantine/skip decisions, resource snapshots, and meters. `OnResult` observes successful hits and fresh values. A `Ledger` receives lifecycle events. Hook or ledger errors fail the run because these observers commonly publish required artifacts.

Use `AttemptMeter` when failed provider calls can still report billable usage. Use `Meter` only when successful fresh values carry usage. Cache hits are never metered as current-run spend.

## Preserve critical invariants

Changes to Flowkit must protect these contracts:

- Results remain aligned to input indexes.
- Cache keys and envelope bytes remain stable unless a cache epoch is deliberate.
- Hits consume no admission budget.
- Every fresh attempt, including retries, obtains admission.
- Multi-resource refusal rolls back earlier reservable admission.
- Successful expensive work commits even after sibling cancellation.
- Fatal errors stop a run regardless of item failure mode.
- Unknown classifier results and corrupt entries fail closed.

Run unit and race tests after touching caches, budgets, reports, in-flight deduplication, hooks, pipelines, or batching.

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| Work runs again unexpectedly | Identity omitted a semantic field or changed bytes | Inspect `Identity.Key`, version it deliberately, and add a golden key test |
| Cache entry reports corrupt | Schema, key, digest, strict JSON, or size validation failed | Preserve the file for diagnosis; do not silently recompute |
| Retry never occurs | Error is unknown/fatal or attempts are exhausted | Use typed status errors or an explicit application classifier |
| Budget is exhausted early | Every physical retry consumes admission | Size the budget for attempts, or reduce retry bounds |
| Shared calls have separate budgets | `Options.Share()` was not used | Create shared options once and reuse the returned value |
| Pipeline appears blocked | A stage is a barrier or downstream workers are saturated | Remove unnecessary barriers and inspect per-stage workers |
| Bulk call receives cached items | Identity/store is missing or keys differ | Configure a stable identity and shared store |

## See Also

- [`../README.md`](../README.md) — package overview and quick start.
- [`../examples/bounded-map`](../examples/bounded-map) — ordered bounded execution.
- [`../examples/cached-step`](../examples/cached-step) — cache hits, duplicate suppression, and reports.
- [`../examples/pipeline`](../examples/pipeline) — typed streaming composition.
- [`../flow/doc.go`](../flow/doc.go) — package scope and non-goals.
- [`../execution/doc.go`](../execution/doc.go) — low-level execution package contract.
