# Flowkit examples

Flowkit has **two** example homes, by design:

| Home | What it is | How to run | When to read it |
|---|---|---|---|
| [`examples/`](.) | **One** complete, runnable `main()` program that ties the whole story together (cached step + retry + budget + on-disk cache + replay + quarantine). | `go run ./examples/full-run` | When you want to see the entire library work end-to-end as a program. |
| [`../scripts/`](../scripts) | The exhaustive, **verified** per-API reference — 24 `Example` functions with checked `// Output:` blocks. | `go test ./scripts/ -v` | When you want a copy-paste recipe for one specific API. |

## Run the end-to-end program

```bash
go run ./examples/full-run
```

It builds a cached, retryable, budgeted `flow.Step` over a durable on-disk
`execution.FileCache`, runs it twice (the second run is all cache hits —
"resume = replay"), and then demonstrates a quarantined bad item. The output
shows cache hits/misses, work calls, retries, quarantine, and budget spend.

## Why two homes?

The two homes serve different readers and do not duplicate each other:

- `examples/` is a single *program* you can run and modify; it shows how the
  pieces compose, but it is not exhaustively verified.
- `scripts/` is a *test suite* that verifies every documented behavior holds,
  but its snippets are intentionally small and per-API.

Keeping them separate avoids a 2000-line `main()` that is both unreadable and
untestable.
