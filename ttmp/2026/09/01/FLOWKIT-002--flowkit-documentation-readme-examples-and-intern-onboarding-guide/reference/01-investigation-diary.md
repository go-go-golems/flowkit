---
Title: Investigation diary
Ticket: FLOWKIT-002
Status: active
Topics:
    - documentation
    - go
    - onboarding
    - examples
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://README.md
      Note: Current README — dense, jargon-first; the comprehensibility problem this ticket targets
    - Path: repo://execution/cache.go
      Note: content-addressed FileCache, Key, envelope, fail-closed corruption
    - Path: repo://execution/map.go
      Note: bounded ordered Map — feeder+workers+ordered results, first error cancels
    - Path: repo://flow/run.go
      Note: Run engine, stages, typedRunner, inflight dedup, commit-after-cancel
    - Path: repo://flow/step.go
      Note: Step[I,O] = Identity+Policy+Do; identity vs policy discipline
    - Path: repo://flow/store.go
      Note: Store durability seam (FileCache/MemoryStore/MySQLCache)
ExternalSources: []
Summary: 'Chronological investigation diary for FLOWKIT-002: analyzing flowkit, writing examples, README, and an intern onboarding guide.'
LastUpdated: 2026-09-01T17:45:00-04:00
WhatFor: Record what flowkit is, how its parts fit together, what was tried, and how to validate the documentation deliverables.
WhenToUse: Read before resuming FLOWKIT-002 work; review the analysis-to-date before editing the onboarding guide.
---







# Investigation diary

## Goal

Capture the step-by-step investigation of the `flowkit` Go library so that the
final deliverable (a rewritten README, a set of runnable examples in `scripts/`,
and an intern onboarding design-doc) is grounded in real evidence from the
source rather than guesswork. New developers should be able to read the
onboarding guide cold and understand what flowkit is, why it exists, and how
to use every public API.

## Step 1: Orient — what is flowkit and what already exists?

Flowkit is a domain-neutral Go library extracted from `ragkit`. It provides
"bounded, recoverable, policy-controlled work over collections" — i.e. it runs
expensive, repeatable work over a list of inputs while preserving input order,
caching results durably, bounding concurrency/budget/rate, retrying with error
classification, and reporting what happened. It is explicitly NOT a workflow
engine, DAG scheduler, or distributed coordinator: it keeps no persisted
control state. "Resume" means replay the same inputs and get cache hits for
already-completed items.

The repository has two public packages and some internals:

- `execution/` — the low-level toolbox: bounded `Map`, content-addressed
  `FileCache` / `MySQLCache`, cache-aware `MapCached` / `MapCachedBatches`,
  finite `Budget`, `TokenBucket` rate, transactional `Chain`, `ResourcePlan`
  preflight, atomic file writes.
- `flow/` — the opinionated typed layer: `Step[I,O]` (identity + policy + Do),
  `Run`, streaming `Pipe2/3/4`, `Bulk`, `Batched` (group call + per-item
  repair), error `Classifier`, `Store` (durability seam), `Report`/`Meters`/
  `Ledger`, shared `Options`/`runEnv` for budgets.
- `internal/` — `digest` (SHA-256 hex), `jsonutil` (strict JSON), `fsutil`
  (atomic writes, containment joins), `testsupport/mysqltest`.
- `cmd/flowkit` — a minimal Glazed-help CLI that only hosts the help system.

### Prompt Context

**User prompt (verbatim):** "We want to write good documentation and README for flowkit, and a set of examples on hwo to use it, to make it understandaable for users (for example, the README is pretty incomprehensible). Create a new docmgr ticket for that. Analyze the repository, write examples tests in the scripts/ folder, research and think hard, imagine you are a new developer joining the project and you have no iea what it is about. Keep a detailed diary of your thoughts and discoveries, updating it frequently. Finally, when you are done, Create a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Create docmgr ticket FLOWKIT-002, deeply analyze the flowkit repo, write runnable example tests under `scripts/`, keep a detailed diary, then write a clear technical intern onboarding design-doc with prose/bullets/pseudocode/diagrams/API+file refs, store it in the ticket, validate, and upload to reMarkable.

**Inferred user intent:** The current README is too dense/jargony for a newcomer. The user wants onboarding material that actually teaches flowkit from scratch, backed by real runnable examples, so a brand-new intern can become productive.

