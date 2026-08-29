---
Title: Investigation Diary
Ticket: FLOWKIT-002
Status: active
Topics:
    - architecture
    - backend
    - observability
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/workspaces/2026-08-24/use-optkit/flowkit/ttmp/2026/08/28/FLOWKIT-002--live-execution-observation-seam-for-long-running-flows/design-doc/01-intern-guide-to-live-flow-observation-progress-aggregation-and-safe-event-delivery.md
      Note: Primary architecture and implementation guide produced by this investigation
ExternalSources: []
Summary: Chronological evidence and review notes for designing Flowkit live execution observation.
LastUpdated: 2026-08-28T18:50:00-04:00
WhatFor: Resume or review the FLOWKIT-002 design investigation.
WhenToUse: Read before implementing or reviewing the observer/reporter contracts.
---


# Diary

## Goal

Record how the live execution observation design was derived from Flowkit's actual runner, report, event, and test contracts.

## Step 1: Map the existing observation machinery and design the extension

The investigation began with an important correction: Flowkit is not unobservable. It already has locked live reports, periodic progress logging, exact item events, and a fail-closed ledger. The missing piece is a supported application reporter for immutable periodic snapshots and lifecycle boundaries.

The design therefore extends the existing model instead of introducing a competing event bus. It separates exact custody events from sampled aggregate progress and preserves scalar, Bulk, Batched, and pipeline semantics.

### Prompt Context

**User prompt (verbatim):** "Ok, let's create the tickets all the way up to F2 (included). For each ticket, Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Create the B1 ticket and an evidence-backed intern implementation guide, with full docmgr and delivery bookkeeping.

**Inferred user intent:** Establish a reliable backend foundation before building product-specific progress APIs and UI.

### What I did
- Read `AGENT.md`.
- Inspected `flow/report.go`, `step.go`, `policy.go`, `run.go`, `bulk.go`, `batch.go`, and their tests.
- Identified existing `Event`, `Ledger`, `Report`, `StepReport`, `OnResult`, and `logProgress` behavior.
- Designed immutable snapshots, lifecycle events, reporter error semantics, pipeline parity, and race tests.
- Created FLOWKIT-002, its task list, and the intern guide.

### Why
- The downstream build UI must reflect canonical engine counters rather than parse logs.
- Provider-backed runs need fail-closed custody and reconnect-safe history.

### What worked
- Current runner locks already provide race-safe report reads.
- Existing ledger tests provide a strong extension point.
- The final design avoids a scheduler or domain payloads in Flowkit.

### What didn't work
- An initial subagent research workflow was started, then the user said `don't use subagents`. It was stopped immediately and no subagent output was used in this guide.

### What I learned
- `OnResult` is intentionally narrower than generic progress.
- Exact events and sampled snapshots solve different problems and should remain separate.
- Nested Bulk/Batched execution is the highest-risk parity area.

### What was tricky to build
- The subtle part was avoiding duplicate observation systems. The source already contains a ledger, so the solution is a reporter layered beside it, with deep-cloned report maps and explicit lifecycle semantics.

### What warrants a second pair of eyes
- Reporter failure/cancellation behavior.
- Nested execution double-counting.
- Lifecycle event ordering under concurrent workers.
- Whether event error strings require a stronger redaction contract.

### What should be done in the future
- Implement phases 1–5 in the guide and release Flowkit before RAG-TTC-WORK-001 consumes it.

### Code review instructions
- Start at `flow/report.go` and `flow/run.go`, then read `flow/run_test.go` and `flow/bulk_test.go`.
- Validate with `go test -race ./flow -count=1` and `go test ./... -count=1`.

### Technical details
- The guide defines `Snapshot`, `Reporter`, deep cloning, lifecycle events, and terminal parity requirements.
- No production code changed during this analysis ticket.


## Step 2: Validate documentation and deliver it to reMarkable

The guide and diary passed frontmatter validation and a ticket-scoped `docmgr doctor` with all checks passing. The two documents were rendered together as one PDF with a table of contents, uploaded after a successful dry run, and verified in the reMarkable cloud listing.

No production implementation was attempted: the unchecked ticket tasks remain the implementation plan. This step closes the requested research/design delivery while leaving technical decisions marked proposed for engineering review.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Validate and deliver the completed FLOWKIT-002 intern package.

