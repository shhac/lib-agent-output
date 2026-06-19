# Family output-convention survey (12 repos)

A read-only survey of every `agent-*`/`lin` repo that ships an
`internal/output` package, done to decide what `lib-agent-output` should
capture and where the family diverges. Source repos surveyed: `lin`,
`agent-vercel`, `agent-slack`, `agent-sql`, `agent-stripe`, `agent-deepweb`,
`agent-cloudflare`, `agent-dd`, `agent-incident`, `agent-posthog`,
`agent-postmark`, `agent-statsig`.

## What is unanimous (the stable contract)

- **Error envelope** — identical in all 12: `{"error": msg, "hint"?: ...,
  "fixable_by": agent|human|retry}`, Go struct with `Message json:"error"`,
  `Hint json:"hint,omitempty"`, `FixableBy json:"fixable_by"`,
  `Cause error json:"-"`. (Named `APIError` in the family; `Error` here.)
- **fixable_by** — exactly `agent` / `human` / `retry`, same meanings.
- **NDJSON** — bare JSON record per line on stdout; metadata on `@`-prefixed
  lines; `@pagination` is the universal trailer.
- **Format trio** — `json` (pretty, single-resource default), `jsonl`/`ndjson`
  (list default), `yaml`. Lists→NDJSON, single resources→pretty JSON.
- **Null pruning** before output (compact projections; `--full` for raw).
- **Errors always JSON on stderr**, exit 1, regardless of `--format`.

## Where the family diverges, and the call

| Axis | Majority (→ adopt) | Outliers (→ should converge) |
|---|---|---|
| **JSON casing** | snake_case — 10/12 | camelCase: `agent-sql`, `agent-statsig` |
| **Pagination** | `{has_more, total_items?, next_cursor?}` — vercel/slack/dd/incident/stripe | offset: cloudflare `{page,per_page,total_pages,…}`; url: posthog `next_url`; offset-int: postmark `next_offset`; page: statsig `page`; tabular: sql `rowCount` |
| **Non-fatal notice** | structured `WriteNotice` → `{notice, hint?}` (slack) | plain-text `Warn: …` (agent-sql); most have none |
| **NDJSON writer API** | `WriteItem` + `WriteMetaLine` + `WritePagination` (union) | sql uses `WriteRow(map[string]any)` (tabular) |
| **Meta keys** | `@pagination` (universal) | `@counts`/`@skipped` (dd), `@request` (cloudflare/deepweb), `@redacted` (stripe), `@query` (posthog), `@truncated` (sql) |

### Casing — snake_case
The documented family convention is snake_case; `agent-sql` and `agent-statsig`
are un-converged. `lib-agent-output` uses **snake_case**. Recommend the two
outliers migrate (a breaking wire change for their consumers — schedule
deliberately).

