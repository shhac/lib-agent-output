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

**The wire contract** (what producers and consumers must agree on):

| File | Contents |
|---|---|
| `ndjson.go` | `NDJSONWriter` (`WriteItem`, `WriteMetaLine`, `WritePagination`), `Pagination`, `MetaKeyPagination` |
| `errors.go` | `Error`, `FixableBy`, `New`/`Newf`/`Wrap`/`WithHint`/`As`, `WriteError` |
| `notice.go` | `WriteNotice` |

**Opt-in presentation helpers** (shared, zero-dep, domain-free — so a CLI can
delete its hand-rolled `internal/output/`):

| File | Contents |
|---|---|
| `format.go` | `Format` + `FormatJSON/YAML/NDJSON`, `ParseFormat`, `ResolveFormat`, `Print`, `PrintJSON`, `RegisterEncoder` |
| `prune.go` | `Pruner` type + `PruneNils` / `PruneEmpty` — selectable compact-projection policies |
| `list.go` | `WriteList` — render a record list + `@`-meta as NDJSON stream or `{data, …}` JSON envelope |

### Format routing and the zero-dep YAML hook

`Print`/`WriteList` handle `json` and `jsonl` natively. YAML (or any other
non-stdlib format) is supported via a registered encoder, so **the core never
imports a YAML library** — a CLI opts in once:

```go
import "gopkg.in/yaml.v3"

func init() {
    output.RegisterEncoder(output.FormatYAML, yaml.Marshal)
}
```

Unregistered formats return a structured error. The helpers are **opt-in and
policy-neutral**: pruning is a `Pruner` you pass to `Print`/`WriteList` (or
`nil`), not a baked-in default — `PruneNils` drops only nulls, `PruneEmpty` also
drops empty strings/maps/slices, and a producer that must preserve nulls (e.g.
fixed tabular columns) passes `nil`. Callers also supply their own meta-key
names, so `WriteList` imposes no envelope policy.

```go
output.Print(w, deployment, format, output.PruneEmpty)   // compact
output.WriteList(w, format, rows, meta, nil)             // preserve everything
```

The package has **no third-party dependencies** and must stay that way — see
[`AGENTS.md`](AGENTS.md). Background, the family conventions it codifies, and the
12-repo survey behind these choices are in
[`design-docs/contract.md`](design-docs/contract.md) and
[`design-docs/family-survey.md`](design-docs/family-survey.md).

## Develop

```sh
go test ./...
go vet ./...
```

## License

[PolyForm Noncommercial License 1.0.0](LICENSE) — © 2026 Paul Somers.
