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
    - Path: repo://scripts/README.md
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/bulk_batch_examples_test.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/doc.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/execution_examples_test.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/observe_examples_test.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/pipeline_examples_test.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/policy_examples_test.go
      Note: Runnable verified example for the onboarding guide
    - Path: repo://scripts/step_examples_test.go
      Note: Runnable verified example for the onboarding guide
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

## Step 2: Write runnable, verified examples in scripts/

I created a new top-level `scripts/` package as **tested documentation**: Go
`Example` functions with `// Output:` blocks, so `go test ./scripts/ -v` both
documents and verifies the API. 24 examples across 6 files cover the whole
public surface.

### Prompt Context

**User prompt (verbatim):** (see Step 1) — "...write examples tests in the scripts/ folder..."

**Assistant interpretation:** Build a `scripts/` directory of examples that are also tests, covering every core API, so an intern can read and run them.

**Inferred user intent:** Examples must be real (runnable, verified) and pedagogical — each one a self-contained copy-paste recipe.

### What I did
- Created `scripts/doc.go` (package doc) + 6 `*_examples_test.go` files + `helpers_test.go` + `README.md`.
- Coverage: `execution.Map`, `Chain(budget,rate)`, `MapCached` (+ a resume-after-"crash" demo), `FileCache` fail-closed corruption; `flow.Step` cached/uncached/dedup, retry+classifier, `AsDataError` quarantine; `FailureMode` fail-fast/skip, admission spend, `Preflight` monetary gate, `Options.Share()`, rate-after-budget; `Pipe2`/`Pipe3`, quarantine-bypass; `Bulk`, `Batched` (group+repair); `Meter`/`AttemptMeter`, `Ledger`, `OnResult`, custom `Store` swap.
- Verified: `go test ./scripts/ -v` → 24/24 PASS; full `GOWORK=off go test ./...` green; `gofmt` clean.

### Why
The existing `examples/*/main.go` are tiny `func main()` programs with no
assertions — fine to run but they cannot prove the documented behavior holds.
`Example` functions with `// Output:` are the Go-idiomatic way to make examples
that are both readable and continuously verified, which is exactly what an
intern (and CI) needs.

### What worked
- All examples pass first try (after two fixes, below). The resume example
  (`ExampleMapCached_resume`) is the most compelling onboarding demo: run 1
  stores 3 items, then a "new process" re-runs `[1,2,3,4]` and reports
  `hits=3 misses=1 work=1` — concrete proof of "resume = replay".

### What didn't work
- `ExampleFileCache_corruptionFailsClosed` initially asserted `err == ErrCorruptCache`.
  `FileCache.Load` wraps the error with `%w`, so direct `==` is false. Fixed
  with `errors.Is(err, execution.ErrCorruptCache)`.
- `ExampleBatched` initially omitted position 1 of *every* group (used
  `if i == 1`), forcing 2 repairs instead of 1. Fixed by keying the omission on
  the actual content (`if g[i] == "b"`) so only the first group is short.
- Go's example-name validator (`go vet`) rejected `ExampleMeter` and
  `ExampleOnResult` with "refers to unknown identifier": the prefix before the
  first `_` must be a *top-level* identifier in an imported package, but
  `Meter`/`OnResult` are struct *fields* of `flow.Step`. Renamed to
  `ExampleMeters` (matches `flow.Meters` type) and `ExampleStep_onResult`
  (`Step` is top-level, `onResult` is a freeform label).

### What I learned
- The example-naming rule (the hard way): `ExampleFoo` requires `Foo` to be a
  top-level identifier in an imported package; `ExampleType_label` requires
  `Type` to be top-level (the `label` after `_` is freeform). Field names are
  not accepted as the leading component. This is why `ExampleStep_cached`,
  `ExampleMap`, `ExampleBulk` worked but `ExampleMeter`/`ExampleOnResult` did
  not.
- `Budget.Snapshot()` prints as `{limit spent remaining}` (struct literal
  form), which is handy for example outputs.
- `flow.Options{}.Share()` is idempotent and the returned value must be reused
  (not the original) for budgets to be shared across `Run` calls — the shared
  env lives on the returned copy.

### What was tricky to build
- Making the `Batched` example deterministic and understandable. The
  semantics are subtle: `Group` returns index groups, `DoAll` makes one call
  per group returning a position-keyed response, `Split` parses it, and any
  missing/unparseable/failed member is routed to the `repair` step. I had to
  deliberately manufacture exactly one missing member to show the repair path
  without the example becoming noise.
- Keeping example outputs deterministic under concurrency. I used
  `Workers: 1` and input order for the `OnResult` streaming example so the
  completion order matches input order; other examples print only
  order-independent aggregates (counts, sums) so `Workers: 2` is safe.

### What warrants a second pair of eyes
- The `ExamplePolicy_sharedBudgets` comment claims the second run's items are
  "cache misses under a different key" — true because keys `[3]`/`[4]` differ
  from `[1]`/`[2]`, so they draw from the shared budget. If someone later
  reuses items across the two runs the budget arithmetic would change and the
  Output would break. The intent is documented in the comment.
- All example outputs depend on documented invariants (order alignment,
  hits-are-free, duplicate suppression). If those invariants ever change, the
  examples will fail loudly — which is the point.

