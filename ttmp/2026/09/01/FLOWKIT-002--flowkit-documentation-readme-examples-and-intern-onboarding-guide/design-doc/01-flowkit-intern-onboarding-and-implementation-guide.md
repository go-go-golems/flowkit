---
Title: Flowkit intern onboarding and implementation guide
Ticket: FLOWKIT-002
Status: active
Topics:
    - documentation
    - go
    - onboarding
    - examples
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://README.md
      Note: Onboarding-first README rewritten for FLOWKIT-002
    - Path: repo://execution/cache.go
      Note: Key, FileCache envelope, fail-closed corruption
    - Path: repo://flow/run.go
      Note: Run engine, typedRunner process/lead/work/success/fail, inflight dedup, commit-after-cancel
    - Path: repo://flow/step.go
      Note: Step[I,O] and Identity; the identity-vs-policy discipline
    - Path: repo://scripts/execution_examples_test.go
      Note: Verified examples the design-doc references
ExternalSources: []
Summary: 'An intern-oriented, from-zero tour of Flowkit: the problem it solves, the two-layer architecture, every public API, the runtime model with diagrams and pseudocode, the invariants that make replays byte-exact, and how to extend it.'
LastUpdated: 2026-09-01T18:30:00-04:00
WhatFor: Understand what Flowkit is and how every part fits together, starting from no prior knowledge of the codebase.
WhenToUse: Read this on day one. Reference it before changing cache, admission, retry, pipeline, or batching behavior.
---






# Flowkit intern onboarding and implementation guide

## 1. Executive summary

Flowkit is a Go library for running **expensive, repeatable work over a list
of inputs** — safely. Imagine you have 50,000 documents and you need to call
an LLM to summarize each one. A naive `for` loop is wrong in production because
it ignores the things that will actually hurt you:

- calling the API 50,000 times at once will get you rate-limited or banned;
- if the process crashes at item 30,000, you lose everything and start over;
- a few malformed responses should not kill 50,000 items of work;
- you need to know how much money you spent;
- you need the results back in the same order you sent them.

Flowkit gives you all of that. It runs your function over your items with
**bounded concurrency**, **preserves input order**, **caches each result
durably** (to disk, MySQL, or memory) so a re-run is instant, **bounds spend
and rate**, **retries transient failures**, **classifies errors**, and
**reports** what happened.

The single most important idea: **durability is memoization, not a workflow
log.** Flowkit keeps no "which step am I on" database. It saves each
*completed item* under a content-addressed key. "Resume" means re-running the
same inputs and getting cache hits for everything already done. If a process
dies, you lose at most the one item each worker was mid-flight on.

Flowkit is explicitly **not** a workflow engine, a DAG scheduler, or a
distributed coordinator (see `flow/doc.go:1-17` and the non-goals in §3.2).

## 2. Reader orientation: the problem in one paragraph

You have a function `Do(item) -> result` that is slow or expensive (an API
call, a model invocation, a heavy transform). You have a slice of items. You
want to run `Do` on all of them, and you want: order preserved, concurrency
bounded, results cached so re-runs are cheap, money/time capped, transient
errors retried, bad items isolated, and a report of what happened. Flowkit is
that loop, hardened. Everything in the library exists to serve one of those
goals.

### 2.1 Glossary (read this once)

- **Item** — one input position in a run. `results[i]` always corresponds to
  `inputs[i]`.
- **Step** (`flow.Step[I,O]`) — a typed operation from `I` to `O` plus its
  identity, policy, and function (`flow/step.go:29`).
- **Identity** (`flow.Identity[I]`) — the *cache key* of a step: a `Kind`
  (namespace), a `Version` (semantics), and a `Key` function returning the
  exact bytes the result depends on (`flow/step.go:20`).
- **Policy** (`flow.Policy`) — *how* a step runs: workers, admission
  resources, retry, failure mode (`flow/policy.go:11`). Policy never affects
  the cache key.
- **Store** (`flow.Store`) — the durability seam: `Load` and `Store` under an
  `execution.Key` (`flow/store.go:20`). Canonical impl: `*execution.FileCache`.
- **Cache / Key** (`execution.Cache`, `execution.Key`) — content-addressed
  storage; `Key{Step, Version, InputDigest}` (`execution/cache.go:27`).
- **Limiter** (`execution.Limiter`) — admits integer resource units,
  possibly by finite budget or token-bucket rate (`execution/map.go:13`).
- **Reservation** — provisional admission that can be `Commit()`-ed or
  `Rollback()`-ed, enabling transactional multi-resource admission
  (`execution/map.go:19`, `execution/reservation.go`).
- **Run environment** (`runEnv`) — run-scoped ownership of resource
  declarations, budgets, rates, and the cost preflight (`flow/run.go:121`).
- **Ledger** (`flow.Ledger`) — an event sink; every run event (hit/stored/
  retry/quarantine/skip) is journaled. A ledger error fails the run
  (`flow/report.go:156`).
- **Report** (`flow.Report`) — per-step counters: items, cache traffic, work
  calls, retries, quarantine/skip, spend snapshots, meters (`flow/report.go:89`).