**Inferred user intent:** Make the guide immediately readable on reMarkable and leave an auditable ticket record.

### What I did
- Ran `docmgr validate frontmatter --doc <guide-or-diary> --suggest-fixes`.
- Ran `docmgr doctor --ticket FLOWKIT-002 --stale-after 30`; all checks passed.
- Ran `remarquee upload bundle --dry-run ... --remote-dir /ai/2026/08/28/FLOWKIT-002 --toc-depth 2`.
- Uploaded `FLOWKIT-002 Live Execution Observation.pdf`.
- Verified it with `remarquee cloud ls /ai/2026/08/28/FLOWKIT-002 --long --non-interactive`.

### Why
- Ticket documentation must be structurally valid and available in the requested reading environment before handoff.

### What worked
- Frontmatter and doctor validation passed.
- PDF rendering, upload, and remote listing succeeded.

### What didn't work
- `docmgr doc relate` initially normalized absolute paths to workspace `repo://...` aliases that this checkout's doctor reported as `missing_related_file`. The new-ticket frontmatter was corrected back to absolute paths. FLOWKIT-002 also inherited an unavailable `go` topic from the shared workspace vocabulary and was changed to the valid `backend` topic.

### What I learned
- This multi-repository workspace's docmgr root and alias resolution differ from a standalone repository; absolute file relations are the reliable contract here.

### What was tricky to build
- The uploaded PDF needed to contain both the implementation guide and chronological diary while preserving clear heading hierarchy and a useful table of contents.

### What warrants a second pair of eyes
- Review all proposed decision records before implementation.
- Confirm ticket boundaries still match team ownership when work is scheduled.

### What should be done in the future
- Execute the unchecked implementation tasks in dependency order and update this diary with code commits, failures, and validation evidence.

### Code review instructions
- Begin with the primary guide's executive summary, decisions, implementation phases, and definition of done.
- Re-run ticket-scoped doctor before merging documentation changes.

### Technical details
- Remote destination: `/ai/2026/08/28/FLOWKIT-002`.
- Verified document: `FLOWKIT-002 Live Execution Observation`.
- Repository: `flowkit`.

## Step 3: Define immutable observation contracts

The first implementation phase added the public data contracts needed by downstream run recorders without yet changing execution behavior. Reports now support defensive deep cloning, aggregate snapshots have explicit sequence/time/terminal metadata, and callers can configure a fail-closed `Reporter`, interval, and deterministic clock.

The existing exact `Ledger` vocabulary was extended with run and step lifecycle event types instead of creating a second event bus. Existing item-event JSON semantics were preserved, including index zero.

### Prompt Context

**User prompt (verbatim):** "work on FLOWKIT-002, commit at appropriate intervals and keep a detailed diary as you work (using the diary format from the skill) Print out a brutalist work slip with the plan / different phases for the ticket. then before stsarting a phase, plrint a split about the phase, and print one when the phase is done.\n\n---\n\nThen work on RAG-TTC-WORK-001 and RAG-TTC-WORK-002."

**Assistant interpretation:** Implement FLOWKIT-002 in phased, tested commits with printed phase boundaries and strict diary entries, then implement its two RAG-TTC consumers.

**Inferred user intent:** Produce a reliable backend chain with a physical execution trail and reviewable Git history.

**Commit (code):** 29847b6e40aef1515dcaf84684b21a00245d0ab8 — "FLOWKIT-002: define live progress contracts"

### What I did
- Added `Report.Clone` and `StepReport.Clone` with deep copies of all maps.
- Added immutable `Snapshot`, `Reporter`, and `ReporterFunc` contracts.
- Added `Options.Reporter`, `ReportInterval`, and injectable `Clock`.
- Added run/step lifecycle `EventType` values and event timestamps/total.
- Added report clone and reporter adapter tests.
- Ran `gofmt` and `go test ./flow -count=1`.

### Why
- Retained progress snapshots must never alias counters still being mutated by workers.
- RAG-TTC needs stable timestamps, terminal markers, and a supported sink rather than log parsing.

### What worked
- Existing types accepted the extension without changing current execution behavior.
- The focused Flow package tests passed.

