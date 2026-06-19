# Migrating a CLI onto `lib-agent-output`

How to replace a hand-rolled `internal/output/` (and usually `internal/errors/`)
package in an agent-first CLI with the shared `github.com/shhac/lib-agent-output`
module. This guide is generic — it applies to any `agent-*` family CLI. Work
through it in order; the verification step is the load-bearing one.

The family's `internal/output` packages were copied between repos and then
drifted. The shared module is the de-drifted superset. Migrating is mostly a
mechanical swap **plus** a small number of genuine decisions where your copy
diverged. The whole job is: delete your copy, import `output`, reconcile the
divergences, and prove the bytes on the wire didn't change in ways you didn't
intend.

---

## What the shared module owns vs. what stays yours

**Owned by `lib-agent-output` (delete your copies, import these):**

- The wire contract: `Error` + `FixableBy`, `NDJSONWriter`
  (`WriteItem`/`WriteMetaLine`/`WritePagination`), `Pagination`, `WriteNotice`,
  `MetaKeyPagination`.
- Opt-in, zero-dep helpers: `Format` + `ParseFormat`/`ResolveFormat`, `Print`,
  `PrintJSON`, `WriteList`, the `Pruner` policies (`PruneNils`/`PruneEmpty`), and
  the redaction mechanism (`Redact` + `RedactRule`/`RedactKeys`).

