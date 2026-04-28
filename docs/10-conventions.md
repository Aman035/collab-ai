# 10 · Conventions

Go style and project conventions specific to collab-ai. Follow standard Go conventions (`gofmt`, effective Go) for everything not listed here.

## Imports

- Standard library first.
- Third-party second.
- Project-local third (`github.com/Aman035/collab-ai/...`).
- Group with blank lines. Use `goimports` to format.

## Errors

- Errors are values. Return them; do not panic in normal flow.
- Wrap with `fmt.Errorf("...: %w", err)` when adding context. Use `errors.Is` / `errors.As` to check.
- Do **not** swallow errors silently. If you intentionally ignore one, write a one-line comment explaining why.
- User-facing errors (printed by the CLI) MUST NOT include stack traces or Go-isms. Translate them at the CLI layer.

```go
// good: internal layer
return fmt.Errorf("failed to append entry: %w", err)

// good: CLI layer wrapping the above
fmt.Fprintln(os.Stderr, "could not save your message — see logs for details")
```

## Logging

Use `log/slog` (standard library, Go 1.21+). Two log levels matter:

- `slog.Info` — peer joined, session started, file synced (only at significant moments)
- `slog.Warn` — recoverable problems (slow subscriber, oversize file rejected, peer dropped)
- `slog.Error` — unrecoverable problems

`slog.Debug` is fine but should be rare. Don't log every message handled.

**Do not log** API keys, file contents, or full message payloads. Log peer IDs, paths, and sizes.

## Concurrency

- Components that own goroutines MUST accept a `context.Context` and stop when it's cancelled.
- Channels close from the producer side, not the consumer.
- Use `sync.RWMutex` over `sync.Mutex` when reads dominate writes.
- Don't use buffered channels as queues. They're for backpressure smoothing only.
- Every `go func()` should have a clear answer to "how does this stop?"

## Interfaces

- Define interfaces at the **consumer**, not the producer. The Sync Engine defines what it needs from `store.Store`; `internal/store` provides a struct that satisfies it.
- One exception: `pkg/protocol` types are concrete structs, not interfaces — they're the wire format.
- Avoid mock-ception. If you need a test stub, a concrete in-memory implementation is fine.

## Testing

- Unit tests live next to the code they test (`store_test.go` next to `store.go`).
- Integration tests that span components live in `internal/<a>/integration_test.go`.
- The two-process Axl test in `internal/transport/integration_test.go` should run in CI but be tagged `//go:build integration` so it can be skipped locally.
- Use `t.TempDir()` for filesystem tests. Never write to the actual `./shared/` in tests.

## File naming

- One main type per file when possible (`store.go` defines `Store`; `change.go` defines `Change`).
- Test files: `<source>_test.go`.
- Mock/stub files for tests: `testing.go` in the same package.

## Go module path

```go
module github.com/Aman035/collab-ai

go 1.22
```


## What to avoid

- **Generics for the sake of generics.** v1 is small and concrete. Reach for generics only when they collapse multiple near-identical functions.
- **`init()` functions.** Wire dependencies explicitly in `cmd/collab-ai/main.go`.
- **Global state.** No singletons, no package-level vars holding state. The CLI entry point constructs one Store, one Sync Engine, one Transport, and passes them around.
- **Panics for control flow.** Panic only in genuinely unrecoverable conditions (corrupt internal state). Recover at the top of goroutines started by main.
- **Comments that restate the code.** Comments explain *why*, not *what*. If the code is unclear, rewrite the code.

## Project-specific names

Use these names consistently — both in code and in docs:

| Concept | Name | Notes |
|---------|------|-------|
| The CLI binary | `collab-ai` | Lowercase, hyphenated |
| A participant | "peer" | Not "user", not "client", not "node" |
| The originating peer | "host" | Used in docs and code |
| The other peers | "joiners" | Used in docs and code |
| The shared directory | "the shared dir" or `./shared/` | Not "workspace", not "the folder" |
| The conversation log | "the log" | Not "history", not "transcript" |
| A single conversation entry | "log entry" or "entry" | Not "message", not "turn" |
| Network message | "WireMessage" or "wire message" | Distinct from log entries |

Consistency matters: a contributor reading docs and grepping the code should find the same words.