### What didn't work
- N/A

### What I learned
- `Event.Index` must not use `omitempty`: item zero is a real event identity.
- The contract can remain opt-in, preserving zero-cost behavior for callers without a reporter.

### What was tricky to build
- Deep cloning required copying four map layers: steps, retry classes, spend snapshots, and meters. A shallow struct copy would have exposed live mutable state to reporters.

### What warrants a second pair of eyes
- Whether public lifecycle event names and fields are sufficient before release.
- Whether `Clock` should be a function or a small interface.

### What should be done in the future
- Wire reporting and lifecycle emission into scalar, pipeline, and bulk execution with terminal parity.

### Code review instructions
- Start at `flow/report.go`, then `Options` in `flow/run.go` and `flow/report_test.go`.
- Validate with `go test ./flow -count=1`.

### Technical details
- This phase intentionally did not invoke reporters or emit lifecycle events yet.
- Reporter errors are documented as fatal; runtime enforcement belongs to Phase 2.

## Step 4: Wire live reporting through scalar, pipeline, bulk, and nested execution

The runtime phase now emits one root run lifecycle, step boundaries, initial and terminal snapshots, and optional periodic snapshots. Reporter failure cancels active execution and is returned as the primary observability error; terminal delivery gets a bounded cancellation-independent context so a canceled run can still record its last state.

Bulk execution uses the same periodic helper and exact ledger timestamping as scalar execution. Batched nested runs share observation sequence/start time and do not manufacture extra root-run boundaries, while their group and repair steps remain visible.

### Prompt Context

**User prompt (verbatim):** (same as Step 3)

**Assistant interpretation:** Complete Phase 2 runner integration across all execution shapes and commit it independently.

**Inferred user intent:** Ensure downstream durable progress is canonical, fail-closed, and not limited to the simplest Flowkit path.

**Commit (code):** 73e447bfae11a52d49c241f4d9e67ece760ae8ce — "FLOWKIT-002: publish live run progress"

### What I did
- Added `flow/observe.go` with timestamping, shared snapshot sequences, bounded terminal reporting, ledger emission, and periodic reporter cancellation.
- Refactored `Run` into validation/lifecycle plus `runCore`.
- Added root `run_started` and exactly one terminal run event.
- Added stage start/completion events around pipeline and override execution.
- Replaced scalar and Bulk log-only observation with optional periodic reporter delivery while retaining logs.
- Timestamped every exact ledger event through one helper.
- Shared observation state through Batched nested group/repair runs.
- Added scalar, periodic, reporter-failure, lifecycle, Bulk parity, and Batched lifecycle tests.
- Ran `go test ./flow -count=1` and `go test ./... -count=1`.

### Why
- RAG-TTC needs progress during provider work, not only the terminal report.
- A configured durable reporter must stop spending if its custody record cannot be written.

### What worked
- All repository tests passed.
- Existing cache, retry, budget, quarantine, and ordering tests remained green.
- Nested Batched execution emitted one root lifecycle while retaining step visibility.

### What didn't work
- The first `gofmt` attempt failed with:
  `flow/observe.go:115:3: expected ';', found '('`
  and
  `flow/observe.go:122:2: expression in go must be function call`.
  The reporter goroutine was missing one closing brace around its `select`/`for`; the block was corrected and tests then passed.

### What I learned
- Reporter cancellation must propagate into the worker context; merely checking an error after provider work defeats fail-closed observation.
- Root lifecycle and nested step visibility require shared observation state but distinct boundary ownership.

### What was tricky to build
- `Batched` invokes nested `Run` calls for group and repair work. Without a shared observation pointer, each nested run would emit a false root lifecycle and restart snapshot sequence numbering. Sharing state through the copied `Options` preserves one root identity.
- Bulk has its own execution engine and progress logger, so scalar-only changes would have left embeddings—the primary consumer—unobserved.

### What warrants a second pair of eyes
- Whether periodic snapshots from nested Batched runs should be documented as partial step reports until the terminal full report.
- Terminal timeout duration and error joining.
- Whether lifecycle ledger implementations need an explicit serialization wrapper beyond their documented thread-safety responsibility.

