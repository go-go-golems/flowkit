---
Title: Consolidation and layering hardening design
Ticket: FLOWKIT-003
Status: active
Topics:
    - documentation
    - consolidation
    - testing
    - go
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://boundary_layering_test.go
      Note: New test enforcing execution/internal do not import flow (F2)
    - Path: repo://docs/guides/01-flowkit-intern-onboarding-and-implementation-guide.md
      Note: Refreshed permanent onboarding guide in docs/guides (F1)
    - Path: repo://examples/full-run/main.go
      Note: Composite runnable program replacing duplicated examples (F3)
    - Path: repo://flow/run.go
      Note: runEnv.ensure reimplements ValidateResourcePlans arithmetic (F4, deferred)
ExternalSources: []
Summary: 'Design and implementation guide for the documentation/consolidation findings raised after FLOWKIT-002: permanent guide home, enforced layering, examples consolidation, doc overlap, and a scoped plan for the execution/flow duplication.'
LastUpdated: 2026-09-01T19:40:00-04:00
WhatFor: Decide and implement the concrete fixes for the six findings, and scope the larger execution/flow consolidation as a follow-up.
WhenToUse: Before touching docs layout, example layout, boundary tests, or planning the execution/flow dedup.
---





# Consolidation and layering hardening design

## 1. Executive summary

After FLOWKIT-002 delivered the README rewrite, the verified `scripts/`
examples, and the intern onboarding guide, a review surfaced six quality
findings — some in the codebase, some in the deliverable itself. This ticket
addresses them. Three are implemented here; three are scoped as follow-ups.

The headline problem: the onboarding guide — the canonical entry point for new
developers — was stored in `ttmp/`, which `AGENT.md:22` defines as
*"temporary documentation as well as debugging logs and other reports."* A
permanent document pointing into a "temporary" directory is a structural bug.
The second problem: the one-way layering `flow -> execution -> internal` was
only half-enforced (the ragkit boundary was tested, the internal layering was
not). The third: `examples/` and `scripts/` overlapped without a crisp story.

This document records the evidence, the decisions, the implementation, and a
scoped plan for the larger `execution`/`flow` duplication that is deliberately
deferred.

## 2. Findings (evidence-backed)

### F1. The onboarding guide lived in the wrong place

- **Claim:** the FLOWKIT-002 onboarding guide was stored in
  `ttmp/2026/09/01/FLOWKIT-002--.../design-doc/`, and the README linked there.
- **Evidence:** `AGENT.md:22` defines `ttmp/YYYY-MM-DD/` as "temporary
  documentation as well as debugging logs and other reports." `README.md:156`
  (pre-fix) linked `./ttmp/2026/09/01/FLOWKIT-002--.../design-doc/01-...md`.
- **Impact:** the canonical entry point for new developers points into a
  directory the project itself labels temporary. Long-form guides had no
  permanent home.

### F2. The layering boundary was only half-tested

- **Claim:** only the ragkit boundary was enforced; `execution`/`internal`
  not importing `flow` was not.
- **Evidence:** `boundary_test.go:15` asserts no package imports
  `github.com/go-go-golems/ragkit`. There was no test asserting `execution` or
  `internal/*` do not import `flowkit/flow`. Verified by
  `rg "go-go-golems/flowkit/flow" execution/ internal/` → none today, but
  nothing failed if someone added one.
- **Impact:** a refactor could silently invert `flow -> execution -> internal`
  and only be caught at review.

### F3. `examples/` and `scripts/` duplicated each other

- **Claim:** the three `examples/` programs were fully duplicated by `scripts/`
  Example tests.
- **Evidence:** `examples/bounded-map/main.go` squares ints with
  `Workers: 2` — identical to `scripts/ExampleMap`.
  `examples/cached-step/main.go` doubles `[1,2,2]` against a `MemoryStore` —
  identical to `scripts/ExampleStep_cached`. `examples/pipeline/main.go`
  composes double→format — identical to `scripts/ExamplePipe2`.
- **Impact:** a newcomer had to guess which of two homes to read, and the two
  homes said the same things.

### F4. Real duplication between `execution` and `flow` (deferred)

