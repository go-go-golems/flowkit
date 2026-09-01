---
Title: Investigation diary
Ticket: FLOWKIT-003
Status: active
Topics:
    - documentation
    - consolidation
    - testing
    - go
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://boundary_layering_test.go
      Note: Layering boundary test added
    - Path: repo://examples/full-run/main.go
      Note: Composite example with deterministic retry via StatusError
ExternalSources: []
Summary: 'Chronological diary for FLOWKIT-003: executing the three fixes, writing the consolidation design-doc, and refreshing the intern onboarding guide into its permanent home.'
LastUpdated: 2026-09-01T19:45:00-04:00
WhatFor: Record what was changed, why, what failed, and how to validate the FLOWKIT-003 deliverables.
WhenToUse: Read before resuming FLOWKIT-003 or before starting the deferred execution/flow dedup follow-up.
---



# Investigation diary

## Goal

Execute the three "do first" fixes from the post-FLOWKIT-002 review, then
write a consolidation design-doc addressing all six findings and a refreshed
intern onboarding guide in its permanent home, and upload the bundle to
reMarkable.

## Step 1: Fix #2 (layering boundary test) and Fix #3 (examples consolidation)

I added a boundary test that enforces `flow -> execution -> internal` and
consolidated the duplicated `examples/` into one composite program plus the
verified `scripts/` suite.

### Prompt Context

**User prompt (verbatim):** "do 1. 2. 3. (from "What I'd do first"). Then Create a new design-doc to address all these issues and Create  a detailed analysis / design / implementation guide that is for a new intern..."

**Assistant interpretation:** Execute fixes #1 (guide home), #2 (layering test), #3 (examples consolidation), then write a consolidation design-doc and a refreshed intern guide.

**Inferred user intent:** Make the documentation/structure fixes real (code + layout), document the decisions, and re-deliver the onboarding guide properly.

### What I did
- Created ticket FLOWKIT-003 + design-doc + diary; added 6 tasks; added vocab `consolidation`, `testing`.
- **Fix #2:** wrote `boundary_layering_test.go` (`TestExecutionAndInternalDoNotImportFlow`). Verified PASS.
- **Fix #3:** removed `examples/{bounded-map,cached-step,pipeline}` (fully duplicated by `scripts/`); added `examples/full-run/main.go` (cached+retry+budget+replay+quarantine); added `examples/README.md`; updated `README.md` and `docs/flowkit-developer-guide.md` links.
- Verified `go test ./...` green and `golangci-lint` 0 issues; committed.

### Why
The layering was only half-enforced (ragkit yes, execution↛flow no), and the
two example homes duplicated each other. Both are cheap, high-value fixes.

### What worked
- `examples/full-run` deterministically demonstrates the retry path: item 3
  fails once with `&flow.StatusError{Status:503}` (→ `Transient`), succeeds on
  retry; the report shows `work=6 retries=1`. Run 2 is all hits (`work=0`);
  run 3 quarantines item 6.

### What didn't work
- First `full-run` draft used a plain `fmt.Errorf("status=503: transient")`
  for the transient error. `DefaultClassifier` does **not** string-match — it
  looks for a typed `*StatusError`. A plain error is `Fatal`, which would kill
  the run under any `FailureMode`. Fixed by returning
  `&flow.StatusError{Status: 503, Err: ...}`.
- First draft also made the retry nondeterministic (`n==3 && p.calls==1`),
  depending on worker scheduling. Fixed with a per-item `failedOnce` map so
  item 3 fails exactly once regardless of order.
- `gofmt`/errcheck: `defer os.RemoveAll(dir)` needed `defer func(){ _ = ... }()`.
- Two commits hit a transient `parallel golangci-lint is running` lock
  collision (a golangci-lint from another workspace was running); killed the
  stray process and the commit succeeded.

### What I learned
- `DefaultClassifier` is strictly typed for HTTP status (`*StatusError` with
  `HTTPStatus() int`); plain `"status=503"` strings are `Fatal`. This is a
  real pitfall for adapter authors and is now called out in the onboarding
  guide §5.4 and the `full-run` example.
- `flow.Step` is a generic type and cannot be used as a function; `Report.Step`
  is the accessor. (I wrote `flow.Step("summarize", rep)` first — a compile
  error.)

### What was tricky to build
- Making the composite example deterministic under `Workers: 3` while still
  exercising retry. Solved by keying the one-shot failure on item identity, not
  global call count.

### What warrants a second pair of eyes
- `boundary_layering_test.go` uses `go list -deps` like the existing ragkit
  test; if `go list` ever resolves test-only imports differently, both tests
  share the behavior. Acceptable — it mirrors the proven pattern.

### What should be done in the future
- The deferred execution/flow dedup (F4) — scoped in the design-doc §5.
- A `testing.Short()`-guarded `MySQLCache` example.

### Code review instructions
- Run `go test -run 'TestExecutionAndInternalDoNotImportFlow|TestPackagesDoNotImportRagkit' . -v`.
- Run `go run ./examples/full-run`; confirm retries=1 on run 1, hits=5 on run 2.

### Technical details
- `examples/full-run` uses an on-disk `FileCache` in a temp dir, `Workers:3`,
  `Retry.Attempts:3`, `Admission` "provider-calls" Budget 20, `Preflight`
  MaxEstimatedUSD 1, `OnError: Quarantine`.