### What should be done in the future
- Run race tests, add public package documentation/examples, and complete the no-reporter compatibility and release validation phase.

### Code review instructions
- Start at `flow/observe.go`, then `Run`/`runStages` in `flow/run.go`, then `runBulk`.
- Review `flow/observe_test.go` for the contract and nested lifecycle expectations.
- Validate with `go test ./... -count=1` and `go test -race ./flow -count=1`.

### Technical details
- Periodic snapshots are disabled when `ReportInterval <= 0`; initial and terminal snapshots still publish.
- Existing thirty-second progress logs remain independent of reporter sampling.
- Terminal snapshots and events use a five-second bounded `context.WithoutCancel` context.

## Step 5: Harden compatibility, document the API, and validate the repository

The final local implementation phase proved the observation path under repeated race testing and documented it for consumers. No-observer calls now avoid initializing observation state entirely, preserving the existing execution path's overhead and behavior. Public package and developer documentation explain exact ledger events, immutable sampled snapshots, fail-closed sink behavior, nested sequencing, and pipeline overlap.

A runnable progress-reporter example demonstrates initial, periodic, and terminal JSON snapshots. The full repository quality gate passed, including lint, log generation checks, tests, generation, and build.

### Prompt Context

**User prompt (verbatim):** (same as Step 3)

**Assistant interpretation:** Complete FLOWKIT-002's local implementation, compatibility proof, documentation, and validation before moving to RAG-TTC.

**Inferred user intent:** Leave a release-ready dependency with strong evidence rather than immediately layering product code over an unstable API.

**Commit (code):** 79e586cc7371769ff2afb1b00efbd4e573bf52fc — "FLOWKIT-002: document and harden progress reporting"

### What I did
- Avoided allocating observation state when neither Ledger nor Reporter is configured.
- Clarified that streaming step-start means the runner is ready, not that an item has arrived.
- Added a no-observer compatibility test.
- Expanded package docs, README, and developer guide.
- Added `examples/progress-reporter`.
- Ran `go test -race ./flow -count=10`.
- Ran `go test ./... -count=1`, `go test -race ./... -count=1`, and `go vet ./...`.
- Ran `go run ./examples/progress-reporter` and inspected the snapshot sequence.
- Ran `make ci-check`; lint, logcopter, tests, generation, and build passed.

### Why
- A reusable concurrency API needs race evidence and an executable example before downstream adoption.
- Opt-in observability should impose no observation-state allocation on unchanged callers.

### What worked
- Ten repeated race runs of the Flow package passed.
- Full race, vet, and CI checks passed.
- The example emitted sequences 1–5 with initial, periodic, and terminal snapshots followed by results `2`, `4`, `6`.

### What didn't work
- N/A

### What I learned
- Streaming pipeline stage lifecycles overlap by design; documentation must not imply strictly sequential phase starts.
- The reporter example is also a compact manual compatibility check for JSON shape and terminal parity.

### What was tricky to build
- The final compatibility optimization required distinguishing “root execution with no observer” from “nested execution sharing an observer.” The predicate now requires a Ledger or Reporter before creating root observation state.

### What warrants a second pair of eyes
- Public API naming before assigning a release tag.
- Whether five seconds is the right terminal sink timeout.
- Whether downstream durable reporters need an exported configurable timeout in a future version.

### What should be done in the future
- Merge and release the Flowkit API, then update RAG-TTC's module requirement from v0.1.1. Until release, the workspace checkout supplies the implementation for dependent development.

### Code review instructions
- Review commits `29847b6`, `73e447b`, and `79e586c` in order.
- Run `make ci-check` and `go test -race ./... -count=1`.
- Run `go run ./examples/progress-reporter` and verify the last snapshot is terminal and equals the returned report.

### Technical details
- `make ci-check` completed with zero lint issues.
- The release/publication task remains open until the module is merged/tagged; all local implementation and documentation work is complete.

## Step 6: Correct nested publication ownership and lifecycle validation

PR #9 review identified two correctness gaps in the first observation implementation. Batched group and repair runs shared sequence state but still started independent periodic publishers, so one external reporter could receive competing callbacks with changing totals and partial reports. Invalid runner configuration could also publish `run_started` before runner construction rejected the work.