### Pagination — cursor with an opaque token
The agent only needs two things: *is there more*, and *what token fetches it*.
`has_more` is the one universal field; everything else (`next_cursor`,
`next_url`, `next_offset`, `page`, `rowCount`) is an encoding of "the token."
So the generic shape is **`{has_more, next_cursor?, total_items?}`** with
`next_cursor` treated as an **opaque string** — a URL, an offset, or a page
number all serialize into it and the agent just echoes it back. Genuinely
richer pagination (cloudflare's `page`/`per_page`/`total_pages`) is a domain
choice and should be emitted via `WriteMetaLine("@pagination", customStruct)`
rather than forced into the generic type. `total_items` is kept because five
repos surface it and it's cheap (`omitempty`).

### Notice — structured, not prose
agent-slack's design doc is explicit: stderr must stay machine-parseable JSON,
so a 429 emits `{"notice": ...}` rather than `Warn: ...` text. The structured
form is strictly better for an agent reader. `lib-agent-output` ships
`WriteNotice`; agent-sql's plain-text `Warn` is the outlier to retire.

### Truncation — `@truncated` map beats `XLength` siblings (but stays out of core for now)
Two repos truncate long strings differently: `agent-sql` adds a per-row
`@truncated: {field: originalLength}` map (or `null`); `lin` adds sibling
fields like `descriptionLength`. The **`@truncated` map is better**: it is
self-contained per record, machine-readable, and doesn't pollute the object
with parallel `*Length` keys or require the reader to know field names. But
truncation is only in 2/12 and couples to per-field policy, so it is **not** in
the zero-dep core yet — see `DECISIONS_NEEDED`. If adopted, model it on
agent-sql's `@truncated`.

## Deliberately out of `lib-agent-output` (and why)

- **YAML / CSV encoders** — need third-party deps (`gopkg.in/yaml.v3`); the core
  is zero-dependency. YAML may be supported via an *optional registered
  encoder* hook (keeps the core dep-free); CSV is niche and SQL-shaped.
- **Redaction** (`@redacted`, `--expose`) — *revised in v0.4.0.* Originally filed
  here as "stays in the CLIs," conflating the policy with the whole feature. By
  the same logic as `Pruner`, the *mechanism* (tree-walk, `[REDACTED]` masking,
  `@redacted` notes, `--expose` matching) is shared as `Redact(v, rule, expose)`;
  only the predicate — WHICH fields are secret (stripe's `client_secret`,
  posthog's `phc_` prefixes, deepweb's secret-echo) — is the injected
  `RedactRule`. The one thing that stays CLI-side is **substring** redaction over
  raw bytes (deepweb's echo scrub); `Redact` masks whole values only.
- **HTTP/tabular envelopes** — agent-deepweb's request/response envelope and
  agent-sql's `{columns, rows}` are domain shapes, not the generic record list.

## Convergence recommendation

`lib-agent-output` should be the single home for the **unanimous contract**
(errors, NDJSON records + `@`-meta, pagination, notices) plus the
**domain-free, zero-dep helpers** the family already shares. The `agent-*`
CLIs then delete their copied `internal/output/` and import it. The two
casing outliers (`agent-sql`, `agent-statsig`) need a deliberate, breaking
migration; everything else is a near-mechanical swap (the package is already
named `output`).

## Scope decision (resolved)

Two cases were argued — a minimal "wire-contract types only" package vs. a
broad near-drop-in. **Resolved: broad, but disciplined.**

- The minimal case's load-bearing objection — a YAML dependency would infect
  every consumer's module graph — is removed by the **`RegisterEncoder`
  hook**: the core stores a `func(any)([]byte,error)` and never imports a YAML
  library, so it stays genuinely zero-dependency.
- The minimal case's valid point — pruning and envelope shape are *policy* the
  repos legitimately disagree on (lin strips empties, vercel only nils, sql
  preserves nulls deliberately) — is handled by making the policy an explicit,
  caller-supplied choice. Pruning is a `Pruner` value passed to `Print`/
  `WriteList` (`PruneNils`, `PruneEmpty`, your own, or `nil`), not a baked-in
  default or a boolean that silently picks one; and `WriteList` takes the
  caller's own meta-key names, so it blesses no envelope winner. (The original
  v0.1.0 used a single `Prune` + a `prune bool`; v0.2.0 externalized the policy
  to `Pruner` precisely because *which* values are "empty" is the producer's
  business decision, not this package's.)
- The deciding factor: a types-only package leaves the *most-copied,
  most-drifted* code (format routing, prune, envelope) uncovered, so no CLI
  could actually delete its `output.go`. That would unify the ~5% that already
  agrees and leave the ~95% that drifted — not convergence.

So the package ships the wire contract **plus** opt-in, zero-dep, domain-free
helpers (`Format`/`ParseFormat`/`ResolveFormat`, the `Pruner` policies,
`Redact` + `RedactRule`, `Print`/`PrintJSON`, `WriteList`, `RegisterEncoder`) —
each owning a mechanism while the policy is injected. Truly domain concerns
(truncation field-sets, CSV, substring secret-echo) stay in the CLIs.
