---
Title: Flowkit extraction architecture and implementation guide
Ticket: FLOWKIT-001
Status: active
Topics:
    - architecture
    - migration
    - go
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: "Intern-oriented architecture, compatibility contract, and phased implementation guide for extracting ragkit flow and execution into Flowkit."
LastUpdated: 2026-08-12T23:15:00-04:00
WhatFor: "Understand, review, and implement the behavior-preserving Flowkit extraction."
WhenToUse: "Before moving code, reviewing extraction commits, or changing Flowkit cache, admission, retry, or pipeline behavior."
---

# Flowkit extraction architecture and implementation guide

## 1. Executive summary

This ticket extracts two domain-neutral packages from `ragkit` into the standalone module `github.com/go-go-golems/flowkit`: the low-level `execution` primitives and the higher-level typed `flow` orchestration package. They must move together. `flow` publicly exposes `execution.Key`, `execution.CacheOutcome`, `execution.Limiter`, `execution.BudgetSnapshot`, and `execution.CostPreflight`, and internally delegates admission and bounded mapping to `execution`. Moving only `flow` would leave Flowkit coupled to ragkit and would not create a reusable library boundary.

The first release should be a behavior-preserving `v0.x` extraction, not an API redesign. The principal contracts are not merely Go signatures. They include deterministic cache bytes, atomic cache publication, fail-closed corruption handling, position-preserving concurrency, duplicate-key suppression, per-attempt resource charging, transactional rollback across multiple resources, retry classification, and the rule that completed expensive work is committed even if sibling work cancels the run. These contracts are spread across `ragkit/execution/*.go`, `ragkit/flow/*.go`, their tests, and the embedding, generation, and reranking consumers.

The target dependency graph is one-way:

```text
ragkit domain adapters
  ├─ rag/embedding
  ├─ rag/generation
  └─ rag/reranking (execution primitives today)
          |
          v
flowkit/flow  ------------------+
    |                           |
    v                           |
flowkit/execution               |
    |                           |
    +--> flowkit/internal/* <---+

Forbidden: any github.com/go-go-golems/flowkit package importing ragkit.
```

The recommended implementation sequence is: lock compatibility and boundary tests in ragkit; isolate the exact support code; extract and validate `execution`; extract and validate `flow`; split ragkit-specific substring classification out of the generic default without changing ragkit behavior; migrate all ragkit consumers; run a two-module `go.work` compatibility gate; tag Flowkit `v0.1.0`; then make ragkit consume that tag. Redesigns such as merging `flow.Resource` with `execution.ResourcePlan` belong in follow-up commits.

## 2. Reader orientation: what problem does this subsystem solve?

Many applications perform expensive, repeatable work over a collection: call a model, embed text, rerank candidates, transform records, or validate generated output. A naïve loop is insufficient because production runs also need bounded concurrency, rate and budget enforcement, retries, resumability, duplicate suppression, reporting, and predictable item ordering.

Flowkit has two abstraction levels:

- **`execution`** is a toolbox. It provides bounded map, cache-aware map, cache-aware batch map, finite budgets, token buckets, transactional limiter chains, and resource-plan validation. A caller can adopt one primitive without adopting the flow model.
- **`flow`** is an opinionated orchestration layer. A typed `Step[I,O]` combines identity, execution policy, work, metering, and result hooks. `Run` applies the step to many items. `Pipe2`–`Pipe4` compose typed streaming stages. `Bulk` maps one provider request to many individually cached items. `Batched` performs group calls and repairs missing or malformed members one at a time.

`flow/doc.go:1-17` explicitly rejects a general workflow engine: there is no DAG scheduler, persisted control state, or distributed coordinator. Durability means memoization. Resume means replaying the same input and getting cache hits for completed work. At most one in-flight item per worker is lost when a process dies.

### 2.1 Core vocabulary

- **Item:** one input position in a run.
- **Step:** a typed operation from `I` to `O` plus identity and policy (`flow/step.go:21-61`).
- **Identity:** a stable cache namespace (`Kind`, `Version`) plus exact semantic key bytes.
- **Policy:** workers, admission resources, retry behavior, and terminal item-error policy (`flow/policy.go:9-22`).
- **Store / Cache:** load and atomically store a value under `execution.Key`.
- **Limiter:** admits integer resource units, possibly by finite budget or rate.
- **Reservation:** provisional admission that can be committed or rolled back.
- **Run environment:** run-scoped ownership of resource declarations, budgets, rates, and cost preflight.
- **Ledger:** a required-success event sink; ledger failure fails the run.
- **Report:** per-step counters, meters, and resource snapshots.
- **Quarantine:** preserve an item failure as structured data while continuing.
- **Skip:** drop the value but retain a visible count and marker.
- **Barrier:** wait for all upstream items before a stage begins.
- **Repair:** execute an item individually when a group response does not yield a usable result.

## 3. Scope and non-scope

### 3.1 Move into Flowkit

```text
ragkit/flow/                  -> flowkit/flow/
ragkit/execution/             -> flowkit/execution/
required digest functionality -> flowkit/internal/digest/
required atomic write code    -> flowkit/internal/fsutil/
required strict JSON decode   -> flowkit/internal/jsonutil/
```

The observed movable package dependency closure, measured with `GOWORK=off go list -deps ./flow ./execution`, is exactly:

