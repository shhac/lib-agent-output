// Command colordemo renders representative family output with color forced on,
// so the default scheme can be eyeballed. Run it at a terminal:
//
//	go run ./examples/colordemo
package main

import (
	"os"

	output "github.com/shhac/lib-agent-output"
)

func main() {
	output.SetColorMode(output.ColorAlways)

	// 1. Single resource (get) — pretty JSON on stdout.
	_ = output.PrintJSON(os.Stdout, map[string]any{
		"id":       "U123",
		"name":     "Alice Example",
		"email":    "alice@test.com",
		"age":      30,
		"active":   true,
		"manager":  nil,
		"url":      "https://example.test/u/U123?ref=demo",
		"channels": []any{"general", "random"},
	}, nil)

	// 2. NDJSON list + pagination trailer on stdout.
	nd := output.NewNDJSONWriter(os.Stdout)
	_ = nd.WriteItem(map[string]any{"id": "T1", "title": "First", "count": 5})
	_ = nd.WriteItem(map[string]any{"id": "T2", "title": "Second", "count": 12})
	_ = nd.WritePagination(output.Pagination{HasMore: true, NextCursor: "eyJwIjoy"})

	// 3. Structured error envelope on stderr (semantic emphasis).
	output.WriteError(os.Stderr, output.New("workspace not authenticated", output.FixableByHuman).
		WithHint("run 'agent-slack auth login' then retry"))

	// 4. Non-fatal notice on stderr.
	output.WriteNotice(os.Stderr, "using cached token (offline)", "pass --refresh to force a fetch")
}