**Stays in your CLI (domain-specific — the shared module deliberately won't take it):**

- **The redaction *predicate*** — *which* fields are secret (your key list,
  value prefixes, or secret-echo) is a `RedactRule` you supply; the mechanism
  (`[REDACTED]`, `@redacted` notes, `--expose`) is the library's. See gotcha #6.
  The one part that stays fully yours: masking a secret that appears as a
  *substring* of a larger value (`Redact` masks whole values only).
- **Truncation field-sets** (which fields to truncate and at what limit).
- **CSV** and any other niche format your CLI offers beyond json/yaml/jsonl.
- **Bespoke `@`-meta keys** (`@counts`, `@unresolved`, `@referenced_*`, …) —
  you keep emitting them via `WriteMetaLine`; only the convention is shared.
- **The YAML dependency itself** — see gotcha #3. The core stays dependency-free;
  your CLI registers its YAML encoder.

If a piece of your output code encodes knowledge about *your* API's resources,
it stays. If it encodes the *shape of the contract* (records, errors,
pagination, format routing), it moves.

---

## Step 0 — Assess before changing anything

1. **Add the dependency:**

   ```sh
   go get github.com/shhac/lib-agent-output@latest   # or pin @v0.1.0
   ```

2. **Inventory** your two packages and their blast radius:

   ```sh
   # Which files depend on the packages you're about to delete?
   git grep -l 'internal/output' | wc -l
   git grep -l 'internal/errors' | wc -l
   # Call-site counts for the symbols you'll rename:
   git grep -c 'errors\.\(New\|Newf\|Wrap\)\|FixableBy' | awk -F: '{n+=$2} END{print n}'
   ```

3. **Build the symbol map** (next section) and **flag the divergences** (the
   four gotchas below). The migration is mechanical *except* where your copy
   diverged — find those first so they don't surprise you mid-swap.

---

## The symbol map

The family copies are near-identical, so most symbols map one-to-one. Typical
mapping (yours on the left — names may vary slightly per repo):

| Your `internal/output` / `internal/errors` | `lib-agent-output` (`package output`) |
|---|---|
| `APIError` (struct) | `Error` |
| `FixableBy`, `FixableByAgent/Human/Retry` | same names |
| `errors.New / Newf / Wrap / WithHint / WithHints / WithCause / As` | same names on `output` |
| `WriteError(w, err)` | `output.WriteError(w, err)` |
| `mapStatus` / `classifyHTTPError` status→FixableBy switch | `output.FixableByStatus(code)` (keep your message/hint building; see gotcha #5) |
| Retry-After in hint text | `(*Error).WithRetryAfter(d)` → structured `retry_after_seconds` (gotcha #5) |
| `NDJSONWriter`, `NewNDJSONWriter` | same |
| `WriteItem`, `WriteMetaLine` | same |
| `WritePagination` (if present) | `output.WritePagination(Pagination)` |
| `Pagination` | `output.Pagination` |
| `Format`, `FormatJSON/YAML/NDJSON` | same |
| `ParseFormat`, `ResolveFormat(flag, def)` | same |
| `Print(w, data, format, prune)` | `output.Print(w, data, format, prune)` — `prune` is now a `Pruner`, not a `bool` (gotcha #2) |
| `PrintJSON` | same — `prune` is a `Pruner` |
| paginated-list helper (`PrintList`/`emitList`/`printList`) | `output.WriteList(w, format, items, meta, prune Pruner)` |
| `pruneNulls` (nil-only) | `output.PruneNils` |
| `pruneEmpty` (nil + empties) | `output.PruneEmpty` |
| `Stdout()/Stderr()/SetWriters()` test-injection globals | not provided — pass `io.Writer` explicitly, or keep a tiny local shim |
| `walkTree`, `normalizeYAMLNumbers` | not provided — keep locally; used by your YAML encoder (gotcha #3) |
| redaction walk + `@redacted` notes + `--expose` | `output.Redact(v, rule, expose)`; your `shouldRedact` switch becomes a `RedactRule` (gotcha #6) |
| truncation, CSV | not provided — **stays yours** |

---

## Step 1 — (Optional) alias shim for an incremental swap

If your CLI is large, you can swap the *implementation* without touching call
sites first, then inline later. Keep your `internal/output` package but make it
re-export the shared one:

```go
package output

import out "github.com/shhac/lib-agent-output"

type (
    Error      = out.Error      // type aliases keep call sites compiling
    FixableBy  = out.FixableBy
    Pagination = out.Pagination
    Format     = out.Format
)

const (
    FixableByAgent = out.FixableByAgent
    FixableByHuman = out.FixableByHuman
    FixableByRetry = out.FixableByRetry
    FormatJSON     = out.FormatJSON
    FormatYAML     = out.FormatYAML
    FormatNDJSON   = out.FormatNDJSON
)

var (
    New              = out.New
    Wrap             = out.Wrap
    WriteError       = out.WriteError
    NewNDJSONWriter  = out.NewNDJSONWriter
    Print            = out.Print
    ResolveFormat    = out.ResolveFormat
    // …
)
```

Build, run the suite, diff output (Step 3). Once green, delete the shim and
rewrite imports to point at `output` directly. The shim is a safety net, not the
destination — don't leave it in long-term.

---

## Step 2 — Swap

1. **Delete** `internal/output/` and `internal/errors/` (or empty the shim).
2. **Rewrite imports** to `github.com/shhac/lib-agent-output` (alias it `output`
   so call sites read naturally).
3. **Rename the error type** at construction sites (`APIError` → `Error` is the
   common one; the JSON shape is identical so the wire doesn't change).
4. **Register a YAML encoder** if you support `--format yaml` (gotcha #3).
5. **Pick your prune** (gotcha #2).
6. `go mod tidy`.

Each of these is independently verifiable — do them one at a time and keep the
build green between steps.

---

## The gotchas (where the real decisions are)

### 1. Error type rename, and deleting the `errors` package

The family's error type is usually named `APIError` and lives in
`internal/errors`; here it's `Error` and lives in `output`. The JSON tags
(`error`/`hint`/`fixable_by`) and the `FixableBy` values are identical, so this
is a **rename, not a wire change**. If your CLI has error-classification helpers
(`ClassifyGraphQLError`, `HandleUnknownCommand`, HTTP-status→`FixableBy`
mapping), those are *yours* — keep them, but have them build/return
`output.Error`.

### 2. Prune is a policy you pass — match yours exactly

Which values count as "empty enough to drop" is a content decision, and the
family legitimately disagrees: `agent-sql` preserves nulls (a null column ≠ a
missing one), most CLIs drop nils only, some also drop empty strings/slices. So
`output` does **not** bake in a default — pruning is a `Pruner` you pass to
`Print`/`WriteList` (or `nil`):

| Your old behavior | Pass this | Result |
|---|---|---|
| nil-only (`pruneNulls`) | `output.PruneNils` | identical bytes — zero-diff migration |
| nil + empty strings/maps/slices | `output.PruneEmpty` | the most compact projection |
| preserve everything (tabular nulls) | `nil` | nothing dropped |
| something bespoke | your own `func(any) any` | your rules |

Because the policy is explicit, there's no silent change to hunt for: pass the
`Pruner` that matches what your CLI did, and the output is unchanged. (Still
worth a golden diff per Step 3, but the prune line is no longer the trap it used
to be.) Note the pruner applies before *every* format including the registered
YAML encoder, so it's uniform across `--format`.

### 3. YAML stays in your CLI, via `RegisterEncoder`

`lib-agent-output` is zero-dependency, so it does **not** import a YAML library.
`Print`/`WriteList` handle `json` and `jsonl` natively and delegate everything
else to a registered encoder. If you support `--format yaml`, register your
encoder once at startup — this is also where any YAML-specific massaging (e.g.
normalizing `float64` integers to `int64`) lives:

```go
import (
    "gopkg.in/yaml.v3"
    output "github.com/shhac/lib-agent-output"
)

func init() {
    output.RegisterEncoder(output.FormatYAML, func(v any) ([]byte, error) {
        return yaml.Marshal(normalizeYAMLNumbers(v)) // your existing helper
    })
}
```

`gopkg.in/yaml.v3` thus stays in *your* `go.mod`, not the shared module's. A CLI
that doesn't offer YAML registers nothing and pulls no YAML dependency.

### 4. JSON casing — a real wire change for camelCase CLIs

`output` uses **snake_case** (`has_more`, `next_cursor`, `total_items`), the
family majority. If your CLI emitted **camelCase** pagination/metadata, adopting
the shared `Pagination` **changes the bytes consumers see**. That's a breaking
change for your CLI's output — schedule it deliberately (and bump your CLI's
version / note it), don't let it ride along silently inside a "refactor."

### 5. Error classification — delete your status switch

Every REST CLI in the family independently wrote the same HTTP-status →
`fixable_by` switch (401/402/403 → human, 429/5xx → retry, else → agent). That's
now `output.FixableByStatus(code)`. Keep your vendor-specific message, hint, and
error-body parsing — only the *classification* moves:

```go
// before: a 30-line classifyHTTPError switch
// after:
return output.Newf(output.FixableByStatus(status), "%s", vendorMessage(body)).
    WithHints(vendorHints...).
    WithRetryAfter(retryAfterFromHeader(resp)) // structured, your value
```

If your CLI refines a code beyond the default (e.g. Stripe promoting an
`authentication_error` body to `human`), branch *after* the default:

```go
fb := output.FixableByStatus(status)
if errType == "authentication_error" || errType == "permission_error" {
    fb = output.FixableByHuman
}
```

**Retry timing is policy you own.** `WithRetryAfter(d)` surfaces a structured
`retry_after_seconds` for the agent — and the library never supplies a default,
so the value is always yours (parse it from the `Retry-After` header, or set
your own). Old CLIs that buried "wait ~30s" in hint text can promote it to the
structured field. (Backoff/retry *loops* stay in your client layer — they vary
across the family and aren't an output concern.)

### 6. Redaction — your `shouldRedact` switch becomes a `RedactRule`

If your CLI masks secret response fields, delete the tree-walk, the `@redacted`
note-builder, and the `--expose` matcher — `output.Redact(v, rule, expose)` owns
all of that. Keep only the **policy** as a `RedactRule`:

```go
// before: redactValue/redactMap/redactSlice + shouldRedactField + isExposed + …
// after — a fixed key list is one line:
data = output.Redact(data, output.RedactKeys("client_secret", "api_key", "secret"), g.Expose)

// value-prefix (posthog), context-aware (stripe), or secret-echo: a custom rule.
rule := func(path, key string, value any, parent map[string]any) bool {
    if s, ok := value.(string); ok && strings.HasPrefix(s, "phc_") { return true } // value-prefix
    return key == "name" && parent["object"] == "customer"                          // context-aware
}
```

The `--expose` flag value passes straight through (`output.Redact` parses
comma-joined entries and matches by exact path, exact key, `<prefix>.`, or
`all`/`*`). The note shape (`{path, reason, expose_hint}`), the `[REDACTED]`
placeholder, and the top-level `@redacted` attachment match the family's
existing wire output, so this is a near-zero-diff swap. **The exception**: if you
redact a secret that's *echoed inside* a larger string (deepweb's raw-byte
scrub), keep that pass — `Redact` masks whole values, not substrings.

### Also worth checking

- **`WriteError` does not exit.** `output.WriteError(w, err)` writes one JSON
  line and returns. Keep your top-level `os.Exit(1)` (e.g. in `main` after
  `root.Execute()`); don't assume the writer exits for you.
- **Pagination field fit.** `output.Pagination` is `{has_more, next_cursor?,
  total_items?}`. If your API paginates by URL/offset/page, put that token in
  `next_cursor` as an opaque string, or emit a bespoke struct via
  `WriteMetaLine(MetaKeyPagination, yourStruct)` — don't force a mismatched
  shape.
- **Test-writer globals.** If you relied on package-level `Stdout()`/`Stderr()`
  with a `SetWriters` test hook, the shared module doesn't have them (it takes
  `io.Writer` arguments). Thread the writer through, or keep a tiny local helper.
- **Return signatures.** `output.Print`, `WriteList`, `WriteItem`,
  `WriteMetaLine`, and `WritePagination` all return `error`. If your local
  equivalents were `void` (several family copies are), call sites won't compile
  until you handle or `_ =` the returned error. This is a compile-time nudge,
  not a behavior change — but it's the one that turns a "find/replace" into a
  "touch every call site," so expect it.

---

## Step 3 — Verify (do not skip)

Behavior preservation is the whole point. In order of value:

1. **Golden-output diffs.** Before migrating, capture real output for a
   representative spread — a list (with pagination), a single resource, each
   `--format`, and at least one error of each `fixable_by` class:

   ```sh
   for cmd in "thing list" "thing get ID" "thing list --format json" "thing list --format yaml"; do
     ./your-cli $cmd > golden/"${cmd// /_}".out 2> golden/"${cmd// /_}".err
   done
   ```

   After migrating, regenerate and `git diff golden/`. **Every diff must be one
   you intended** (e.g. an expected prune change). Unintended diffs are bugs.

2. **Run the test suite** — `go test ./...`. Update any tests that asserted on
   the old type names/paths.

3. **Manual smoke** of a couple of real commands, eyeballing stdout and stderr.

---

## Step 4 — Cleanup

- `go mod tidy` — drops now-unused deps. `gopkg.in/yaml.v3` stays only if you
  registered a YAML encoder; otherwise it should disappear.
- Remove helpers that are now dead (e.g. `walkTree` if nothing else used it).
- `go vet ./...` and your linter.
- Grep for stragglers: `git grep 'internal/output\|internal/errors\|APIError'`
  should come back empty (or only your retained domain code).

---

## Rollback

The migration is a normal sequence of commits. If a golden diff reveals an
unintended wire change you can't reconcile, `git revert` the swap commit — the
shared module and your old package are byte-compatible on the contract, so a
clean revert restores the previous behavior exactly.

---

## Convergence note

Once a CLI is migrated, the two casing outliers in the family
(`agent-sql`, `agent-statsig`, which use camelCase) are the ones with a real
wire decision to make (gotcha #4); everything else is a near-mechanical swap.
See [`design-docs/family-survey.md`](design-docs/family-survey.md) for the
per-repo divergence map.