```text
github.com/go-go-golems/ragkit/digest
github.com/go-go-golems/ragkit/execution
github.com/go-go-golems/ragkit/flow
github.com/go-go-golems/ragkit/internal/fsutil
github.com/go-go-golems/ragkit/internal/jsonutil
```

Do not copy all utility APIs merely because they share a package. `execution/cache.go` needs SHA-256 bytes, `fsutil.AtomicWrite`, and strict JSON decoding. `flow/step.go` needs SHA-256 bytes. A small private `internal/digest` package avoids creating an accidental public API.

### 3.2 Keep in ragkit

RAG types and adapters remain in ragkit:

- `rag/embedding/cached.go`: turns embedding items and provider batches into `flow.Bulk`.
- `rag/generation/flow_step.go`: turns `rag.GenerationRequest` into a cache-compatible `flow.Step`.
- `rag/generation/flow_adapters.go`: adapts flow steps to legacy generation interfaces and report shapes.
- `rag/reranking/cached.go`: currently consumes `execution.MapCached` directly and must also change its import.

The handoff named embedding and generation as direct `flow` consumers. Repository inspection adds an important nuance: reranking and several legacy cached decorators consume `execution` directly. Therefore migration scope must inventory both import paths, not only `ragkit/flow`.

### 3.3 Explicit non-goals for the extraction commits

- No DAG scheduler, database-backed workflow state, or distributed execution.
- No wholesale API cleanup.
- No cache schema rename just to remove `rag-ttc` wording.
- No forwarding `ragkit/flow` or `ragkit/execution` compatibility packages unless a concrete consumer is found and maintainers explicitly approve one.
- No movement of provider or RAG adapters into Flowkit.
- No `v1` compatibility promise.

## 4. Current-state architecture

Repository evidence shows approximately 3,645 production lines and 2,930 test lines across `flow` and `execution`. The focused suites pass with workspace mode disabled:

```bash
cd ragkit
GOWORK=off go test ./flow ./execution ./rag/embedding ./rag/generation ./rag/reranking -count=1
```

The subsystem is therefore substantial and tested, but recent history (`git log -- flow execution`) contains repeated hardening changes around admission, accounting, integrity, staged validation, and result hooks. This supports a `v0.x` release and argues against combining extraction with redesign.

### 4.1 Layer map

```text
+------------------------------------------------------------------+
| Application / ragkit adapters                                    |
| choose domain keys, provider calls, usage mapping, classifier     |
+----------------------------------+-------------------------------+
                                   |
+----------------------------------v-------------------------------+
| flow                                                              |
| Step + Identity + Policy                                           |
| Run / Pipe / Bulk / Batched                                        |
| runEnv / retries / failure modes / ledger / reports / in-flight   |
+----------------------------------+-------------------------------+
                                   |
+----------------------------------v-------------------------------+
| execution                                                         |
| Map / MapCached / MapCachedBatches                                |
| Cache + FileCache + Key                                            |
| Budget + TokenBucket + Chain + Reservation                         |
| ResourcePlan + preflight                                           |
+----------------------------------+-------------------------------+
                                   |
+----------------------------------v-------------------------------+
| internal support                                                   |
| SHA-256 digest / strict JSON / atomic file publication             |
+------------------------------------------------------------------+
```

### 4.2 `execution.Map`: bounded ordered concurrency

`execution/map.go:50-112` creates an `errgroup`, a job producer, a fixed number of workers, and a result slice preallocated to input length. Workers write `results[current.index]`; completion order is irrelevant, so return order always matches input order. The first error cancels the errgroup and is annotated with the item index.

Pseudocode:

```text
function Map(items, workers, limiter, cost, work):
    results = array(len(items))
    jobs = channel(index, item)
    group = cancellable errgroup

    producer:
        send every indexed item unless group is cancelled

    start min(max(workers, 1), len(items)) workers:
        for job in jobs:
            units = cost(job.item) or 1
            reject units < 1
            limiter.Wait(units), if configured
            results[job.index] = work(job.item)

    if any goroutine fails: return annotated error
    return results
```

The important invariants are bounded active calls, cancellation propagation, positive resource units, and position preservation.

### 4.3 Cache identity and durable envelopes

`execution/cache.go:27-77` defines a stable `Key`:

```go
type Key struct {
    Step        string `json:"step"`
    Version     string `json:"version"`
    InputDigest string `json:"input_digest"`
}
```

`NewKey` JSON-marshals semantic input and hashes those bytes with SHA-256. The file name is another SHA-256 over the JSON encoding of the complete key. `FileCache` stores this envelope (`execution/cache.go:124-129`):

```go
type cacheEnvelope struct {
    SchemaVersion string          `json:"schema_version"`
    Key           Key             `json:"key"`
    ValueDigest   string          `json:"value_digest"`
    Value         json.RawMessage `json:"value"`
}
```

The schema string is currently `rag-ttc-execution-cache/v1`. Despite the historical name, changing it during extraction would intentionally invalidate every entry. Preserve it unless a cache epoch is approved.

Load is fail-closed (`execution/cache.go:140-181`): an absent file is a miss, while an oversized file, unknown field, wrong schema, wrong full key, wrong value digest, or invalid value is `ErrCorruptCache`. It must not silently recompute expensive work because corruption could mask tampering or produce inconsistent results.