- **Claim:** `flow` reimplements cache/preflight logic that `execution`
  already provides, rather than building on it.
- **Evidence:** `flow` does not call `execution.MapCached`,
  `MapCachedBatches`, `ValidateResourcePlans`, or `NewResourceBudgets` as
  implementations — the only references are *tests* proving shared cache
  entries (`flow/run_test.go:724`, `flow/bulk_test.go:319`). `runEnv.ensure`
  (`flow/run.go:149`) reimplements the ceiling-coverage and cost-summation
  arithmetic of `execution.ValidateResourcePlans` (`resource_plan.go:26`).
  `flow.Resource` and `execution.ResourcePlan` are near-identical structs
  bridged by a copy method `plan()` (`flow/policy.go:30`). `runBulk`/
  `runBatched`/`typedRunner` each reimplement the group-by-digest → load-hits
  → fan-out → store flow that `MapCached`/`MapCachedBatches` already encode.
- **Context:** FLOWKIT-001 explicitly deferred "merging `flow.Resource` with
  `execution.ResourcePlan`" to keep the first extraction behavior-preserving.
- **Impact:** three parallel code paths must be kept behaviorally in sync by
  hand.

### F5. Doc overlap: onboarding guide vs developer guide

- **Claim:** the new onboarding guide and the existing
  `docs/flowkit-developer-guide.md` overlap (invariants, API chooser table,
  troubleshooting).
- **Evidence:** both contain an "API chooser" table and a "critical
  invariants" list. The developer guide is the Glazed help entry
  (`docs_test.go` asserts frontmatter/slug); the onboarding guide is
  narrative.
- **Impact:** a reader must decide which to open; the two can drift.

### F6. Minor / edge findings

- `Step` carries hidden state (`override`, `stages`, `extraPlans`,
  `extraPolicies`) set by `Bulk`/`Batched`/`Pipe*` — subtle for callers
  copying a `Step{}` literal.
- `CachePending` naming could read as "still running on disk" in a cache
  context (never persisted).
- `Meter` vs `AttemptMeter` "not both" is a runtime check (`newTypedRunner`),
  not a type-level one.
- `MySQLCache` (the largest, most operationally surprising file) has no
  operator runbook.
- Guide line anchors are point-in-time; a reformat drifts them silently.

## 3. Decisions

### Decision: permanent long-form guides live in `docs/guides/`

- **Context:** `ttmp/` is temporary; `docs/` is `go:embed`ded as Glazed help
  entries with a frontmatter/slug contract that long narratives don't fit.
- **Options considered:** (a) put guides in `docs/` alongside help entries;
  (b) create `docs/guides/` for long-form narratives, keep `docs/*.md` for
  Glazed help entries; (c) keep guides in `ttmp/` and relabel `ttmp/`.
- **Decision:** (b) — `docs/guides/` for permanent long-form guides; `docs/*.md`
  stays the Glazed help surface.
- **Rationale:** keeps the Glazed embed contract (`doc_embed.go:15`,
  `docs_test.go`) intact, gives guides a permanent, browsable home, and matches
  the existing `docs/` convention without overloading it.
