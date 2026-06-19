# AGENTS.md — lib-agent-output

Guidance for an agent (or human) working in this repo. `CLAUDE.md` is a symlink
to this file.

## What this is

`lib-agent-output` (Go package `output`, module
`github.com/shhac/lib-agent-output`) is the canonical, zero-dependency
definition of the NDJSON output contract used by the agent-first CLI family. It
is imported by **producers** (the `agent-*` CLIs that emit the format) and
**consumers** (e.g. `lib-agent-mcp`, which parses it).

Read [`design-docs/contract.md`](design-docs/contract.md) for the full contract
and where it came from.

## Layout

| File | Contents |
|---|---|
| `ndjson.go` | `NDJSONWriter`, `Pagination` |
| `errors.go` | `Error`, `FixableBy`, constructors, `WriteError` |
| `notice.go` | `WriteNotice` |
| `output_test.go` | shape/framing/escaping tests |

## Build, test, verify

```sh
go build ./...
go vet ./...
go test ./...
```

## Hard constraints

- **Zero third-party dependencies.** This package sits at the bottom of the
  dependency graph for the whole family; anything it imports, every CLI
  inherits. Standard library only. If you reach for a dependency, stop and
  reconsider. Formats that need a third-party encoder (YAML) are supported via
  the `RegisterEncoder` hook — the CLI imports `yaml.v3` and registers it; the
  core never does. Do not add an encoder dependency directly.
- **Presentation helpers are opt-in and policy-neutral.** `Print`, `WriteList`,
  the pruners (`PruneNils`/`PruneEmpty`/`Pruner`), redaction (`Redact`/
  `RedactRule`/`RedactKeys`), and the `Format` routing are conveniences, not part
  of the must-agree wire contract. The *policy* is always caller-supplied —
  pruning is a `Pruner` (or `nil`), redaction is a `RedactRule` (or none),
  retry-after is `WithRetryAfter`. The library owns the *mechanism* and the wire
  shape (`@redacted` notes, `[REDACTED]`, `--expose` matching); WHICH fields are
  secret / empty is the producer's business decision, not this package's. Keep it
  that way; don't reintroduce a boolean or a baked-in field list. The contract
  types (`Error`, `Pagination`, `NDJSONWriter`, `WriteNotice`) are what consumers
  like `lib-agent-mcp` rely on.
- **This is a contract, not just code.** A change to the JSON shape (field
  names, the `@`-prefix convention, `fixable_by` values, error keys) ripples to
  every producer and consumer. Treat the wire format as a stable API: additive
  changes preferred; breaking changes need a deliberate version bump and a
  migration note in `design-docs/`.
- **HTML escaping stays off.** All encoders use `SetEscapeHTML(false)` so URLs
  and query strings survive intact. Keep it that way.

## Conventions / how to infer things

- **Code style**: early returns; self-documenting code; comments explain *why*.
- **Source of truth**: when in doubt about a field name or shape, the existing
  family CLIs (`lin`, `agent-vercel`, `agent-slack`, `agent-sql`) are the
  reference this package was distilled from. `lin` is the family's canonical
  example. Match them; this module exists to *unify* those copies, not diverge.
- **Backwards compatibility**: a consumer may be parsing output from an older
  producer and vice versa. Prefer `omitempty` and tolerant readers.

## Naming convention (family-wide)

- `lib-agent-*` = shared libraries (this repo, `lib-agent-mcp`).
- `agent-*` = the CLIs that consume them.

## Design docs

`design-docs/` holds durable rationale. Record contract decisions there
(why a field exists, why a shape was chosen, rejected alternatives) so future
changes don't relitigate settled tradeoffs.
