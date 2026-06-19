# lib-agent-output

The shared, **zero-dependency** NDJSON output contract for agent-first CLIs.

A family of CLIs built for LLM/agent consumption (`agent-sql`, `agent-slack`,
`agent-vercel`, `lin`, …) all follow the same output conventions — and each one
had copy-pasted its own `internal/output/` package. This module is the single
canonical home for those conventions, so the wire format is defined once and
imported by both **producers** (the CLIs) and **consumers** (such as
[`lib-agent-mcp`](https://github.com/shhac/lib-agent-mcp)).

## The contract

- **stdout — results as NDJSON.** One bare JSON record per line. Metadata rides
  on `@`-prefixed lines (e.g. a trailing `{"@pagination": {...}}`).
- **stderr — structured diagnostics.** Errors as `{error, fixable_by, hint?}`;
  non-fatal notices as `{notice, hint?}`. Never freeform prose, so stderr stays
  machine-parseable.
- **`fixable_by`** classifies who resolves an error: `agent` (fix input and
  retry), `human` (auth / confirmation), or `retry` (transient — 429/5xx/net).
- **exit code** — non-zero means failure.

## Usage

```go
import output "github.com/shhac/lib-agent-output"

// List → NDJSON records, then a pagination trailer.
w := output.NewNDJSONWriter(os.Stdout)
for _, it := range items {
    _ = w.WriteItem(it)
}
_ = w.WritePagination(output.Pagination{HasMore: true, NextCursor: cur})

// Structured, classified errors to stderr.
return output.New(fmt.Sprintf("widget %q not found", id), output.FixableByAgent).
    WithHint("list ids with 'mycli item list'")

// ...written at the top level:
if err := root.Execute(); err != nil {
    output.WriteError(os.Stderr, err) // {"error":...,"fixable_by":...,"hint":...}
    os.Exit(1)
}
```

## What's here

| File | Contents |
|---|---|
| `ndjson.go` | `NDJSONWriter` (`WriteItem`, `WriteMetaLine`, `WritePagination`), `Pagination` |
| `errors.go` | `Error`, `FixableBy`, `New`/`Newf`/`Wrap`/`WithHint`/`As`, `WriteError` |
| `notice.go` | `WriteNotice` |

The package has **no third-party dependencies** and must stay that way — see
[`AGENTS.md`](AGENTS.md). Background and the family conventions it codifies are
in [`design-docs/contract.md`](design-docs/contract.md).

## Develop

```sh
go test ./...
go vet ./...
```

## License

[PolyForm Noncommercial License 1.0.0](LICENSE) — © 2026 Paul Somers.
