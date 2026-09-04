# Flowkit examples (`scripts/`)

This directory is **runnable, verified documentation** for the Flowkit public
API. Every example is a Go `Example` function with an `// Output:` block, so the
whole suite both teaches the library and is checked by:

```bash
go test ./scripts/ -v
```

Read each `Example*` function top-to-bottom as a copy-paste recipe.

## How to run

```bash
# Run every example and verify its output:
go test ./scripts/ -v

# Run one example:
go test ./scripts/ -run ExampleMapCached -v

# Race-check the examples (they exercise real concurrency):
GOWORK=off go test -race ./scripts/ -count=1
```

## What is here

| File | Covers |
|---|---|
| `execution_examples_test.go` | `execution.Map`, budget+rate `Chain`, `MapCached` (durable resume), `FileCache` fail-closed corruption |
| `step_examples_test.go` | `flow.Step` cached/uncached, duplicate-key suppression, retry with classification, `AsDataError` quarantine |
| `policy_examples_test.go` | `FailureMode` (fail-fast/skip), admission budgets, monetary `Preflight`, `Options.Share()`, rate-after-budget |
| `pipeline_examples_test.go` | `Pipe2`/`Pipe3` streaming composition, quarantine bypass |
| `bulk_batch_examples_test.go` | `flow.Bulk` (one call, per-item cache), `flow.Batched` (group call + per-item repair) |
| `observe_examples_test.go` | `Meter`/`AttemptMeter`, `Ledger`, `OnResult` streaming hook, custom `Store` swap |

## The one-minute mental model

Flowkit runs expensive, repeatable work over a list of inputs and:

1. **preserves input order** — `results[i]` always corresponds to `inputs[i]`;
2. **caches durably** — completed items become cache hits on the next run
   ("resume = replay");
3. **bounds concurrency, budget, and rate** — every fresh work call (retries
   included) pays admission; cache hits are free;
4. **classifies errors** — `Transient` retries, `DataError`/`Fatal` are handled
   per the step's `FailureMode`;
5. **reports everything** — per-step cache traffic, work calls, retries,
   quarantine/skip counts, resource spend, and meters.

It is **not** a workflow engine, DAG scheduler, or distributed coordinator.
There is no persisted control state — durability is memoization.

See the [intern onboarding guide](../docs/guides/01-flowkit-intern-onboarding-and-implementation-guide.md) for the full narrative.