Store is crash-conscious (`execution/cache.go:185-224`): marshal value, calculate value digest, marshal envelope, enforce maximum size, write a sibling temporary file, sync it, rename it atomically, then sync the directory. `internal/fsutil/fsutil.go:22-73` supplies those publication semantics.

```text
semantic input
   |
   v JSON bytes
SHA-256 ----------------------> Key.InputDigest
   |                                  |
   + Kind/Version --------------------+
                                      v JSON(Key)
                                  SHA-256
                                      |
                                      v
cache/<first 2 hex>/<full key digest>.json
```

### 4.4 Cache-aware map and duplicate suppression

`execution.MapCached` (`execution/cached_map.go:44-172`) groups inputs by key digest before loading. One cached value populates all duplicate positions. Only unique misses enter `Map`, and each successful miss is stored immediately with `context.WithoutCancel(ctx)`. This deliberate cancellation boundary protects completed expensive work from sibling cancellation.

`MapCachedBatches` applies the same model to batches (`execution/cached_batch_map.go:26-181`). Limiter cost is the sum of unique missed-item costs. The batch function must return exactly one result per unique group. Each returned value is committed independently, preserving earlier results if a later store fails.

### 4.5 Budgets, rates, reservations, and chains

A `Budget` is finite and non-replenishing (`execution/budget.go:17-104`). Successful admission permanently consumes units even if provider work later fails; this models attempted spend and prevents retries from escaping the experiment ceiling.

A `TokenBucket` replenishes to a burst capacity (`execution/rate.go:22-143`). Reservations acquire tokens provisionally. Cancellation during partial acquisition refunds the tokens. `Close` stops replenishment and unblocks waiters.

`Chain` (`execution/chain.go:11-69`) composes limiters in order. Reservable limiters produce provisional reservations. If a later limiter refuses, earlier reservations roll back in reverse order. Commit occurs only after all limiters admit.

```text
request N units
   |
   v
Budget.Reserve ----success----> TokenBucket.Reserve ----success----> commit both
   |                                  |
 refusal                              refusal
   |                                  |
 no charge                            rollback budget reservation
```

Custom `Limiter` implementations that only implement `Wait` remain usable, but successful admission through them cannot be rolled back. This limitation must remain documented.

### 4.6 Resource plans and cost preflight

`execution.ResourcePlan` (`execution/resource_plan.go:11-16`) declares a name, worst-case ceiling, admitted budget, and optional unit price. `ValidateResourcePlans` rejects duplicate/empty names, negative values, invalid floating-point prices, insufficient budget when partial work is disallowed, unknown pricing when unpriced work is disallowed, and estimated cost over the monetary maximum.

`flow.Resource` duplicates the same fields (`flow/policy.go:26-42`). The duplication is visible technical debt, but `flow` adds shared declarations and ownership diagnostics in `runEnv.ensure` (`flow/run.go:126-238`). Keep both representations for the first extraction unless changing them is necessary to break a cycle; consolidate later.

### 4.7 `flow.Step`: identity, policy, work, and observation

`flow.Step[I,O]` (`flow/step.go:30-61`) is the central public abstraction:

```go
type Step[I, O any] struct {
    Name         string
    Identity     Identity[I]
    Policy       Policy
    Barrier      bool
    Do           func(context.Context, I) (O, error)
    Meter        func(O) Meters
    AttemptMeter func(O, error) Meters
    OnResult     func(context.Context, int, O, execution.CacheOutcome) error
    // private composition fields
}
```

Identity and policy are deliberately separate. Identity contains only semantic inputs that affect the output. Worker count, retries, rates, and budget must never enter a cache key. An empty `Identity.Kind` means uncached computation.

`Step.Key` (`flow/step.go:72-92`) hashes the exact bytes returned by `Identity.Key` and directly maps `Kind` and `Version` onto `execution.Key`. This is how adapters reproduce historical generation and embedding keyspaces.

### 4.8 `flow.Run`: validation, preflight, and execution

`Run` (`flow/run.go:382-418`) performs these phases:

```text
1. Flatten a composed step into stage specifications.
2. Validate every visible and nested policy.
3. Create or reuse a runEnv.
4. Collect and atomically validate all resource plans before item 0.
5. Dispatch a custom Bulk/Batched engine, or run streaming stages.
6. Restore output positions and return a best-effort report with errors.
```

`Options.Share` allows multiple top-level calls to share a run environment (`flow/run.go:50-61`). Same-name resources share one budget only if declarations are identical. A name-only `Resource{Name: ...}` references an existing plan. A mismatched declaration fails before provider work.

`runEnv.admit` (`flow/run.go:314-344`) creates a transaction across resource names. If admission of resource three fails, reservations for resources two and one roll back. Once all succeed, every reservation commits.

### 4.9 Per-item engine: cache → admission → retry → store

The typed runner's critical path is distributed across `process`, `lead`, `work`, `success`, and `fail` in `flow/run.go:674-943`.