- **Quarantine** — turn an item error into a structured `ItemError` record and
  keep going (`flow/policy.go:55`).
- **Skip** — drop the item but keep a visible count and `Skipped` marker
  (`flow/policy.go:58`).
- **Barrier** — a pipeline stage that waits for *all* upstream items before
  processing its first (`flow/step.go:39`).
- **Repair** — in `Batched`, run an item individually when a group response
  does not yield a usable result (`flow/batch.go:44`).

## 3. Scope and architecture

### 3.1 The two-layer architecture

Flowkit is deliberately split into two packages you can adopt independently.
A dependency test (`boundary_test.go`) enforces the one-way boundary:

```text
your application / provider adapters
        |
        v
flowkit/flow  ----------+
        |               |
        v               |
flowkit/execution       |
        |               |
        v               v
flowkit/internal/* (digest, jsonutil, fsutil, testsupport)
```

- **`execution`** is a toolbox of small, focused, generic primitives
  (`execution/doc.go`). You can grab *just* `Map` for bounded parallelism, or
  *just* `FileCache` for memoization, without buying into any opinionated
  model. The functions are plain and composable.
- **`flow`** is an opinionated layer that combines those mechanics behind one
  typed `Step[I, O]` (`flow/doc.go`). A step bundles *what identifies the work*
  (its cache key), *how it runs* (workers, retry, admission, failure mode),
  and *what it does* (the function). Use this when a unit of work needs
  caching + retry + policy together.

`flow` publicly re-exports the execution types it needs
(`execution.Key`, `execution.CacheOutcome`, `execution.Limiter`,
`execution.BudgetSnapshot`, `execution.CostPreflight`) and internally delegates
admission and bounded mapping to `execution`. This is why the layers must move
together — `flow` is a typed shell over `execution`.

### 3.2 Non-goals (what Flowkit will not become)

From `flow/doc.go:1-17` and the FLOWKIT-001 decision record DR-1:

- **No DAG scheduler.** There is no graph of steps with conditional branching.
  `Pipe2`–`Pipe4` are linear, typed streaming compositions.
- **No persisted control state.** There is no "run record" database tracking
  which step is active. Durability = per-item memoization.
- **No distributed coordination.** Everything is in one process. Budgets,
  token buckets, and in-flight dedup maps are process-local.
- **No workflow semantics.** `flow` owns mechanics; anything resembling
  workflow belongs to the calling program.

### 3.3 Package map

| Package | Purpose | Key files |
|---|---|---|
| `execution` | bounded map, caches, budgets, rates, preflight | `map.go`, `cache.go`, `cached_map.go`, `cached_batch_map.go`, `budget.go`, `rate.go`, `chain.go`, `reservation.go`, `resource_plan.go`, `mysql_cache.go` |
| `flow` | typed steps, pipelines, retry, policy, reports | `step.go`, `policy.go`, `run.go`, `pipe.go`, `store.go`, `classify.go`, `report.go`, `bulk.go`, `batch.go` |
| `internal/digest` | stable SHA-256 hex | `digest.go` |
| `internal/jsonutil` | strict JSON decode, fence stripping | `jsonutil.go` |
| `internal/fsutil` | atomic writes, containment joins, dir sync | `fsutil.go` |
| `internal/testsupport/mysqltest` | disposable Testcontainers MySQL | `mysqltest.go` |
| `cmd/flowkit` | minimal Glazed-help CLI hosting the docs | `main.go` |
| `scripts` | runnable, verified examples (this ticket) | `*_examples_test.go` |
| `examples` | tiny runnable `main()` programs | `bounded-map`, `cached-step`, `pipeline` |

## 4. The `execution` layer (mechanics)

### 4.1 Bounded map: `execution.Map`

`execution.Map` (`execution/map.go:50`) runs a function over a slice with at
most `Workers` concurrent calls and writes each result back to its original
index. Completion can be out of order; returned order always matches input
order. The first error cancels pending work (via `errgroup.WithContext`) and is
annotated with its input index.

```go
func Map[T, R any](ctx context.Context, items []T,
    options MapOptions[T],
    work func(context.Context, T) (R, error)) ([]R, error)
```

Internal structure (`execution/map.go:60-110`):

```text
                ┌──────────────┐
   items ──────►│   feeder     │── jobs ──►┌────────┐ ┌────────┐ ┌────────┐
                │ (1 goroutine)│           │worker 0│ │worker 1│ │worker N│
                └──────────────┘           └───┬────┘ └───┬────┘ └───┬────┘
                                               │          │          │
                               results[index] = work(ctx, item)      │
                                               └──────────┴──────────┘
                                errgroup.Wait() → first error cancels all
```

- `Workers < 1` selects one worker; `Workers > len(items)` is clamped to
  `len(items)`.
- `Limiter` gates each item *before* its work call; `Cost` returns resource
  units (default 1, must be positive).
- A nil `work` is an error; empty items return an empty slice (not nil).

**Example** (see `scripts/execution_examples_test.go` `ExampleMap`):

```go
results, _ := execution.Map(ctx, []int{1,2,3,4},
    execution.MapOptions[int]{Workers: 2},
    func(_ context.Context, v int) (int, error) { return v*v, nil })
// [1 4 9 16]
```

