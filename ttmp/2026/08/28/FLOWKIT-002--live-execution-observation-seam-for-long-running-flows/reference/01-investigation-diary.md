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