```text
process(index, item)
  |
  +-- uncached -------------------------------> work
  |
  +-- cached: build key + digest
         |
         +-- duplicate already in flight -----> follow leader
         |
         +-- become leader
                |
                +-- Store.Load hit -----------> report + ledger + hook
                |
                +-- miss ---------------------> work
                                                   |
                         for each attempt: admit all resources
                                                   |
                                            Step.Do(item)
                                              /          \
                                       success            error
                                          |                 |
                                     meter result      classify error
                                          |          transient + attempts?
                                  Store.Store          retry with backoff
                                  without cancel             |
                                          |           exhausted/nonretryable
                                  ledger + hook      fail fast/quarantine/skip
```

Every physical attempt obtains fresh admission. Cache hits obtain none. Fatal errors always fail the run regardless of item policy. Ledger and result-hook errors are also fatal because they are part of the observable contract.

### 4.10 Pipelines and barriers

`Pipe2`, `Pipe3`, and `Pipe4` flatten stage lists (`flow/pipe.go:14-53`). `runStages` connects each stage with channels and uses an order-restoring collector (`flow/run.go:499-558`). Item `i` can enter stage two as soon as stage one finishes it; a slow item does not impose a global barrier. A stage with `Barrier=true` drains all upstream items before processing. `Batched` is represented as a barrier stage because it needs a complete collection.

Quarantined and skipped items bypass all later stages and retain their original positions.

### 4.11 Bulk execution

`Bulk` (`flow/bulk.go:24-32`) is for APIs such as embeddings where one request accepts many items and returns one ordered result per item. It:

- groups duplicate keys;
- loads hits before admission;
- batches unique misses;
- charges units per unique missed item;
- retries the complete provider batch;
- requires output cardinality to equal input cardinality;
- stores each item separately;
- expands one result to duplicate input positions.

Do not confuse `Bulk` with `Batched`. Bulk output is structurally complete and ordered when successful.

### 4.12 Batching with repair

`Batched` (`flow/batch.go:44-58`) handles group responses that may omit, corrupt, or fail individual members. `BatchSpec.Group` returns input-index groups, `DoAll` returns a raw response, and `Split` maps group-local positions to outputs. Uncovered indexes, split failures, missing positions, and nonfatal failed groups enter the repair step.

```text
all items
  |
  +--> validate group indexes (in range, no duplicates)
  |
  +--> group calls via ordinary flow.Run
  |       |
  |       +--> parse complete members --------> final result slots
  |       +--> malformed/missing/nonfatal ----+
  |
  +--> uncovered items ------------------------+--> repair Run
                                                       |
                                                       v
                                             restore original indexes
```

The repair step's identity must match the standalone per-item identity so both paths share cache entries. Group and repair policies can share a named resource through the same run environment.

### 4.13 Error classification and the domain-neutrality gap

The classification API is generic (`flow/classify.go:13-113`): `Transient`, `DataError`, and `Fatal`; a `Classifier` interface; `AsDataError`; context handling; typed HTTP statuses; and fail-closed unknowns.

However, `transientMarkers` (`flow/classify.go:121-160`) contains incident-specific strings and comments about judge runs, summary builds, embeddings, and provider outages. That table is mechanically reusable but not domain-neutral policy.

Flowkit should retain:

- explicit data-error markers;
- typed HTTP status classification;
- context cancellation/deadline handling;
- budget-exhaustion handling;
- classifier interfaces and composition;
- fatal fallback for unknown errors.

Ragkit should own or explicitly opt into the historical substring classifier. Ragkit adapter tests must prove that representative old messages keep their retry behavior after the move.

## 5. Consumer and compatibility analysis

### 5.1 Known consumers

A repository-wide import search identifies:

- `flow` consumers in embedding and generation production code and tests;
- `execution` consumers in embedding, generation, reranking, provider contract tests, and older cached decorators;
- `flow` itself as an `execution` consumer.

Before deletion, repeat the search across local workspaces, GitHub, private modules, and `go.mod`/`go.sum` files:

```bash
rg -n 'github.com/go-go-golems/ragkit/(flow|execution)' ~/code ~/workspaces --glob '*.go' --glob 'go.mod'
gh search code 'github.com/go-go-golems/ragkit/flow' --language go
gh search code 'github.com/go-go-golems/ragkit/execution' --language go
```

Absence from indexed GitHub search is evidence, not proof, that no private consumer exists.

### 5.2 Cache compatibility is a data-format contract

Import-path compatibility may be intentionally broken, but cache compatibility should be preserved. Protect all of these:

- `Key` JSON field names and field order as emitted by Go JSON;
- input digest algorithm and exact adapter input bytes;
- key digest algorithm and two-character directory sharding;
- envelope JSON field names;
- schema string `rag-ttc-execution-cache/v1`;
- value digest algorithm;
- strict decoding and corruption behavior;
- maximum-size behavior;
- atomic write and directory sync semantics;
- `CacheState` strings: `hit`, `stored`, `pending`;
- domain adapter `Identity.Kind`, version, and key-byte construction.

A module-path change must not create a cache epoch.

### 5.3 Cross-version fixture strategy

Before moving code, generate fixtures using ragkit's implementation and commit them as test data. After extraction:

```text
old ragkit writer -> fixture -> new Flowkit reader
new Flowkit writer -> temporary file -> old ragkit reader (during migration window)
```

