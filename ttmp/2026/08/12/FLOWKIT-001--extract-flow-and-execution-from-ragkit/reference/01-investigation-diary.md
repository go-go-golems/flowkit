---
Title: Investigation diary
Ticket: FLOWKIT-001
Status: active
Topics:
    - architecture
    - migration
    - go
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://boundary_test.go
      Note: Step 4 extraction boundary guard (commit 2042a08)
    - Path: repo://execution/cache.go
      Note: Step 4 cache extraction and digest ownership (commit 2042a08)
ExternalSources: []
Summary: Chronological evidence, commands, failures, decisions, and review guidance for the Flowkit extraction analysis.
LastUpdated: 2026-08-12T23:18:00-04:00
WhatFor: Resume or review the extraction investigation without repeating discovery work.
WhenToUse: Before continuing FLOWKIT-001 or reviewing the architecture guide and extraction plan.
---


# Diary

## Goal

Capture how the Flowkit extraction ticket and implementation guide were built, including repository evidence, command failures, corrected assumptions, and delivery validation.

## Step 1: Initialize the extraction ticket

I read the handoff and the ticket-research writing/checklist references before creating the workspace. The ticket establishes a durable home for the architecture guide, chronological diary, tasks, and delivery evidence, and the first commit provides a clean bookkeeping baseline before analysis content.

The initial scope follows the handoff's central recommendation: move `flow` and `execution` together into the standalone Flowkit module while preserving behavior and cache interoperability.

### Prompt Context

**User prompt (verbatim):** "Create a new docmgr ticket to extract flow + execution out of ./ragkit/ into ./flowkit/ . Read /tmp/handoff-flowkit-extraction.md to get into it, then Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable.

commit at appropriate intervals and keep a detailed diary as you work (using the diary format from the skill)

[REMINDER] Output a <summary>...</summary> block at the VERY END of your response. This is mandatory."

**Assistant interpretation:** Create and maintain a docmgr ticket containing an evidence-backed, intern-oriented Flowkit extraction design and diary, commit at reviewable boundaries, validate the ticket, and upload a bundled PDF to reMarkable.

**Inferred user intent:** Give a new implementer enough architectural and operational context to execute the extraction safely without rediscovering subtle cache, concurrency, admission, and migration contracts.

**Commit (code):** f990c6b — "docs: initialize Flowkit extraction ticket"

### What I did

- Read `/tmp/handoff-flowkit-extraction.md` completely.
- Read the ticket-research writing style and deliverable checklist.
- Ran `docmgr status --summary-only` and confirmed the configured root is `flowkit/ttmp`.
- Created ticket `FLOWKIT-001` with architecture, Go, and migration topics.
- Created the design doc and investigation diary.
- Added three tasks for architecture mapping, guide authoring, and validation/upload.
- Committed the generated ticket workspace and docmgr templates.

### Why

- A separate baseline commit makes generated scaffolding distinguishable from authored analysis.
- Creating tasks before research makes the required evidence and delivery gates visible.

### What worked

- `docmgr` created the complete ticket structure, including index, tasks, changelog, design doc, and diary.
- The initial commit completed cleanly on Flowkit's `task/extract-flowkit` branch.

### What didn't work

- Reading `ragkit/AGENT.md` returned the exact error:

  `ENOENT: no such file or directory, access '/home/manuel/workspaces/2026-08-12/extract-flowkit/ragkit/AGENT.md'`

  The workspace-level `AGENT.md` therefore governs ragkit unless a deeper instruction file is discovered.

### What I learned

- The workspace root itself is not a Git repository; `flowkit` and `ragkit` are separate nested repositories.
- Flowkit began as a template repository with only an initial commit and a placeholder module.
- Docmgr is configured from the workspace but stores tickets under Flowkit.

### What was tricky to build

The main sharp edge was repository ownership. Running Git from the workspace root fails because the workspace is not a repository, while commits must be made with `git -C flowkit` or from inside `flowkit`. I verified both nested statuses before creating or committing ticket files.

### What warrants a second pair of eyes

- Confirm `FLOWKIT-001` and the proposed module name match project naming conventions.
- Confirm the generated docmgr templates belong in this otherwise new repository; they were created by first-time docmgr initialization.

### What should be done in the future

- Complete evidence mapping before changing source code.
- Keep ragkit and Flowkit commits separate and phase-focused.

### Code review instructions