### 4.2 Limiters, budgets, and rates

A `Limiter` (`execution/map.go:13`) gates work by integer units; a nil limiter
means no limit. Two built-ins implement `ReservableLimiter`
(`execution/map.go:27`), meaning their admission can be reserved and rolled
back — the foundation of transactional multi-resource admission.

**`Budget`** (`execution/budget.go`) — a concurrency-safe, *non-replenishing*
total. Successful admission consumes units permanently, *including when the
admitted work later fails*. This is deliberate: it models *attempted*
provider cost so retries cannot silently escape the ceiling.

```go
budget, _ := execution.NewBudget(6)       // total 6 units
budget.Wait(ctx, 4)                        // charges 4
budget.Remaining() == 2
budget.Snapshot() == BudgetSnapshot{Limit:6, Spent:4, Remaining:2}
```

`Reserve` (`execution/budget.go:46`) provisionally charges; `Commit()` makes
it permanent, `Rollback()` refunds it.

**`TokenBucket`** (`execution/rate.go`) — a token-bucket rate: `Units`
replenished evenly over `Per`, up to `Burst`. A background goroutine
replenishes tokens; `Close()` stops it and unblocks waiters. `Wait` acquires
units and refunds partial acquisitions on context cancellation
(`execution/rate.go:83`).

```go
rate, _ := execution.NewTokenBucket(execution.Rate{
    Units: 100, Per: time.Second, Burst: 3, // 100/s, burst 3
})
defer rate.Close()
```

**`Chain`** (`execution/chain.go:11`) — composes limiters in order. Put a
`Budget` *before* a `TokenBucket` so unaffordable work is rejected without
waiting for a rate token. A `Chain` is itself `ReservableLimiter`: it reserves
through every limiter and rolls back earlier reservations if a later limiter
refuses — the transactional admission that prevents "pay for resource A, then
resource B says no, and you lost A's units."

```go
limiter := execution.Chain(budget, rate)
```

### 4.3 Content-addressed cache: `Key` and `FileCache`

**`Key`** (`execution/cache.go:27`) identifies one deterministic item result:

```go
type Key struct {
    Step        string `json:"step"`         // operation namespace
    Version     string `json:"version"`      // semantic version
    InputDigest string `json:"input_digest"` // SHA-256 hex of input bytes
}
```

`NewKey(step, version, input)` (`execution/cache.go:36`) JSON-marshals `input`
and SHA-256-digests it. **Include every input that can affect the result**
(model, prompt, provider behavior version, normalization) and **exclude**
worker counts, retry limits, and budgets. `Key.Digest()` (`execution/cache.go:68`)
further hashes the whole key into a stable path fingerprint.

**`Cache`** interface (`execution/cache.go:81`): `Load(ctx, key, target) ->
(found bool, err error)` and `Store(ctx, key, value) -> error`. `found` is
false only when no entry exists; an *invalid existing* entry returns an error.

**`FileCache`** (`execution/cache.go:93`) stores one validated JSON envelope
per key at `<dir>/<digest[:2]>/<digest>.json`. The envelope
(`execution/cache.go:129`) is:

```json
{
  "schema_version": "rag-ttc-execution-cache/v1",
  "key":            { "step": "...", "version": "...", "input_digest": "..." },
  "value_digest":   "<sha256 of value bytes>",
  "value":          <raw result JSON>
}
```

`Load` (`execution/cache.go:138`) validates the schema, the full key, the
value digest, the strict JSON shape (`jsonutil.DecodeStrict`), and the entry
size. A corrupted entry returns `ErrCorruptCache` — **corruption fails closed**
rather than silently recomputing expensive work. `Store`
(`execution/cache.go:183`) publishes atomically: write temp → fsync → rename
→ sync parent directory (`fsutil.AtomicWrite`).

**`MySQLCache`** (`execution/mysql_cache.go`) is a drop-in replacement: the
`value_json` column holds the *exact same envelope* `FileCache` writes, so
`Load` performs the same validation and fails closed the same way. MySQL's
commit *is* the atomic publish, removing the temp→fsync→rename hazard. It
migrates its own schema under a `GET_LOCK`-guarded, versioned migration
(`execution/mysql_cache.go` `migrate`), and uses `ON DUPLICATE KEY UPDATE`
(idempotent because entries are content-addressed).

### 4.4 Cache-aware map: `MapCached`

`execution.MapCached` (`execution/cached_map.go:44`) layers caching on top of
bounded `Map`:

1. **Group** duplicate keys so one key executes once per process.
2. **Load** hits before admitting anything — cache hits are free.
3. **Run** the unique misses through `Map` (workers/limiter/cost).
4. **Store** each successful miss *immediately*, using `context.WithoutCancel`
   so a completed item commits even if a sibling just canceled the run.
5. Duplicate keys fan the single computed value out to every matching index.

It returns a `CacheReport` (`execution/cached_map.go:25`) with `Hits`,
`Misses`, `Writes`, `WorkCalls`, and per-item `Outcomes` (`hit`/`stored`/
`pending`).