Fixtures should include a valid hit and intentionally corrupt variants: unknown field, wrong schema, wrong key, wrong value digest, malformed value, and oversized entry. Record the semantic input, expected key, expected digest, relative file path, and expected decoded value in a manifest so failures are diagnosable.

## 6. Proposed target architecture

```text
flowkit/
  go.mod                         module github.com/go-go-golems/flowkit
  README.md
  LICENSE
  flow/
    batch.go                     group call + repair orchestration
    bulk.go                      ordered bulk calls over unique misses
    classify.go                  generic typed classification only
    doc.go
    pipe.go
    policy.go
    report.go
    run.go
    step.go
    store.go
    *_test.go
    example_test.go
  execution/
    budget.go
    cache.go
    cached_batch_map.go
    cached_map.go
    chain.go
    doc.go
    map.go
    rate.go
    reservation.go
    resource_plan.go
    *_test.go
    example_test.go
  internal/
    digest/                      only stable SHA-256 helpers required here
    fsutil/                      atomic write + directory sync required here
    jsonutil/                    strict decode required here
  internal/boundarytest/         optional shared import-boundary helper
  testdata/cache-compat/         pre-extraction fixtures and manifest
```

The current Flowkit template still declares `module github.com/go-go-golems/XXX` in `flowkit/go.mod`. Renaming the module, placeholder command/package cleanup, and Go version alignment are bootstrap work, not evidence that extraction has already happened.

### 6.1 Public API posture

For `v0.1.0`, preserve exported names and behavior except import paths. Keep `flow` and `execution` separate. This lets advanced users adopt `execution.Map` or `MapCached` directly while applications needing retries, policies, and composition adopt `flow`.

Add external-package tests (`package flow_test`, `package execution_test`) and examples for:

1. bounded ordered `execution.Map`;
2. durable cached map with `FileCache`;
3. typed cached `flow.Step`;
4. retry plus finite resource budget;
5. streaming `Pipe2` and a barrier;
6. `Bulk` with duplicate inputs;
7. `Batched` with missing-member repair;
8. shared `Options` across calls;
9. custom `Store` and `Ledger` implementations.

### 6.2 Boundary enforcement

Add a test or CI command that loads all Flowkit packages and rejects imports prefixed by `github.com/go-go-golems/ragkit`.

Pseudocode:

```text
packages = go list -json ./...
for package in packages:
    for importPath in package.Imports:
        if importPath startsWith "github.com/go-go-golems/ragkit":
            fail(package.ImportPath + " imports forbidden " + importPath)
```

Also reject RAG-specific terminology in generic package docs, except migration notes and compatibility-test fixture descriptions.

## 7. Design decisions

### Decision: extract `flow` and `execution` together

- **Context:** `flow` exposes and consumes many `execution` types.
- **Options considered:** move only `flow`; merge both into one package; move both as separate packages in one module.
- **Decision:** move both as separate packages in Flowkit.
- **Rationale:** this removes the ragkit dependency while preserving the useful low-level/high-level abstraction boundary.
- **Consequences:** ragkit must update both import families; Flowkit maintains two public packages.
- **Status:** proposed.

### Decision: preserve behavior before API cleanup

- **Context:** recent hardening and subtle concurrency/cache semantics make combined redesign difficult to review.
- **Options considered:** redesign during extraction; mechanically preserve first and refactor later.
- **Decision:** preserve signatures, JSON, errors, and runtime behavior in extraction commits.
- **Rationale:** small semantic deltas make cache and accounting regressions detectable.
- **Consequences:** temporary duplication such as `flow.Resource` remains.
- **Status:** proposed.

### Decision: preserve the existing cache epoch

- **Context:** cache entries represent expensive completed work and include historical schema naming.
- **Options considered:** rename schema and invalidate; support two schemas; retain existing schema.
- **Decision:** retain bytes and schema for `v0.1.0`.
- **Rationale:** package movement is not a semantic change to cached work.
- **Consequences:** historical `rag-ttc` text remains in a private constant; fixtures become a release gate.
- **Status:** proposed.

### Decision: use private support packages

- **Context:** only small portions of ragkit digest/fs/json helpers are required.
- **Options considered:** make digest public; copy whole packages; create minimal private helpers.
- **Decision:** use `flowkit/internal/digest`, `internal/fsutil`, and `internal/jsonutil` with only required behavior.
- **Rationale:** minimizes public surface and accidental coupling.
- **Consequences:** support tests must move with the selected functions; future public use requires an explicit API decision.
- **Status:** proposed.

### Decision: separate generic and ragkit-specific classification

- **Context:** the default classifier contains incident-specific string markers.
- **Options considered:** leave them as generic defaults; delete them; move them to ragkit and compose explicitly.
- **Decision:** Flowkit defaults to typed generic classification; ragkit composes the historical fallback explicitly.
- **Rationale:** preserves domain neutrality without silently changing ragkit retries.
- **Consequences:** adapter wiring changes and regression tests are mandatory.
- **Status:** proposed.

### Decision: no compatibility shims without a known consumer

- **Context:** aliases reduce immediate source breakage but preserve the unwanted dependency boundary.
- **Options considered:** forwarding packages, type aliases, hard import migration.
- **Decision:** update consumers directly unless Phase 0 proves a shim is required and maintainers approve it.
- **Rationale:** the repository guidelines explicitly avoid unrequested compatibility layers.
- **Consequences:** unknown private consumers may require coordinated migration.
- **Status:** proposed.