- Start at `flowkit/ttmp/2026/08/12/FLOWKIT-001--extract-flow-and-execution-from-ragkit/index.md`.
- Verify ticket structure with `cd flowkit && docmgr ticket list --ticket FLOWKIT-001 && docmgr doc list --ticket FLOWKIT-001`.
- Review commit `f990c6b` as scaffolding only.

### Technical details

Creation commands included:

```bash
docmgr ticket create-ticket --ticket FLOWKIT-001 \
  --title "Extract flow and execution from ragkit" \
  --topics architecture,migration,go
docmgr doc add --ticket FLOWKIT-001 --doc-type design-doc \
  --title "Flowkit extraction architecture and implementation guide"
docmgr doc add --ticket FLOWKIT-001 --doc-type reference \
  --title "Investigation diary"
```

## Step 2: Map execution, flow, support code, and consumers

I inspected the implementation from the low-level primitives upward: ordered maps, cache envelopes, budgets and reservations, then typed flow steps, shared run environments, pipeline execution, bulk calls, batching-with-repair, reports, and classifiers. I also read the embedding, generation, and reranking adapters to determine which policy belongs in Flowkit and which remains domain-specific.

This investigation corrected one assumption from the handoff: embedding and generation are the direct `flow` consumers, but reranking and several legacy decorators directly consume `execution`. Both import families must be migrated.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Build a source-backed architecture map and migration plan detailed enough for an intern to implement safely.

**Inferred user intent:** Avoid a mechanical copy that accidentally breaks hidden consumers, cache data, accounting, or retry behavior.

### What I did

- Read all central production files under `ragkit/flow` and `ragkit/execution`.
- Read support implementations in `digest`, `internal/fsutil`, and `internal/jsonutil`.
- Read production adapters in embedding, generation, and reranking.
- Enumerated exported APIs with `rg -n '^(type|func|var) [A-Z]'`.
- Searched all ragkit Go files for both old import paths.
- Measured approximately 3,645 production lines and 2,930 test lines in the two packages.
- Inspected recent subsystem history with `git -C ragkit log --oneline -20 -- flow execution`.
- Measured the exact ragkit package dependency closure with `GOWORK=off go list -deps ./flow ./execution`.
- Ran focused tests in workspace-disabled mode.

### Why

- The design guide must explain runtime behavior, not just list files.
- Migration scope must include every direct consumer and every support dependency.
- Cache and concurrency recommendations require evidence from actual code paths and tests.

### What worked

The corrected focused validation passed:

```text
ok  github.com/go-go-golems/ragkit/flow
ok  github.com/go-go-golems/ragkit/execution
ok  github.com/go-go-golems/ragkit/rag/embedding
ok  github.com/go-go-golems/ragkit/rag/generation
ok  github.com/go-go-golems/ragkit/rag/reranking
```

The dependency inventory with `GOWORK=off` showed only ragkit's `digest`, `execution`, `flow`, `internal/fsutil`, and `internal/jsonutil` packages in the movable closure.

### What didn't work

The initial workspace-mode dependency and test command failed before package analysis:

```text
go: module ../coinvault listed in go.work file requires go >= 1.26.5, but go.work lists go 1.26; to update it:
	go work use
go: module . listed in go.work file requires go >= 1.26.5, but go.work lists go 1.26; to update it:
	go work use
go: module ../flowkit listed in go.work file requires go >= 1.26.1, but go.work lists go 1.26; to update it:
	go work use
go: module ../rag-ttc listed in go.work file requires go >= 1.26.5, but go.work lists go 1.26; to update it:
	go work use
go: module ../ragopt listed in go.work file requires go >= 1.26.1, but go.work lists go 1.26; to update it:
	go work use
go: module ../glazed listed in go.work file requires go >= 1.26.1, but go.work lists go 1.26; to update it:
	go work use
```

I did not mutate the shared workspace as part of a documentation task. I reran module-local commands with `GOWORK=off`, which produced valid focused evidence.

### What I learned

- `flow` is an orchestration layer over `execution`, not an independent package.
- Cache compatibility depends on JSON field names, SHA-256 input bytes, schema text, strict decoding, and atomic file publication.
- Every fresh retry receives admission; hits do not.
- Multi-resource admission is transactional only for reservable limiters; custom wait-only limiters cannot be refunded.
- `context.WithoutCancel` intentionally protects storage after successful expensive work.
- Pipelines stream by default and restore final order; batched overrides are barriers.
- The default classifier contains a generic API but a ragkit incident-derived fallback table.
- Flowkit's current `go.mod` still declares `github.com/go-go-golems/XXX`.

### What was tricky to build