```text
  items ──► group by key ──► for each unique key:
                                ├─ Load hit?  ──► fill all matching indexes (CacheHit)
                                └─ miss      ──► queue for Map
                              run misses through Map(workers, limiter)
                              for each result: Store(WithoutCancel(ctx)) → CacheStored
                              fan stored value to all matching indexes
```

`MapCachedBatches` (`execution/cached_batch_map.go`) is the same idea but
misses are executed in bounded *batches* — `work` receives a slice and returns
a slice, while each result still caches under its own key. Admission charges
the sum of item costs in a batch.

### 4.5 Resource plans and preflight

`execution.ResourcePlan` (`execution/resource_plan.go:11`) declares a named
integer resource's `Ceiling` (worst case), `Budget` (admitted allowance), and
optional `UnitUSD`. `ValidateResourcePlans` (`execution/resource_plan.go:26`)
validates identities, budgets, pricing, and an explicit monetary ceiling
*before any work*. `NewResourceBudgets` (`execution/resource_plan.go:89`)
builds one `Budget` + composed `Chain(budget, rate)` per resource.

## 5. The `flow` layer (typed orchestration)

### 5.1 `Step[I, O]`: identity + policy + work

A `Step` (`flow/step.go:29`) is a value with four parts:

```go
type Step[I, O any] struct {
    Name     string              // report/log name; convention prefix for resources
    Identity Identity[I]         // cache key (Kind=="") => uncached
    Policy   Policy              // how it runs
    Barrier  bool                // pipeline stage waits for all upstream
    Do       func(context.Context, I) (O, error)
    Meter       func(O) Meters           // meter fresh success
    AttemptMeter func(O, error) Meters   // meter every attempt (incl. errors)
    OnResult    func(context.Context, int, O, execution.CacheOutcome) error
    // unexported: override (Bulk/Batched), stages (Pipe), extraPlans/extraPolicies
}
```

**`Identity`** (`flow/step.go:20`):

```go
type Identity[I any] struct {
    Kind    string             // "" => uncached (pure compute)
    Version string
    Key     func(I) ([]byte, error) // exact semantic bytes
}
```

`Step.Key(item)` (`flow/step.go:80`) maps directly onto `execution.Key`:
`Kind`→`Step`, `Version`→`Version`, and the key bytes are digested
*unmodified*. So a step whose `Identity` mirrors an existing cache family
produces byte-identical keys and replays historical entries.

**`Policy`** (`flow/policy.go:11`):

```go
type Policy struct {
    Workers   int          // <1 => 1
    Admission []Resource   // fail-closed resource plans
    Retry     RetrySpec    // attempts + backoff + classifier
    OnError   FailureMode  // FailFast (default) | Quarantine | Skip
}
```

> **The discipline that makes replays byte-exact:** Identity and Policy are
> separate by construction. `Workers`, `Retry`, `Admission`, and `Budget`
> never enter the cache key. Change the model or prompt → new key (correct).
> Change the worker count → cache hits (also correct).

### 5.2 `Run`: the engine

`flow.Run[I,O]` (`flow/run.go:382`) executes a step (or composed pipeline) over
items:

```go
func Run[I, O any](ctx context.Context, s Step[I, O], items []I,
    o Options) ([]Result[O], Report, error)
```

It (1) flattens the step into `stageSpec`s, (2) validates every policy, (3)
ensures all admission plans are admitted by the shared `runEnv` preflight,
(4) if the step has an `override` (Bulk/Batched) runs that, else (5) streams
items through typed runners and restores order at the end.

**Per-item lifecycle** (`typedRunner.process`, `flow/run.go:882`):

```text
process(item):
  if uncached (Kind=="" or Store==nil): return work(item, key=nil)
  key = Step.Key(item)
  if key already in-flight: follow(leader)          # dedup
  else: lead(item, key)

lead(item, key):
  found = Store.Load(key)
  if found: count Hit; notify; return cached value   # free of admission
  count Miss
  return work(item, &key)

work(item, key):           # the retry loop
  for attempt in 1..Attempts:
    if attempt>1: sleep(backoff with jitter)
    admit(resources, 1 unit)            # EVERY attempt pays; hits don't
    value, err = Do(item)
    count WorkCall
    if AttemptMeter: meter(value, err)
    if err==nil: return success(value, key)
    class = Classifier.Classify(err)
    if class==Transient and attempt<Attempts: count Retry; continue
    break
  return fail(err, class, attempts)

success(value, key):
  if Meter: meter(value)
  if key!=nil: Store.Store(WithoutCancel(ctx), key, value)  # commits after cancel
  count Stored; notify; return value

fail(err, class, attempts):
  if class==Fatal or OnError==FailFast: return err  # kills the run
  if OnError==Skip: count Skipped; return {Skipped:true}
  record = ItemError{Step,Index,Class,Attempts,Message}
  count Quarantined; return {Quarantined:record}
```

Key invariants visible here:

- **Hits are free** — they never enter `work`, so they pay no admission.
- **Every attempt pays** — admission is charged per `Do` call, retries
  included (`flow/run.go` `work`).
