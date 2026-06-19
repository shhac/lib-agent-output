# The agent-CLI NDJSON output contract

This module codifies an output contract that the agent-first CLI family already
followed by convention. This doc records *what* the contract is, *where it came
from*, and *why* it is shaped this way — so changes don't relitigate it.

## Origin

Four CLIs — [`lin`](https://github.com/shhac/lin), `agent-vercel`,
`agent-slack`, `agent-sql` — independently shipped a near-identical
`internal/output/` package (same `NDJSONWriter`, `Pagination`, `APIError`,
`FixableBy`). `lin` is the family's reference implementation; `agent-slack`'s
`design-docs/cli-design.md` documents the conventions explicitly ("follows the
family convention", "conventions lifted from lin").

Four copies of the same ~150 lines is three copies too many. This module is the
single home; the CLIs should depend on it and delete their copy. It is also
imported by `lib-agent-mcp`, which *consumes* the format to translate it to MCP.
That dual producer/consumer role is the reason it must be standalone and
zero-dependency: a CLI should be able to emit a pagination line without pulling
in an MCP server, and the contract's version must not be coupled to either side.

## Principles (inherited from the family)

1. **LLM-first.** Output is for an agent, not a human terminal. Structured,
   parseable, token-economical.
2. **Token economy.** Compact records by default; bulky payloads behind opt-in
   flags; truncation with explicit markers. (Projection/truncation lives in the
   CLIs; this module provides the framing.)
3. **Chainability.** Every record carries the IDs the next command needs.
4. **Structured errors always.** JSON on stderr with `fixable_by` and a hint
   that names the exact follow-up command — never a bare message.

## The contract

### stdout — results

- **NDJSON**: one bare JSON record per line (`NDJSONWriter.WriteItem`).
- **Metadata** rides on `@`-prefixed single-key lines, emitted after the data
  records. The canonical one is the pagination trailer:
  `{"@pagination": {"has_more": true, "next_cursor": "..."}}`. Others seen in
  the family: `@unresolved`, `@referenced_projects`, and `@redacted` (the
  redaction note list, attached to a record by `Redact`). A consumer
  distinguishes metadata from data by the single `@`-prefixed key.

### stderr — diagnostics

- **Errors**: `{"error": msg, "fixable_by": ..., "hint": ...?,
  "retry_after_seconds": ...?}` — one JSON object (`WriteError`). `hint` and
  `retry_after_seconds` omitted when empty/zero.
- **Notices** (non-fatal): `{"notice": msg, "hint": ...?}` (`WriteNotice`).
- Keeping stderr structured (never freeform prose) means a consumer can parse
  both streams.

### `fixable_by` — who resolves the error

| Value | Meaning | Typical cause |
|---|---|---|
| `agent` | Caller can fix its input and retry | bad args/flags/target (4xx validation) |
| `human` | A person must act | auth, permissions, payment, explicit confirmation |
| `retry` | Transient; retry with backoff | 429, 5xx, network |

This field is the contract's highest-value idea: it tells an automated caller
*what to do next* without parsing the message. `lib-agent-mcp` maps it straight
onto agent behaviour.

**`FixableByStatus(httpStatus)`** captures the HTTP-status → `fixable_by`
mapping that all six surveyed REST CLIs (vercel, cloudflare, dd, incident,
stripe, posthog) wrote identically — `401/402/403 → human`, `429/5xx → retry`,
everything else → `agent`. It is classification only; the message, hint, and
error-body parsing stay vendor-specific.

**`retry_after_seconds`** (optional) carries how long to wait before retrying a
`retry` error. It is set by the producer via `WithRetryAfter` — typically from a
`Retry-After` header — and the library imposes **no default**: retry timing is
policy the CLI owns, the same stance taken for pruning. Backoff/retry *loops*
are a client-layer concern and live in the CLI, not here.

### exit code

Non-zero ⇒ failure. The family uses a binary 0/1 (no per-error gradation).

## Shape decisions

- **Bare records, not an envelope, on stdout.** NDJSON is for streaming; an
  envelope (`{"data": [...]}`) is what `--format json` produces instead. The
  default for lists is NDJSON; single resources default to pretty JSON. This
  module provides the NDJSON primitives; format routing stays in the CLIs.
- **`@`-prefix for metadata.** Lets data and metadata share one stream without
  a wrapper object, and is trivially detectable.
- **`SetEscapeHTML(false)` everywhere.** Slack/Vercel/Linear payloads are full
  of URLs and query strings; HTML-escaping `&`/`<`/`>` corrupts them.
- **`Error` carries an unexported `Cause`.** Preserves wrapping/`errors.As`
  while keeping the JSON shape clean (`Cause` is `json:"-"`).

## Compatibility

A newer consumer may read an older producer's output and vice versa. Prefer
`omitempty`, tolerant readers, and additive change. A breaking change to field
names or the `@`-convention is a family-wide event — bump the version and write
a migration note here.

## Migration plan

Point each family CLI at this module and delete its `internal/output/`:
the package name is `output` (matching existing call sites), so
`output.WriteError`, `output.NewNDJSONWriter`, `output.Pagination`, etc. mostly
compile unchanged. The one rename to watch: the family's `APIError` is `Error`
here.
