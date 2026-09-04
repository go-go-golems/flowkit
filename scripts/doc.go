// Package examples holds runnable, verified examples of the Flowkit public
// API. Each example is a Go Example function with an // Output: block, so the
// whole suite both documents the library and is exercised by:
//
//	go test ./scripts/ -v
//
// Read each Example function top-to-bottom as a copy-paste recipe. The files
// are organized by layer:
//
//   - execution_examples_test.go  — the low-level execution toolbox
//   - step_examples_test.go       — flow.Step, caching, dedup, retry
//   - policy_examples_test.go     — failure modes, admission, preflight
//   - pipeline_examples_test.go   — Pipe2/3/4 streaming composition
//   - bulk_batch_examples_test.go — Bulk and Batched group execution
//   - observe_examples_test.go    — metering, ledger, OnResult, Store swap
//
// Flowkit is a domain-neutral Go library for bounded, recoverable,
// policy-controlled work over collections. It runs expensive, repeatable work
// over a list of inputs while preserving input order, caching results
// durably, bounding concurrency/budget/rate, retrying with error
// classification, and reporting what happened. It is NOT a workflow engine,
// DAG scheduler, or distributed coordinator.
package examples