- **Commit after cancellation** — successful work stores with
  `context.WithoutCancel(ctx)` so a sibling's cancellation doesn't lose a
  completed result (`flow/run.go` `success`, `execution/cached_map.go` store
  call).
- **In-flight dedup** — duplicate keys share one `inflightCall`; followers
  copy the leader's outcome so a key never runs twice in one process
  (`flow/run.go` `inflightCall`).

### 5.3 Options and the shared run environment

`flow.Options` (`flow/run.go:32`) carries run-scoped collaborators:

```go
type Options struct {
    Store     Store                       // nil => uncached run
    Ledger    Ledger                      // optional event journal
    Preflight *Preflight                  // optional monetary gate
    Rates     map[string]execution.Limiter // rate per resource name
    env       *runEnv                     // unexported shared state
}
```

`Options.Share()` (`flow/run.go:54`) returns Options whose budgets, preflight
arithmetic, and admission state are shared across every `Run` made with the
returned value — two steps declaring the same resource draw from *one*
budget. Idempotent; you must reuse the returned value, not the original.

`runEnv.ensure` (`flow/run.go:149`) validates and registers plans with
fail-closed arithmetic *before any item runs*: it rejects duplicate
declarations with differing fields, refuses budgets that can't cover the
stated ceiling (unless `AllowPartial`), refuses unpriced plans (unless
`AllowUnpriced`), and refuses an estimated cost over `MaxEstimatedUSD`. A
zero-valued `Resource{Name:...}` is a *reference* to an earlier full
declaration (draws from that budget); a mistyped reference fails loudly as a
zero plan.

`runEnv.admit` (`flow/run.go:308`) reserves every named resource as one
transaction; a refusal rolls back earlier reservable admission.

`Options.Declare`, `.Limiter(name)`, `.Snapshots()`, `.Cost()` expose the
shared state to non-step consumers (`flow/run.go:66-100`).

### 5.4 Error classification

`ErrorClass` (`flow/classify.go:12`): `Transient` (retry), `DataError`
(never retry, honor `FailureMode`), `Fatal` (always kill the run).

`DefaultClassifier` (`flow/classify.go:125`) is domain-neutral and layered:

1. `AsDataError` markers → `DataError` (`flow/classify.go:86`). A step's
   *parser* wraps a malformed-but-transported response with `AsDataError` —
   the provider can't produce `DataError`, only the parser can.
2. `context.Canceled` / `context.DeadlineExceeded` / `ErrBudgetExceeded` →
   `Fatal` (these are operator verdicts; retrying fights the operator).
3. Typed `StatusError` (`flow/classify.go:102`, `HTTPStatus() int`): 408/429/5xx
   → `Transient`, everything else → `Fatal`.
4. Unknown → `Fatal` (fail closed).

Provider-specific string matching belongs in *your* `Classifier`
(`flow.ClassifierFunc`), passed via `RetrySpec.Class`.

### 5.5 Failure modes

`FailureMode` (`flow/policy.go:46`):

| Mode | Behavior |
|---|---|
| `FailFast` (default, zero value) | First non-retryable item error cancels the whole run. |
| `Quarantine` | Bad item becomes an `ItemError` record; `Value` is zero; run succeeds. Quarantined items are **never stored** (they don't poison the cache). |
| `Skip` | Drop the item; `Skipped` marker + count in the report. Rare; discouraged. |

`Fatal` errors *always* kill the run, regardless of `FailureMode`
(`flow/run.go` `fail`). `DataError` and non-fatal classes honor `FailureMode`.

### 5.6 Pipelines: `Pipe2` / `Pipe3` / `Pipe4`

`Pipe2`/`Pipe3`/`Pipe4` (`flow/pipe.go:14-38`) compose typed steps into one
`Step` whose stages **stream per item**: item `i` enters stage 2 as soon as
stage 1 finishes it — no barrier unless a stage sets `Barrier`. Each stage
keeps its own `Policy` and its own `Report` entry. An item quarantined or
skipped by a stage **bypasses every later stage** and surfaces at its original
position.

```text
  feeder ──► [stage1 runner] ──(chan, buf 16)──► [stage2 runner] ──► collector
                                                              (restores order by item.index)
```

Pipelines beyond arity four should prefer *nesting* (`Pipe2(Pipe2(a,b), c)`)
— stage lists flatten, so nesting costs nothing at runtime (`flow/pipe.go`
doc comment).

### 5.7 `Bulk`: one call, per-item cache

`flow.Bulk` (`flow/bulk.go:24`) wraps a step whose provider call takes many
items at once while every item's result stays individually durable (the
embeddings shape: one request carries a batch, one result per item, each
caches under its own key). `doBulk` must return exactly one result per input,
in order. Cache hits never enter a bulk call; unique misses are grouped into
batches of at most `batchSize`. Admission charges one unit per *unique missed
item*; retry and classification follow the step's `Policy` around each bulk
call. A bulk-call failure honors the step's `FailureMode` for every item in
the batch.

### 5.8 `Batched`: group call + per-item repair

`flow.Batched` (`flow/batch.go:44`) serves less-structured group responses.
A `BatchSpec` (`flow/batch.go:12`) declares:

- `Group(items) -> [][]int` — partition into index groups (every index at
  most once; uncovered indexes go straight to repair).
