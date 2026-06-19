// Package output is the shared NDJSON output contract for agent-first CLIs.
//
// It is the canonical, zero-dependency home for the conventions the agent-*
// family already follows by hand: NDJSON records on stdout, structured
// {error, fixable_by, hint} objects on stderr, and @-prefixed metadata lines
// such as @pagination. Both the CLIs (which produce this output) and tools
// that consume it (such as lib-agent-mcp) import this package so the wire
// format is defined in exactly one place.
package output