The subsystem has overlapping abstraction levels: `execution.MapCachedBatches` resembles `flow.Bulk`, and `execution.ResourcePlan` resembles `flow.Resource`. The underlying cause is that `execution` offers focused primitives while `flow` adds typed orchestration, shared preflight, retries, failure policies, hooks, and reports. Treating overlap as accidental duplication would encourage a risky merge. I documented the distinction and deferred consolidation until after behavior-preserving extraction.

A second sharp edge was cancellation semantics. Ordinary work should honor cancellation, but a successful expensive result must still be atomically committed even if a sibling has failed. The exact solution already used by the code is `Store.Store(context.WithoutCancel(ctx), ...)`; this must survive extraction.

### What warrants a second pair of eyes

- Cache fixtures need byte-level review by someone familiar with historical provider-step caches.
- Classifier separation must not remove retries from existing ragkit campaigns.
- Race coverage should scrutinize report maps, meters, in-flight calls, shared budgets, hooks, and bulk result writes.
- The full consumer inventory should be repeated outside this workspace, including private repositories.

### What should be done in the future

- Add pre-extraction compatibility fixtures and boundary tests before moving files.
- Repair or regenerate the shared `go.work` version before cross-module integration.
- Decide source-history preservation and logging strategy.

### Code review instructions

- Start with `ragkit/execution/cache.go`, then `ragkit/execution/map.go`, `ragkit/flow/step.go`, and `ragkit/flow/run.go`.
- Compare adapter contracts in `ragkit/rag/embedding/cached.go`, `ragkit/rag/generation/flow_step.go`, and `ragkit/rag/reranking/cached.go`.
- Reproduce focused tests with:

```bash
cd ragkit
GOWORK=off go test ./flow ./execution ./rag/embedding ./rag/generation ./rag/reranking -count=1
```

### Technical details

Observed dependency direction:

```text
ragkit domain packages -> flow -> execution -> digest/fsutil/jsonutil
```

Required target direction:

```text
ragkit domain packages -> flowkit/flow -> flowkit/execution -> flowkit/internal/*
```

## Step 3: Author the intern-oriented architecture and implementation guide

I converted the evidence into a long-form guide that teaches the subsystem before prescribing migration steps. It includes conceptual vocabulary, layered diagrams, runtime pseudocode, API sketches, compatibility contracts, design decisions, phased commits, a validation matrix, risks, alternatives, file references, and a definition of done.

The guide distinguishes observed current behavior from proposed extraction choices. It also records the workspace-version failure and the broader execution-consumer inventory so an implementer does not repeat the same assumptions.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Produce a clear technical teaching document and actionable extraction runbook in the ticket.

**Inferred user intent:** Enable a new intern to understand both the system and the reasons behind each safe migration phase.

### What I did

- Replaced the generated design placeholder with a comprehensive architecture and implementation guide.
- Added ASCII diagrams for dependency layering, cache addressing, admission transactions, per-item execution, and batching repair.
- Added pseudocode for ordered map, boundary enforcement, retry/classification flow, and compatibility testing.
- Added compact decision records for extraction boundary, compatibility, support packages, classifier policy, shims, resource types, and release version.
- Added explicit implementation phases and command-level validation gates.
- Added a file/API reference map and intern review checklist.

### Why

- An intern needs a mental model before a file-move checklist.
- The highest-risk behavior is encoded across several functions, so diagrams and pseudocode make control flow reviewable.
- Decision records prevent future implementers from casually re-litigating compatibility and package-boundary choices during extraction.

### What worked

- The design now contains concrete file references for every major architectural claim.
- The plan separates mechanical extraction, policy separation, consumer migration, and later redesign into independently reviewable phases.
- The validation matrix maps each invariant to both source evidence and a required test.

### What didn't work

- N/A during authoring; document validation and rendering are recorded in the next step.

### What I learned

- The documentation is most understandable when introduced in the same direction as runtime dependencies: support and execution primitives first, then flow orchestration, then adapters and migration.
- Cache compatibility and retry classification deserve separate sections because they are release contracts, not implementation details.

### What was tricky to build

The guide had to be exhaustive without collapsing into a source-code paraphrase. I addressed this by defining terms, explaining each subsystem's purpose in prose, then adding only the API and pseudocode needed to expose invariants. File references are grouped both inline and in a final map so readers can move between conceptual and code-oriented review.

### What warrants a second pair of eyes

- Verify all line ranges remain accurate if ragkit changes before implementation begins.
- Confirm the proposed generic classifier boundary is acceptable; the guide intentionally does not prescribe a chaining API that would require an `Unknown` class.
- Confirm the recommended cache compatibility policy and `v0.1.0` release sequence.