### Decision: retain separate `flow.Resource` for the first release

- **Context:** `flow.Resource` and `execution.ResourcePlan` duplicate fields.
- **Options considered:** consolidate now; retain during move; permanently duplicate.
- **Decision:** retain during behavior-preserving extraction and open a focused follow-up.
- **Rationale:** run-scoped reference semantics and owner-attributed errors need careful API design beyond a mechanical type replacement.
- **Consequences:** small duplication remains but extraction review stays narrow.
- **Status:** proposed.

### Decision: release as `v0.1.0`

- **Context:** APIs are tested but have not been validated by a second non-RAG consumer.
- **Options considered:** no tag, `v0.1.0`, or `v1.0.0`.
- **Decision:** tag `v0.1.0` after compatibility gates pass.
- **Rationale:** provides a stable dependency target without promising mature compatibility.
- **Consequences:** ragkit should consume the tag, not retain a permanent `replace`.
- **Status:** proposed.

## 8. Detailed implementation guide

### Phase 0: confirm naming, history, consumers, and compatibility policy

1. Confirm repository/module name `github.com/go-go-golems/flowkit`.
2. Decide whether to preserve source history using `git filter-repo`, subtree, or ordinary copied commits.
3. Search local, public, and private consumers of both packages.
4. Record whether source compatibility is required.
5. Explicitly approve cache interoperability and no cache epoch.
6. Align `go.work` and module Go versions before relying on workspace-mode commands. The current workspace declares `go 1.26` while modules require patch versions such as `1.26.5`; current `go list`/`go test` in workspace mode fails and requires either `GOWORK=off` or a workspace fix.

Exit evidence: written answers to open decisions and a consumer inventory.

### Phase 1: lock ragkit behavior before moving

Add tests in ragkit for:

- dependency boundary (`flow`/`execution` import no `rag/*`);
- pre-extraction cache fixtures;
- exact key JSON and key digest;
- strict corruption behavior;
- atomic publication expectations;
- order preservation and cancellation;
- duplicate suppression;
- cache-hit-free admission;
- retry-per-attempt admission;
- multi-resource rollback;
- result commit after sibling cancellation;
- representative historical classifier strings.

Prefer black-box external-package tests for public contracts. Commit this phase independently, e.g. `test: lock Flowkit extraction contracts`.

### Phase 2: bootstrap Flowkit

1. Change `flowkit/go.mod` from the `XXX` placeholder to `github.com/go-go-golems/flowkit`.
2. Remove or rename template placeholder packages and commands.
3. Align the Go version with the workspace policy.
4. Configure logging generation for Flowkit package names, or remove library logging only if observation seams and tests support that decision.
5. Add the forbidden-import boundary check.
6. Add README scope, stability, and cache-compatibility statements.

Do not import ragkit as an implementation shortcut.

### Phase 3: isolate support code

Copy only needed support behavior and tests:

- `digest.Bytes` semantics: lowercase hexadecimal SHA-256.
- `fsutil.AtomicWrite` and `SyncDirectory` semantics.
- strict JSON decoding used by cache envelopes and values.

Update imports in extracted code to `github.com/go-go-golems/flowkit/internal/...`. Verify byte-for-byte digest fixtures before proceeding.

### Phase 4: extract `execution`

Move production files and tests. Preserve package names and public API. Update only internal support imports. Validate:

```bash
cd flowkit
go fmt ./...
go test ./execution ./internal/... -count=1
go test -race ./execution ./internal/... -count=1
go vet ./execution ./internal/...
```

Review in this order:

1. `execution/cache.go` and compatibility fixtures;
2. `execution/map.go` ordering/cancellation;
3. `cached_map.go` and `cached_batch_map.go` duplicate/store semantics;
4. `budget.go`, `rate.go`, `reservation.go`, and `chain.go` rollback;
5. `resource_plan.go` validation arithmetic.

Commit independently, e.g. `feat(execution): extract execution primitives from ragkit`.

### Phase 5: extract `flow`

Move flow production files and tests. Replace ragkit execution imports with Flowkit execution imports and digest imports with the private helper. Preserve all public names and private runtime structure.

Validate first with focused tests, then race tests. Pay special attention to shared `runEnv`, in-flight maps, report meters, hooks, and `Bulk` shared result writes.

Commit independently, e.g. `feat(flow): extract typed orchestration from ragkit`.

### Phase 6: split classifier policy

Create a generic Flowkit default classifier and a ragkit-owned historical classifier/composition function. A possible API sketch is:

```go
// Flowkit
type ClassifierFunc func(error) ErrorClass
func ChainClassifiers(classifiers ...Classifier) Classifier // only if semantics are explicit
var DefaultClassifier Classifier // typed statuses, context, budget, fatal unknown

// Ragkit
var ProviderIncidentClassifier flow.Classifier = flow.ClassifierFunc(...markers...)
func DefaultFlowClassifier() flow.Classifier {
    return ragkitTypedAndMarkerComposition
}
```

Composition must distinguish “unrecognized” from “fatal” if chaining is introduced; otherwise a first classifier's fail-closed `Fatal` would prevent fallback. A safer first implementation may keep one ragkit classifier that performs typed generic checks plus the migrated table, while Flowkit independently has its generic default. Do not invent an `Unknown` class merely to support composition without reviewing public semantics.