- `DoAll(group) -> (raw string, error)` — one provider call per group.
- `Split(raw, group) -> (map[int]O, error)` — parse per-item results out of
  the raw response. Missing positions are repaired; a `Split` error
  (wrapped with `AsDataError`) sends the *whole group* to repair.

Whatever the group response misses (missing, unparseable, or nonfatally
failed members) is repaired one item at a time through the `repair` step.
**The repair step's `Identity` must byte-match the standalone per-item step**
so repairs and standalone runs share cache entries. Group-call failures that
do not kill the run route every item of the group to repair. The nested
repair run operates on a compact slice; indexes and ledger events are
remapped back to original positions (`flow/batch.go` `remappedLedger`).

### 5.9 Observation: reports, meters, ledger, `OnResult`

- **`Report`** (`flow/report.go:89`): one `StepReport` per step name — items,
  hits/misses/stored, work calls, retries (by class), quarantine/skip,
  `Spend` snapshots, `Meters`.
- **`Meters`** (`flow/report.go:14`): named usage counters (tokens, cost).
  Only fresh work is metered; cache hits are never metered as current spend.
  `AddChecked` rejects non-finite values. Set `Meter` (success) *or*
  `AttemptMeter` (every attempt, incl. errors), not both.
- **`Ledger`** (`flow/report.go:156`): receives `Event`s (`hit`, `stored`,
  `done`, `retry`, `quarantined`, `skipped`). A ledger error **fails the run**
  — journals are part of the record, not best-effort.
- **`OnResult`** (`flow/step.go`): observes every successful item (hits and
  fresh work, never quarantined/skipped) in completion order, with index and
  cache outcome. Streaming artifact writers hang here; an error fails the run.

### 5.10 `Store`: the durability seam

`flow.Store` (`flow/store.go:20`) is two methods:

```go
type Store interface {
    Load(ctx context.Context, key execution.Key, target any) (bool, error)
    Store(ctx context.Context, key execution.Key, value any) error
}
```

`*execution.FileCache` (`flow/store.go` `var _ Store = (*execution.FileCache)(nil)`),
`*execution.MySQLCache`, and `*flow.MemoryStore` (`flow/store.go:30`) all
satisfy it. Swapping the durable mechanism means implementing these two
methods — **steps, pipelines, and reports never change.** This is THE
extension point. `MemoryStore` proves the swap: the same steps run unchanged
against it, they just stop surviving the process.

## 6. Worked example: a cached, retryable, budgeted step

This is the full shape an application adapter assembles (see
`scripts/step_examples_test.go` and `scripts/policy_examples_test.go` for
runnable versions):

```go
unitUSD := 0.002
step := flow.Step[Request, Response]{
    Name: "summarize",
    Identity: flow.Identity[Request]{
        Kind:    "summary",
        Version: "v2",
        Key:     func(r Request) ([]byte, error) { return json.Marshal(r.CacheIdentity()) },
    },
    Policy: flow.Policy{
        Workers: 4,
        Admission: []flow.Resource{{
            Name: "summarize-calls", Ceiling: len(requests) * 3, Budget: 500,
            UnitUSD: &unitUSD,
        }},
        Retry:  flow.RetrySpec{Attempts: 3, Backoff: flow.Backoff{Base: 2 * time.Second, Cap: time.Minute}},
        OnError: flow.Quarantine,
    },
    Do:    summarize,
    Meter: func(r Response) flow.Meters { return flow.Meters{"tokens": float64(r.Tokens)} },
}

opts := flow.Options{
    Store:     cache,
    Preflight: &flow.Preflight{MaxEstimatedUSD: 10, AllowUnpriced: false},
    Rates:     map[string]execution.Limiter{"summarize-calls": tokenBucket},
}.Share()

results, report, err := flow.Run(ctx, step, requests, opts)
// results[i] <-> requests[i]; report.Step("summarize") has hits/misses/spend/meters.
```

What happens, in order: preflight validates the plan and the estimated cost
*before item one*; each item loads (hits are free); misses pay admission and
retry with backoff; each success is stored atomically (commits even if a
sibling cancels); bad items become `Quarantined` records; the report counts
everything.

## 7. Critical invariants (do not break these)

From the source and the FLOWKIT-001 contract:

1. **Results stay aligned to input indexes** — `results[i]` ↔ `items[i]`,
   always; evaluation joins depend on it.
2. **Cache keys and envelope bytes stay stable** unless a cache epoch is
   deliberate (schema `rag-ttc-execution-cache/v1`).
3. **Hits consume no admission budget.**
4. **Every fresh attempt, including retries, obtains admission.**
5. **Multi-resource refusal rolls back earlier reservable admission**
   (`execution/chain.go` `Reserve`, `flow/run.go:308` `admit`).
6. **Successful expensive work commits even after sibling cancellation**
   (`context.WithoutCancel` in `success`/`MapCached`/`Bulk` store calls).
7. **Fatal errors stop a run regardless of item failure mode**
   (`flow/run.go` `fail`).
8. **Unknown classifier results and corrupt entries fail closed**
   (`flow/classify.go:155`, `execution/cache.go:170`).