### What I did
- Read `AGENT.md`, `README.md`, `go.work`, `.ttmp.yaml`.
- Created docmgr ticket `FLOWKIT-002` with topics `documentation,go,onboarding,examples`; added vocab slugs `documentation`, `onboarding`, `examples`, `design-doc`, `reference`.
- Added the two standard docs (design-doc + reference diary) and 5 tasks.
- Mapped the directory tree (55 Go files, ~8.3k LOC excluding ttmp).
- Read the core source: `execution/{map,cache,cached_map,cached_batch_map,budget,rate,chain,reservation,resource_plan,mysql_cache}.go`, `flow/{step,policy,run,pipe,store,classify,report,bulk,batch}.go`, `internal/{digest,jsonutil,fsutil}/*.go`, the three `examples/*/main.go`, `cmd/flowkit/main.go`, `execution/example_test.go`, `boundary_test.go`, `doc_embed.go`, `docs_test.go`.
- Read the existing `docs/flowkit-developer-guide.md` and the prior `FLOWKIT-001` extraction design-doc.

### Why
Before writing any teaching material I needed to know exactly what the public
API is, what the invariants are, and what the existing docs already cover (so I
do not duplicate, and so I can fill the "from zero" gap the README leaves).

### What worked
- `go build ./...` clean; `go run ./examples/*` all run and produce the
  documented output.
- `GOWORK=off go test ./... -count=1` — every package passes (mysql tests pass
  in ~0.05s, i.e. they self-skip without a Docker daemon via testcontainers).
- The cached-step example already demonstrates the key insight: input
  `[1,2,2]` produces `work=2` on run 1 (duplicate key `2` executes once) and
  `hits=3` on run 2 (all served from the in-memory store).

### What didn't work
- Nothing failed yet. The mysql integration tests need Docker/Testcontainers to
  do real work, but they skip cleanly, so they are not a blocker for examples.

### What I learned
- The mental model is two layers: `execution` (mechanics you can adopt
  piecemeal) and `flow` (an opinionated `Step` + `Run` model that composes those
  mechanics behind a typed API). The README blurts this in two bullet points
  and then dives straight into generics + `Identity`/`Policy` — which is why a
  newcomer finds it "incomprehensible." The onboarding guide must start from
  the *problem* (expensive repeatable work over a list) and only then introduce
  the two layers.
- Critical invariants worth teaching explicitly (from source + FLOWKIT-001):
  results stay index-aligned; cache keys/envelope bytes are stable across
  versions (compat schema `rag-ttc-execution-cache/v1`); hits cost no
  admission; every fresh attempt (incl. retries) pays admission; multi-resource
  refusal rolls back earlier reservations; successful expensive work commits
  even after a sibling cancels the run (`context.WithoutCancel`); unknown
  classifier results and corrupt entries fail closed.
- The durability seam is `flow.Store` (2 methods). `FileCache` is canonical;
  `MySQLCache` and `MemoryStore` are drop-ins. This is THE extension point to
  highlight for interns.
- Existing `docs/flowkit-developer-guide.md` is already a decent reference but
  is reference-shaped (tables + short snippets), not onboarding-shaped. My new
  doc should complement it (narrative + diagrams + a full worked example), not
  replace it.

### What was tricky to build
- (Analysis, not code yet.) The trickiest concept to convey will be the
  separation of **identity** (what changes the cache key) from **policy** (how
  it runs). The README conflates them. The guide must show that
  `Workers`/`Retry`/`Admission` deliberately do NOT affect the key, which is
  what makes replay byte-exact.

### What warrants a second pair of eyes
- The claim "resume = replay, at most one in-flight item per worker lost" —
  verify against `run.go` commit-after-cancellation path
  (`context.WithoutCancel` in `success`/`work` store calls). Confirmed present
  in `flow/run.go` `success` and `execution/cached_map.go` store calls.

### What should be done in the future
- Add a `scripts/` example that actually demonstrates a crash-and-resume with a
  `FileCache` on disk (a run that stores 2 items, "crashes" by canceling, then
  re-runs and shows hits). This is the single most compelling onboarding demo
  and is not covered by the current 3 examples.

### Code review instructions
- n/a for this step (no code changed yet, only investigation + ticket setup).

### Technical details
- Module: `github.com/go-go-golems/flowkit`, Go 1.26.1, toolchain go1.26.6.
- Cache schema constant: `rag-ttc-execution-cache/v1` (`execution/cache.go`).
- `execution.Map` uses an `errgroup` with a feeder goroutine + N workers +
  ordered result slice; first error cancels pending work.
- `flow.Run` flattens a step into `stageSpec`s, builds a `stageRunner` per
  stage, and streams `erasedItem`s through channels (buffered 16 between
  stages), then a collector restores input order via `item.index`.
- Duplicate-key suppression in `flow` uses an `inflightCall` map so a key runs
  once per process; followers copy the leader's outcome.