### What should be done in the future

- Refresh line references immediately before extraction if ragkit receives new commits.
- Convert accepted design decisions from `proposed` to `accepted` after maintainer review.

### Code review instructions

- Read the design document from §1 through §4 for the mental model, then §7–§9 for decisions and implementation.
- Search for `Decision:` to review every proposed architectural choice.
- Search for `Open questions` before implementation begins.

### Technical details

Primary deliverable:

`flowkit/ttmp/2026/08/12/FLOWKIT-001--extract-flow-and-execution-from-ragkit/design-doc/01-flowkit-extraction-architecture-and-implementation-guide.md`

## Step 4: Extract the execution layer and establish the module boundary

I replaced the template module boundary with the real Flowkit module and extracted the low-level execution primitives first. This commit also introduces the approved narrow simplifications: only required support helpers moved, and `execution.Key.Digest` is now the single authoritative key fingerprint implementation used by cache paths, outcomes, and Flow callers.

The extraction remains behavior-preserving. Production files and tests moved together, the historical cache schema remains unchanged, and a root boundary test rejects any direct or transitive ragkit dependency.

### Prompt Context

**User prompt (verbatim):** "ok, cool, lets keep 1-4 and defer all the rest. 

Work on the xtraction task by task, commit at appropriate intervals, keep a detailed diary as you work. 

Also write a great developer guide (use glazed help entry format) and a README for flow, and a couple of example programs in examples/ 

Budget 1M

[REMINDER] Output a <summary>...</summary> block at the VERY END of your response. This is mandatory."

**Assistant interpretation:** Continue the extraction task by task, preserve only the four approved extraction-safe simplifications, and produce reviewable code, diary, developer documentation, README, and examples.

**Inferred user intent:** Complete the extraction with reviewable commits, exact compatibility, strong documentation, examples, validation, and delivery.

**Commit (code):** 2042a08bbbc317f8cfb72f6e69aabd0c8ca66e53 — "feat(execution): extract execution primitives"

### What I did

- Changed the module path from `github.com/go-go-golems/XXX` to `github.com/go-go-golems/flowkit`.
- Removed placeholder `cmd/XXX` and `pkg` files.
- Extracted all `execution` production files and tests.
- Added minimal private digest, atomic-file, and strict-JSON support packages.
- Exported `execution.Key.Digest` and updated execution callers to use it.
- Added `TestPackagesDoNotImportRagkit`.
- Updated logcopter generation paths and package areas.
- Ran formatting, dependency tidying, focused tests, and all available Flowkit tests.

### Why

- Moving `execution` first establishes the dependency foundation required by `flow`.
- Central digest ownership prevents cache fingerprint drift.
- Minimal private support packages avoid importing or exposing unrelated ragkit utilities.

### What worked

- `GOWORK=off go test ./execution ./flow ./internal/... -count=1` passed while the uncommitted Flow package was present.
- `GOWORK=off go test ./... -count=1` passed.
- A source search found no ragkit import in extracted packages; only the boundary test's forbidden-prefix string remained.

### What didn't work

- N/A. The first extraction attempt compiled and passed focused tests.

### What I learned

- The execution package requires only one digest operation, two filesystem operations, and two strict JSON decoding operations from ragkit support code.
- Exporting `Key.Digest` removes Flow's need to know the key JSON fingerprint algorithm.

### What was tricky to build

The key digest is a persisted compatibility boundary, not merely an internal helper. I preserved validation, JSON marshaling, SHA-256 encoding, and all call sites while changing only method visibility. The copied Flow files were intentionally left uncommitted so execution could remain its own reviewable commit even though both layers were validated together.

### What warrants a second pair of eyes

- Compare `execution/cache.go` against ragkit for schema, JSON tags, path sharding, corruption handling, and atomic publication.
- Verify the minimal support copies preserve exactly the functions execution uses and expose nothing extra.
- Inspect `boundary_test.go` for direct and transitive dependency coverage.

### What should be done in the future

- Add explicit pre-extraction cache fixture coverage beyond existing cache tests.
- Extract and commit Flow against this execution package.

### Code review instructions

- Start with `execution/cache.go` at `Key.Digest`, then inspect `internal/digest/digest.go` and `boundary_test.go`.
- Validate with `GOWORK=off go test ./execution ./internal/... -count=1`.

### Technical details

The extracted cache retains `rag-ttc-execution-cache/v1`. The module import path changes, but cache keys and envelope bytes do not.