9. **Quarantined items are never stored** — they don't poison the cache
   (`flow/run_test.go` `TestRunQuarantineNeverStoresBadItems`).

## 8. Decision records

### Decision: durability = memoization, not a workflow log (DR-1)

- **Context:** How should "resume" work, and what persistent state does
  Flowkit own?
- **Options considered:** (a) a persisted run/state machine recording step
  progress; (b) per-item content-addressed memoization with no control state.
- **Decision:** (b) — per-item memoization only.
- **Rationale:** A state machine couples Flowkit to a database, adds
  distributed-coordination concerns, and still loses in-flight work on crash.
  Memoization gives byte-exact replay, needs no control state, and loses at
  most one in-flight item per worker on crash. It matches the ragkit proven
  behavior.
- **Consequences:** Enables a tiny, dependency-free library; makes "resume"
  trivial (re-run); requires the identity/policy separation discipline. No
  DAG scheduler or distributed coordination will be added.
- **Status:** accepted (carried from FLOWKIT-001).

### Decision: two layers, `execution` + `flow`

- **Context:** Should the library be one opinionated API or composable
  primitives?
- **Options considered:** (a) one unified `Step` API; (b) a low-level toolbox
  plus an opinionated typed layer.
- **Decision:** (b) — `execution` (mechanics) + `flow` (typed `Step`).
- **Rationale:** Some callers want only bounded parallelism or only a cache
  without the policy model; forcing the full `Step` would block adoption.
  `flow` re-exports the execution types it needs, so the layers move together.
- **Consequences:** Two APIs to document; a one-way boundary test
  (`boundary_test.go`) must prevent `execution` from depending on `flow`.
- **Status:** accepted.

### Decision: identity is separate from policy (DR-3)

- **Context:** What goes in the cache key?
- **Options considered:** (a) key includes workers/retry/budget; (b) key
  includes only `Kind`, `Version`, and semantic input bytes.
- **Decision:** (b).
- **Rationale:** Replays must be byte-exact. If `Workers` changed the key,
  changing concurrency would invalidate expensive work. Semantic identity only.
- **Consequences:** `Step.Key` mirrors `execution.NewKey` directly so existing
  cache families replay. Callers must be disciplined: bump `Version` whenever
  semantics change. Validated by golden key tests in ragkit.
- **Status:** accepted.

### Decision: corruption fails closed

- **Context:** What if an on-disk cache entry is invalid?
- **Options considered:** (a) silently recompute; (b) return `ErrCorruptCache`.
- **Decision:** (b) — fail closed.
- **Rationale:** Silent recompute hides disk/encoding bugs and can silently
  re-burn money. A visible corruption error preserves the entry for diagnosis.
- **Consequences:** Callers must handle `ErrCorruptCache`; the same validation
  is duplicated in `FileCache` and `MySQLCache` via the shared envelope.
- **Status:** accepted.

### Decision: "bad item" is a result, not an error (DR-4)

- **Context:** How to handle per-item failures in a large run?
- **Options considered:** (a) always fail the run; (b) quarantine as data;
  (c) silent drop.
- **Decision:** `Quarantine` turns a bad item into an `ItemError` record; the
  run continues and succeeds. `Skip` is a rare, discouraged variant.
- **Rationale:** In evaluation campaigns, "this item failed" is analysis data,
  not a run-killing error. Quarantined items are never stored (no cache poison).
- **Consequences:** Adapters choose `FailureMode` deliberately; `Fatal` still
  kills the run regardless.
- **Status:** accepted.

## 9. Phased onboarding plan for a new intern

1. **Run the examples.** `go test ./scripts/ -v` and read each `Example*`
   top-to-bottom. Then `go run ./examples/bounded-map` and friends.
2. **Read `execution/map.go`.** Understand the feeder+workers+ordered-results
   shape and that the first error cancels. This is the atom everything builds
   on.
3. **Read `execution/cache.go` + `internal/fsutil/fsutil.go`.** Understand the
   `Key`, the envelope, and atomic publication. Write a 10-line program that
   stores and reloads a value with `FileCache`.
4. **Read `execution/budget.go`, `rate.go`, `chain.go`, `reservation.go`.**
   Trace one `Chain(budget, rate).Reserve` that rolls back. This is the
   transactional admission model.
5. **Read `flow/step.go` + `flow/policy.go`.** Note the identity/policy
   separation. Write a cached step (copy `ExampleStep_cached`), change
   `Workers`, and confirm the cache still hits.
6. **Read `flow/run.go` end-to-end.** Walk `Run` → `runStages` →
   `typedRunner.process` → `lead` → `work` → `success`/`fail`. Find the
   `context.WithoutCancel` store call and the `inflightCall` dedup.
7. **Read `flow/classify.go`.** Understand the three classes and why
   `AsDataError` is set by the *parser*, not the provider.
8. **Read `flow/pipe.go` + `runStages`.** See how items stream through
   buffered channels and how the collector restores order.
9. **Read `flow/bulk.go` then `flow/batch.go`.** Understand the difference:
   `Bulk` = one call, per-item cache; `Batched` = group call + per-item
   repair sharing the repair step's identity.
