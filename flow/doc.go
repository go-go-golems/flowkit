// Package flow is the one typed step/pipeline layer over pkg/execution: it
// unifies bounded parallelism, fail-closed admission, retry with error
// classification, content-addressed per-item caching, batching-with-repair,
// per-item failure policy, exact lifecycle journaling, and immutable live
// progress snapshots behind a single Step type.
//
// flow owns mechanics; anything resembling workflow belongs to the calling
// program. There is no DAG scheduler, no persisted control state, and no
// distributed anything (RAG-TTC-FLOW-001 DR-1). Durability is memoization:
// the Store interface carries the execution cache contract, the default
// implementation is the existing content-addressed *execution.FileCache, and
// swapping the durable mechanism means providing another Store — steps and
// pipelines never change. Resume = replay: a killed process costs one
// in-flight item per worker.
//
// A Ledger receives exact run, step, retry, cache, and terminal item events.
// A Reporter receives deep-cloned initial, periodic, and terminal aggregate
// Snapshots. Both are fail-closed when configured; callers wanting best-effort
// telemetry must wrap the sink and explicitly swallow its errors. With neither
// configured, execution does not initialize observation state.
//
// The package is deliberately generic. It knows nothing about RAG, models,
// providers, jobs, or user interfaces; domain adapters (generation,
// embeddings) live next to their domain types and produce Steps.
package flow