Regression tests should cover typed 429/408/5xx, other HTTP status, cancellation, deadline, budget exhaustion, data errors, known historical strings, and unknown fatal errors.

### Phase 7: migrate ragkit consumers

Update every production and test import found by repository search, including reranking and legacy cached decorators. For embedding and generation, preserve exact adapter identity bytes and report conversion behavior.

During local integration use a temporary `go.work` or temporary `replace`:

```go
use ./flowkit
use ./ragkit
```

A local `replace` must not remain in the final ragkit release commit.

Do not delete old packages until all consumers compile against Flowkit and compatibility tests pass.

### Phase 8: add public documentation and examples

Write package docs that teach the two layers and make explicit what Flowkit is not. Add compiling external examples listed in §6.1. Document lifecycle requirements such as closing token buckets and sharing options when budgets must span calls.

### Phase 9: cross-repository validation

Run:

```bash
# Flowkit
GOWORK=off go test ./... -count=1
GOWORK=off go test -race ./... -count=1
GOWORK=off go vet ./...
golangci-lint run -v

# Ragkit against local Flowkit in a corrected go.work
GOWORK=/absolute/path/to/go.work go test ./... -count=1
GOWORK=/absolute/path/to/go.work go test -race ./rag/embedding ./rag/generation ./rag/reranking -count=1
GOWORK=/absolute/path/to/go.work go vet ./...

# Boundary
go list -deps ./... | grep 'github.com/go-go-golems/ragkit' && exit 1 || true
```

Also run generation checks and confirm no stale generated logging files.

### Phase 10: release and cleanup

1. Merge Flowkit extraction.
2. Tag Flowkit `v0.1.0`.
3. Update ragkit to require the tag.
4. Run ragkit CI without a local replacement.
5. Remove migrated ragkit packages.
6. Publish release notes covering `v0` stability and cache compatibility.

## 9. Validation matrix

| Contract | Primary evidence | Required test |
|---|---|---|
| Input order preserved | `execution/map.go:69-107`; `flow/run.go:382-418` | delayed out-of-order workers return aligned results |
| Duplicate key runs once | `cached_map.go:72-96`; `flow/run.go:674-731` | duplicates produce one work call and identical outcomes |
| Hits are free | load occurs before `work`/admit | exhausted budget still permits cache hits |
| Retry is charged | `flow/run.go:798-855` | N attempts consume N units |
| Admission rolls back | `execution/chain.go:39-67`; `flow/run.go:314-344` | later refusal restores earlier reservations |
| Corruption fails closed | `execution/cache.go:140-181` | invalid existing entry returns `ErrCorruptCache`, no work call |
| Completed work survives sibling failure | `context.WithoutCancel` stores | successful sibling is a hit on rerun |
| Fatal overrides item policy | `flow/run.go:919-943` | fatal + quarantine policy still fails run |
| Unknown classifier fails closed | classifier validation and fatal fallback | malformed class / unknown error fails visibly |
| Pipeline streams | channel stages in `runStages` | downstream item begins before all upstream complete |
| Barrier waits | typed runner barrier buffer | no downstream start before upstream drain |
| Bulk cardinality | `flow/bulk.go` result-length check | wrong result count fails |
| Batched repairs | `flow/batch.go:100-169` | uncovered/missing/split error repaired at original indexes |
| Hooks and ledgers are required | notify/event error propagation | callback failure fails run |
| Meter values finite | `Meters.AddChecked` | NaN/Inf and overflow rejected |
| Shared plans agree | `runEnv.ensure` | mismatched same-name declaration fails before work |
| Cache remains interoperable | schema/key fixtures | old→new and temporary new→old round trip |
| No ragkit dependency | module boundary check | forbidden prefix absent from all imports |

## 10. Risks and mitigations

### Cache-key drift

A harmless-looking change from exact byte hashing to hashing a higher-level object can invalidate caches. Mitigate with golden key/path/envelope fixtures generated before extraction.

### Partial semantic redesign

Changing types while moving files makes regressions hard to attribute. Mitigate with phase-specific commits and defer resource consolidation.

### Retry behavior drift

Removing string markers makes historical provider failures fatal. Leaving them in Flowkit undermines neutrality. Mitigate with an explicit ragkit classifier and behavior-regression tests.

### Incomplete consumer migration

The handoff's flow-consumer list does not include all direct execution users. Mitigate by searching both import paths and making the old-package removal commit depend on a clean search.

### Workspace false negatives

The current `go.work` version is lower than module patch requirements, so workspace-mode Go commands fail before tests run. Mitigate by repairing the workspace or using `GOWORK=off` for module-local evidence; record which mode produced each result.

### Data races

Reports, meters, shared environments, in-flight deduplication, bulk results, and hooks execute concurrently. Mitigate with race tests and focused high-contention tests.

### Generated logging coupling

Copied generated logger files may retain ragkit package or generation metadata. Mitigate by regenerating under Flowkit and enforcing clean generation in CI.

### Custom non-reservable limiters

A custom `Limiter` that lacks `Reserve` cannot be rolled back after success. Mitigate through documentation; do not claim fully transactional admission for arbitrary custom limiters.