- **Consequences:** the onboarding guide moves to `docs/guides/`; README and
  `scripts/README` link there. FLOWKIT-001's guide has the same problem and is
  a follow-up (moving it risks touching its closed ticket's index).
- **Status:** accepted & implemented (F1).

### Decision: enforce `flow -> execution -> internal` with a test

- **Context:** review-only boundaries rot.
- **Options considered:** (a) rely on review; (b) add a `go list -deps` test
  asserting `execution` and `internal/*` do not import `flowkit/flow`.
- **Decision:** (b).
- **Rationale:** the existing ragkit boundary test already proved the
  pattern works; mirroring it for the internal layering is cheap and makes the
  architecture diagram executable.
- **Consequences:** `boundary_layering_test.go` added; must stay green. If
  `execution` ever legitimately needs a `flow` type, that's a layering
  inversion to fix, not a test to relax.
- **Status:** accepted & implemented (F2).

### Decision: two example homes with disjoint purpose

- **Context:** `examples/` and `scripts/` duplicated each other.
- **Options considered:** (a) delete `examples/`, keep only `scripts/`; (b)
  keep `examples/` as tiny programs and `scripts/` as tests, accept overlap;
  (c) `examples/` = one composite runnable program, `scripts/` = exhaustive
  verified per-API reference, no overlap.
- **Decision:** (c).
- **Rationale:** (a) loses the valuable `go run` experience; (b) keeps
  duplication. (c) gives each home a unique purpose: `examples/full-run` shows
  how the pieces compose as a program; `scripts/` proves every documented
  behavior holds as a test. They do not overlap because the composite program
  is not an `Example` test and the `Example` tests are not a program.
- **Consequences:** delete `examples/{bounded-map,cached-step,pipeline}`;
  add `examples/full-run`; update README + developer guide links; add
  `examples/README.md` documenting the two homes.
- **Status:** accepted & implemented (F3).

### Decision: defer execution/flow dedup, scope it as a follow-up

- **Context:** F4 is a real refactor, not a doc fix; doing it inside a
  documentation ticket risks scope creep and behavior changes.
- **Options considered:** (a) do the dedup now; (b) defer with a scoped plan.
- **Decision:** (b) — document the dedup targets and acceptance criteria here,
  implement in a separate ticket.
- **Rationale:** the first extraction was deliberately behavior-preserving;
  the dedup should be its own behavior-preserving (then simplifying) change
  with its own tests and review.
- **Consequences:** F4 remains open; the onboarding guide flags it (§4.5, §11)
  so it is not forgotten.
- **Status:** proposed (follow-up ticket).

### Decision: split onboarding guide (narrative) from developer guide (reference)

- **Context:** F5 overlap.
- **Options considered:** (a) merge into one doc; (b) keep both, make
  onboarding guide the canonical *learning* doc and trim the developer guide
  to a pure API reference + troubleshooting.
- **Decision:** (b), applied lightly in this ticket: the onboarding guide now
  owns the narrative, invariants, decision records, and onboarding plan; the
  developer guide keeps the chooser table + troubleshooting (duplicated
  intentionally as a quick-reference). A deeper trim is a follow-up to avoid
  churning the Glazed help surface.
- **Rationale:** the developer guide is embedded in the CLI (`flowkit help`)
  and must stay self-contained; the onboarding guide is for reading cover to
  cover.
- **Consequences:** minor intentional duplication remains; a future ticket can
  trim the developer guide's invariants section.
- **Status:** accepted (light); deeper trim proposed (follow-up).

## 4. Implementation (this ticket)

### 4.1 F2 — layering boundary test (DONE)

`boundary_layering_test.go` at the repo root adds
`TestExecutionAndInternalDoNotImportFlow`, which runs `go list -deps` for
`./execution/...` and `./internal/...` and fails if any dependency is
`github.com/go-go-golems/flowkit/flow`. Verified passing:
`go test -run TestExecutionAndInternalDoNotImportFlow .` → PASS.

### 4.2 F3 — examples consolidation (DONE)

- Removed `examples/bounded-map`, `examples/cached-step`, `examples/pipeline`
  (fully duplicated by `scripts/`).
- Added `examples/full-run/main.go`: one complete program — a cached,
  retryable, budgeted `flow.Step` over an on-disk `execution.FileCache`, run
  twice (replay), plus a quarantine demo. Uses `&flow.StatusError{Status:503}`
  so the retry is deterministic and classified `Transient` by
  `DefaultClassifier`. Verified: `go run ./examples/full-run` produces the
  documented output (5 fresh → 5 hits → 1 quarantine).
- Added `examples/README.md` documenting the two homes.
- Updated `README.md` and `docs/flowkit-developer-guide.md` links.
- `golangci-lint` clean; `go test ./...` green.

### 4.3 F1 — permanent guide home (DONE)

- Created `docs/guides/`.
- Wrote the refreshed onboarding guide at
  `docs/guides/01-flowkit-intern-onboarding-and-implementation-guide.md`
  (supersedes the FLOWKIT-002 `ttmp/` copy, which stays as the historical
  record in the closed ticket).
- Updated `README.md` and `scripts/README.md` to link to the new permanent
  path. The guide now documents the enforced layering (§3.4), the two example
  homes (§3.3, §6), the `*StatusError` pitfall (§5.4), and the deferred
  execution/flow dedup (§4.5, §11).

### 4.4 Validation

- `GOWORK=off go test ./... -count=1` → all green (incl. new boundary test).
- `GOWORK=off golangci-lint run` → 0 issues.
- `go run ./examples/full-run` → documented output.
- `docmgr doctor --ticket FLOWKIT-003` → all checks passed (run at close).

## 5. Scoped follow-up: execution/flow dedup (F4)

This is proposed, not implemented. It should be its own ticket.

### 5.1 Targets

1. **Preflight:** make `runEnv.ensure` delegate to
   `execution.ValidateResourcePlans` (or share a single validation function)
   instead of reimplementing ceiling-coverage and cost arithmetic. Keep the
   `runEnv`-specific behavior (per-resource budgets, shared declarations,
   reference-vs-declaration rule) but factor the pure arithmetic out.
2. **Resource types:** merge `flow.Resource` and `execution.ResourcePlan`
   into one type (or make `flow.Resource` a thin wrapper with no copied
   fields), removing the `plan()` copy method.
3. **Cache-aware execution in flow:** evaluate having `runBulk`/`runBatched`
   build on `execution.MapCached`/`MapCachedBatches` for the group→load→
   fan-out→store flow, keeping `flow`'s retry/admission/failure-policy
   additions as the differentiating layer. This is the riskiest target
   because `flow` adds in-flight dedup, per-item failure policy, metering, and
   ledger events that `MapCached` does not have.

### 5.2 Acceptance criteria

- Behavior-preserving: all existing `flow/*_test.go` and `execution/*_test.go`
  pass unchanged, including `TestRunFlowStoreInteroperatesWithMapCached` and
  `TestBulkInteroperatesWithMapCachedBatchesEntries` (the shared-cache
  contracts).
- `boundary_layering_test.go` stays green (the dedup must not invert the
  layering — `flow` may call `execution`, not vice versa).
- Race tests pass: `GOWORK=off go test -race ./... -count=1`.
- The onboarding guide's "Known consolidation opportunity" note (§4.5) is
  removed once the dedup lands.

### 5.3 Risks

- `flow`'s `runEnv` owns *shared* budgets across stages and sub-runs
  (`Options.Share()`), which `execution`'s standalone APIs do not. The dedup
  must preserve the sharing semantics, not just the per-call path.
- `flow`'s in-flight dedup (`inflightCall`) and commit-after-cancel
  (`context.WithoutCancel`) are stricter than `MapCached`'s; merging must not
  weaken them.
- This is exactly the kind of change FLOWKIT-001 deferred to avoid muddying
  the extraction; it deserves its own focused PR.

## 6. Other follow-ups (minor, F6)

- Add a `testing.Short()`-guarded `MySQLCache` example in `scripts/` so the
  MySQL path has a hermetic runnable demo without forcing Docker.
- Add a `MySQLCache` operator runbook (corrupt-row diagnosis, table recreate,
  `GET_LOCK` migration behavior under concurrent processes).
- Consider a CI check that verifies the onboarding guide's line anchors
  still point at the cited symbols (catches drift after reformatting).
- Optionally trim `docs/flowkit-developer-guide.md`'s duplicated invariants
  section once the onboarding guide is established as canonical.
- Move FLOWKIT-001's extraction guide to `docs/guides/` too (same F1 problem),
  updating its ticket index and the README link.

## 7. References

- `AGENT.md:22` — `ttmp/` is temporary.
- `boundary_test.go:15` — ragkit boundary; `boundary_layering_test.go` —
  execution/internal↛flow boundary (new).
- `doc_embed.go:15`, `docs_test.go` — `docs/` is the Glazed help surface.
- `flow/run.go:149` — `runEnv.ensure`; `execution/resource_plan.go:26` —
  `ValidateResourcePlans`; `flow/policy.go:30` — `Resource.plan()` copy.
- `flow/run_test.go:724`, `flow/bulk_test.go:319` — shared-cache interop tests.
- `docs/guides/01-flowkit-intern-onboarding-and-implementation-guide.md` —
  the refreshed permanent guide (this ticket's other deliverable).