The correction makes the root execution the only owner of external publication. Nested engines register live report functions with their enclosing runner while their external Reporter is disabled. The root ticker reads the resulting cumulative report. Runner construction and override-specific validation now complete before any lifecycle event or initial snapshot is emitted.

### Prompt Context

**User prompt (verbatim):** "Address https://github.com/go-go-golems/flowkit/pull/9 code review issues. Then  write a detailed project report for the obsidian vault as a deep dive technical analysis blog post using a textbook writing style (no analogies, see skill).      \n Commit and push the bsidian vault when done (go-go-parc vault) about the flowkit work (read design doc and diary to remind yourself of the work we did)"

**Assistant interpretation:** Resolve both unresolved PR review threads with tests, update the implementation diary, then document the complete Flowkit work in the Obsidian vault.

**Inferred user intent:** Make PR #9 technically safe and preserve the architecture, implementation reasoning, failures, and review corrections as durable project knowledge.

**Commit (code):** 89b210703463a3f97404163ec0f1223561943b30 — "FLOWKIT-002: serialize nested progress reporting"

### What I did
- Added override-specific validators for Bulk and Batched configuration.
- Probed every stage builder before emitting `run_started` or the initial snapshot.
- Moved periodic publication ownership for custom engines into `runCore`.
- Added internal live-report registration so nested execution exposes current counters without calling the external Reporter.
- Made Batched aggregate completed group work with the active repair report under a mutex.
- Made pipeline barrier runners expose nested live reports to the outer pipeline publisher.
- Added tests proving stable root totals, contiguous sequences, cumulative group/repair reports, and zero events/snapshots for invalid plain, Bulk, and Batched runners.
- Ran repeated focused tests, the full race suite, and `go vet ./...`.

### Why
- Sequence uniqueness alone does not guarantee callback order or snapshot coherence when multiple goroutines call the same sink.
- `run_started` is a durable claim that validated execution began; configuration errors must precede it.

### What worked
- `go test ./flow -count=10` passed.
- `go test -race ./...` passed across every package.
- `go vet ./...` passed.
- Batched periodic snapshots retain the root input total and show completed group work plus active repair work in one report.

### What didn't work
- The first implementation assumed sharing `observationState` was sufficient for nested execution. It serialized sequence allocation but did not serialize external callback invocation or preserve one cumulative denominator/report.

### What I learned
- Publication ownership and report aggregation are separate invariants. Shared counters solve identity; a single root-owned ticker solves delivery order and denominator stability.
- Validation boundaries must include constructor-owned override configuration, not only generic policy and resource plans.

### What was tricky to build
- Standalone Bulk/Batched runs and custom engines embedded as pipeline barriers need the same ownership rule. A standalone custom engine has no outer stage ticker, while a pipeline barrier already has one. `registerReport` supplies live state upward in both cases, and nested runs always clear their external Reporter.
- A finished nested report must replace, not coexist with, its live report function. Otherwise the outer report double-counts the same counters. The barrier and Batched aggregators clear the live source before merging the final report.

### What warrants a second pair of eyes
- The internal report registration seam is intentionally unexported; review locking and callback lifetime under cancellation.
- Stage builders are constructed once for validation and again for execution. They are currently side-effect free; future builders must preserve that invariant or validation should return prepared runners.
- Verify Reporter callbacks remain strictly non-concurrent if future runner kinds introduce their own publishers.

### What should be done in the future
- Resolve the two PR review threads after the pushed commit is visible.
- Merge and release the Flowkit API, then update RAG-TTC's module requirement.

### Code review instructions
- Start with `Run` and `runCore` in `flow/run.go`, then follow `registerReport` through `runStages`, `overrideStageRunner`, `runBulk`, and `runBatched`.
- Run `go test ./flow -count=10`, `go test -race ./...`, `go vet ./...`, and `make ci-check`.
- Inspect `TestBatchedReporterUsesOneRootPublisherAndStableTotal` and `TestInvalidRunnersEmitNoLifecycleOrSnapshots`.

### Technical details
- Nested runs retain Ledger step/item events but cannot publish externally.
- Root `Snapshot.Total` remains the original input length throughout a Batched run.
- Invalid runner configuration emits neither ledger events nor snapshots.