### What should be done in the future
- Add an example wiring a real `*sql.DB`-backed `MySQLCache` (guarded by a
  build tag or `testing.Short()` skip) so the MySQL path has a runnable demo
  without forcing Docker on every contributor. Left out now to keep `scripts/`
  hermetic.

### Code review instructions
- Start at `scripts/doc.go` for the map; run `go test ./scripts/ -v` to see
  every example execute.
- Validate: `GOWORK=off go test -race ./scripts/ -count=1` (concurrency paths)
  and `gofmt -l scripts/` (clean).

### Technical details
- Package: `examples_test` (external test package), so examples read like a
  caller would write them (qualified `flow.`/`execution.`).
- `Example*` with `// Output:` are matched by `go test` and shown under `go
  test -v`; they also appear in `go doc`/pkg.go.dev as runnable examples.

## Step 3: Rewrite README and write the intern onboarding design-doc

I rewrote `README.md` to lead with the *problem* and a two-layer mental model,
then wrote the full intern onboarding design-doc with prose, ASCII diagrams,
pseudocode, decision records, line-anchored file references, and a phased
onboarding plan.

### Prompt Context

**User prompt (verbatim):** (see Step 1) — "...imagine you are a new developer joining the project and you have no iea what it is about... Create a detailed analysis / design / implementation guide that is for a new intern... Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Rewrite the README to be comprehensible from zero, then write a thorough intern onboarding design-doc and store it in the ticket.

**Inferred user intent:** A new intern with no context must be able to read the README + design-doc and understand what Flowkit is and how to use it.

### What I did
- Rewrote `README.md`: problem-first intro, "what it is not", two-layer ASCII diagram, 30-second tour, run-the-examples, the identity/policy invariant, an API chooser table, and links to the new onboarding guide + developer guide + examples.
- Wrote the design-doc (`design-doc/01-flowkit-intern-onboarding-and-implementation-guide.md`, ~40KB): executive summary, problem, glossary, two-layer architecture + non-goals, package map, full `execution` layer walkthrough (Map diagram, limiters/budgets/rates, Key+FileCache+MySQLCache, MapCached flow, preflight), full `flow` layer (Step, Run engine with pseudocode, Options/runEnv, classification, failure modes, pipelines diagram, Bulk, Batched, observation, Store seam), a worked example, the 9 critical invariants, 5 decision records, an 11-step phased onboarding plan, test strategy, risks, and line-anchored references.
- Gathered line anchors via grep for every key symbol to cite precisely.

### Why
The old README opened with generics and `Identity`/`Policy` and never said what
problem the library solves. The design-doc is the "from zero" narrative the
README links to; the README is the "30 seconds to decide this is relevant"
page. Together they cover both the browse-path and the study-path reader.

### What worked
- `go build ./...` and `GOWORK=off go test ./...` still green after the README
  rewrite (README has no code, but the design-doc links to `scripts/` examples
  that are verified).
- Line anchors were gathered programmatically and cross-checked against the
  source, so the references in the design-doc are real, not invented.

### What didn't work
- Nothing failed in this step.

### What I learned
- The `runEnv.ensure` preflight has a subtle "reference vs declaration" rule:
  a zero-valued `Resource{Name:...}` reuses an earlier full declaration, but a
  zero resource with NO earlier declaration becomes a zero plan that admits
  nothing and fails loudly on first spend. This is a footgun worth teaching
  explicitly (documented in §5.3 and the risks).
- `flow` exposes `Options.Declare/Limiter/Snapshots/Cost` specifically so
  non-step consumers (legacy per-call caches) can draw from shared budgets —
  a detail easy to miss when reading only `Run`.

### What was tricky to build
- Keeping the design-doc's pseudocode honest. I wrote `process`/`lead`/`work`/
  `success`/`fail` pseudocode from `flow/run.go` and then re-checked each
  branch against the actual source (e.g. that `AttemptMeter` runs on every
  attempt including errors, that `Meter` runs only on success, that store
  uses `context.WithoutCancel`). The pseudocode is a faithful summary, not a
  re-implementation.
- Choosing what to leave to the existing `docs/flowkit-developer-guide.md`
  (reference tables) vs. the new doc (narrative + diagrams + onboarding plan).
  They are complementary, not duplicative.

### What warrants a second pair of eyes
- The line anchors in §12 are point-in-time; if the source is reformatted they
  drift. They are accurate as of commit (this step). A future CI job could
  check them, but that is out of scope.
- The "at most one in-flight item per worker lost" claim is the central
  resume guarantee; it rests on the `context.WithoutCancel` store calls.
  Confirmed in `flow/run.go` `success`, `execution/cached_map.go`, and
  `flow/bulk.go` store calls.

### What should be done in the future
- Add a `go test ./scripts/` step to CI (it currently runs the whole suite, so
  it is covered, but an explicit note would help).
- Consider a `testing.Short()`-guarded `MySQLCache` example.

### Code review instructions
- Read `README.md` top-to-bottom; click through to the design-doc.
- Read the design-doc §4–§5 alongside the cited source lines; spot-check 3–4
  line anchors.
- Run `go test ./scripts/ -v` to confirm the examples the doc references
  still pass.

### Technical details
- Design-doc frontmatter follows the docmgr design-doc template (Topics,
  DocType, Summary, WhatFor, WhenToUse).
- README links use relative paths into `ttmp/.../design-doc/...` so the bundle
  upload and local browsing resolve the same target.