## 11. Alternatives considered

### Extract only `flow`

Rejected because the public API and implementation retain a ragkit `execution` dependency.

### Merge all code into one package

Rejected because low-level focused primitives are useful without adopting typed orchestration, and merging would expand the extraction's semantic diff.

### Keep the incident string table as Flowkit's default

Rejected as the final architecture because it embeds ragkit operational history in a generic library. It may be tolerated only as a short-lived mechanical extraction commit followed immediately by a tested policy split.

### Introduce forwarding packages in ragkit

Not recommended without a known consumer. They perpetuate the old namespace and create two apparent homes for the API.

### Start a new cache schema

Rejected unless maintainers explicitly choose an epoch. It discards expensive reusable results for no semantic reason.

### Replace both resource types immediately

Deferred. The field duplication is real, but flow-specific reference semantics and diagnostics need focused design and migration tests.

## 12. Intern review checklist

Before opening each pull request, answer these questions with command output or tests:

- Did this commit change any exported name beyond its import path?
- Did it change any JSON tag, constant string, digest input, or filename rule?
- Can a cache hit reach a limiter?
- Can a retry call `Do` without new admission?
- Can cancellation prevent storage after expensive work has succeeded?
- Can two equal keys execute twice in one process?
- Can any output move to a different input index?
- Can a fatal error be quarantined or skipped?
- Can a ledger/hook error be ignored?
- Can Flowkit import ragkit directly or transitively?
- Did tests run in workspace mode or with `GOWORK=off`, and why?
- Is the commit mechanical extraction, policy separation, consumer migration, or redesign? Do not mix categories.

## 13. Open questions requiring maintainer decisions

1. Is `github.com/go-go-golems/flowkit` final?
2. Must private consumers retain source compatibility?
3. Is old/new cache interoperability mandatory? This guide recommends yes.
4. Should source history be preserved with filtered extraction?
5. Should generic logging remain logcopter-based or be reduced to observation seams?
6. Where exactly should the historical classifier live inside ragkit?
7. Should `flow.Resource` consolidation occur after `v0.1.0`?
8. Which non-RAG application will validate the API before `v1`?
9. Should reranking remain on low-level `execution.MapCached` or later adopt a flow step? This is a follow-up design, not extraction scope.

## 14. File and API reference map

### Execution layer

- `ragkit/execution/map.go`: `Limiter`, `Reservation`, `ReservableLimiter`, `MapOptions`, `Map`.
- `ragkit/execution/cache.go`: `Key`, `Cache`, `FileCache`, durable envelope contract.
- `ragkit/execution/cached_map.go`: cache outcomes/reports and unique-miss map.
- `ragkit/execution/cached_batch_map.go`: unique-miss batch execution.
- `ragkit/execution/budget.go`: finite attempted-work budget.
- `ragkit/execution/rate.go`: token bucket lifecycle and reservations.
- `ragkit/execution/chain.go`: transactional limiter composition.
- `ragkit/execution/reservation.go`: idempotent commit/rollback state.
- `ragkit/execution/resource_plan.go`: resource validation and cost preflight.

### Flow layer

- `ragkit/flow/doc.go`: package boundary and non-goals.
- `ragkit/flow/step.go`: `Identity`, `Step`, exact key mapping.
- `ragkit/flow/policy.go`: admission, retries, and failure modes.
- `ragkit/flow/run.go`: shared environment, preflight, pipeline driver, typed item engine.
- `ragkit/flow/pipe.go`: typed streaming composition.
- `ragkit/flow/bulk.go`: provider bulk calls over unique misses.
- `ragkit/flow/batch.go`: grouped calls with per-item repair.
- `ragkit/flow/classify.go`: classifier API and current domain-specific fallback gap.
- `ragkit/flow/store.go`: store seam and in-memory implementation.
- `ragkit/flow/report.go`: results, meters, reports, events, ledger.

### Support and consumers

- `ragkit/internal/fsutil/fsutil.go`: atomic file publication.
- `ragkit/internal/jsonutil/jsonutil.go`: strict cache JSON decoding.
- `ragkit/digest/digest.go`: SHA-256 behavior; extract only required operations.
- `ragkit/rag/embedding/cached.go`: `flow.Bulk` adapter and historical key compatibility.
- `ragkit/rag/generation/flow_step.go`: generation step identity and usage metering.
- `ragkit/rag/generation/flow_adapters.go`: legacy interface/report adapters and shared options.
- `ragkit/rag/reranking/cached.go`: direct low-level execution consumer.
- `/tmp/handoff-flowkit-extraction.md`: initial migration analysis and oversight checklist.

## 15. Definition of done

The extraction is complete only when:

- Flowkit contains the two packages and only required support code;
- Flowkit's module path is final and imports no ragkit package;
- old cache fixtures load in Flowkit and temporary reverse compatibility passes;
- Flowkit unit, race, vet, lint, generation, and external examples pass;
- ragkit unit/race tests pass against the local and then tagged Flowkit module;
- all ragkit consumers import Flowkit;
- RAG adapters and historical retry policy remain in ragkit;
- no unapproved forwarding packages or permanent `replace` directives remain;
- `v0.1.0` release notes state stability and cache compatibility;
- every checked migration item has concrete evidence, not assumption.