10. **Read `flow/store.go` + `execution/mysql_cache.go`.** Implement a trivial
    custom `Store` (see `ExampleStore_swap`) to feel the seam.
11. **Read the boundary test and the compatibility contract** in the README.
    You now understand the whole library.

## 10. Test and validation strategy

- **Unit tests:** `execution/*_test.go` and `flow/*_test.go` cover every
  invariant above (order, dedup, rollback, commit-after-cancel, quarantine,
  preflight refusal, metering, ledger). Run with `GOWORK=off go test ./...
  -count=1`.
- **Race tests:** concurrency paths are exercised under
  `GOWORK=off go test -race ./... -count=1`. Always run these after touching
  caches, budgets, dedup, hooks, pipelines, or batching.
- **Runnable examples:** `go test ./scripts/ -v` verifies 24 documented
  behaviors with checked `// Output:` blocks.
- **MySQL integration:** `internal/testsupport/mysqltest` spins up a
  disposable Testcontainers MySQL 8.4 container; tests self-skip without
  Docker. `make ci-check` runs fmt-check, lint, logcopter-check, test, build.
- **Cache compatibility:** `execution/cache_compat_test.go` and
  `execution/testdata/pre-extraction-cache/` fixtures guard the durable
  schema. When changing cache identity or persistence, update fixtures
  deliberately.

## 11. Risks, alternatives, and open questions

- **MySQL test hermeticity:** the `MySQLCache` path has no hermetic runnable
  example in `scripts/` (it needs Docker). A `testing.Short()`-guarded
  example would close the gap without forcing Docker on every contributor.
- **`Skip` mode is discouraged** and rarely used; document it but steer new
  code to `Quarantine`.
- **Pipeline arity:** `Pipe2`–`Pipe4` exist; deeper pipelines must nest.
  A variadic `Pipe` could reduce boilerplate but add generic complexity; the
  current stance is "nest, it's free."
- **Merging `flow.Resource` with `execution.ResourcePlan`:** explicitly
  deferred to a follow-up after the behavior-preserving extraction
  (FLOWKIT-001). Not in scope here.

## 12. References

### Key files (with line anchors)

- `execution/map.go:50` — `Map`; `:13` `Limiter`; `:19` `Reservation`; `:27`
  `ReservableLimiter`; `:33` `MapOptions`.
- `execution/cache.go:18` schema constant; `:27` `Key`; `:36` `NewKey`;
  `:68` `Key.Digest`; `:81` `Cache`; `:93` `FileCache`; `:102` `NewFileCache`;
  `:138` `Load`; `:183` `Store`.
- `execution/cached_map.go:44` `MapCached`; `:10` `CacheState`; `:25`
  `CacheReport`; `:34` `CachedMapOptions`.
- `execution/cached_batch_map.go` — `MapCachedBatches`.
- `execution/budget.go:27` `NewBudget`; `:35` `Wait`; `:46` `Reserve`;
  `:107` `BudgetSnapshot`.
- `execution/rate.go:14` `Rate`; `:34` `NewTokenBucket`; `:83` `Wait`;
  `:142` `Close`.
- `execution/chain.go:11` `Chain`.
- `execution/resource_plan.go:11` `ResourcePlan`; `:26`
  `ValidateResourcePlans`; `:89` `NewResourceBudgets`; `:19` `CostPreflight`.
- `execution/mysql_cache.go` — `MySQLCache`, migration, validation.
- `flow/step.go:20` `Identity`; `:29` `Step`; `:80` `Step.Key`.
- `flow/policy.go:11` `Policy`; `:26` `Resource`; `:46` `FailureMode`;
  `:76` `RetrySpec`; `:89` `Backoff`; `:107` `ItemError`.
- `flow/run.go:23` `Preflight`; `:32` `Options`; `:54` `Share`; `:66`
  `Declare`; `:149` `runEnv.ensure`; `:308` `admit`; `:382` `Run`; `:707`
  `typedRunner`; `:882` `process`; `:961` `lead`; `:985` `work`; `:1065`
  `success`; `:1098` `fail`.
- `flow/store.go:20` `Store`; `:30` `MemoryStore`.
- `flow/pipe.go:14` `Pipe2`; `:22` `Pipe3`; `:30` `Pipe4`.
- `flow/classify.go:12` `ErrorClass`; `:86` `AsDataError`; `:102`
  `StatusError`; `:125` `DefaultClassifier`.
- `flow/report.go:14` `Meters`; `:42` `StepReport`; `:89` `Report`; `:112`
  `Result`; `:125` `EventType`; `:144` `Event`; `:156` `Ledger`.
- `flow/bulk.go:24` `Bulk`.
- `flow/batch.go:12` `BatchSpec`; `:44` `Batched`.

### Companion docs

- `README.md` — onboarding-first tour and API chooser table.
- `docs/flowkit-developer-guide.md` — reference-style guide + troubleshooting
  (also served by `flowkit help`).
- `scripts/README.md` — how to run the verified examples.
- `ttmp/2026/08/12/FLOWKIT-001--.../design-doc/01-...md` — extraction
  architecture and compatibility contract.
