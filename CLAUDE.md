# mcpx - Claude Code Instructions

## What This Is

MCP protocol bridge for AI agents. Eliminates context bloat by letting agents call MCP servers via CLI instead of loading tool schemas.

## Current State

- **Python implementation**: Working (`mcpx` script)
- **Next step**: Rewrite in Go for single-binary distribution and native concurrency

## Key Files

- `mcpx` - Main CLI (Python, will become `mcpx.py` when Go version arrives)
- `~/.mcpx/servers.json` - Server configuration
- `~/.mcpx/tokens.json` - OAuth tokens (auto-managed)

## Architecture

```
Agent → mcpx (CLI/bash) → MCP Server
```

Agent never loads MCP tool schemas. mcpx translates CLI calls to JSON-RPC.

## Working on This Project

### Priorities

1. Keep it simple - this is a dumb pipe, not a framework
2. Zero overhead for agents - fast CLI, clean JSON output
3. Single binary goal - Go rewrite enables this

### When Making Changes

- All output must be valid JSON (for agent parsing)
- Error codes must be structured: `{"ok": false, "error": {"code": "...", "message": "..."}}`
- Daemon mode is the fast path - optimize for it
- OAuth complexity lives here so agents don't deal with it

### Commit Style

```
feat: Add retry with backoff
fix: Handle token refresh race condition
docs: Update daemon mode examples
refactor: Extract OAuth flow to module
```

## Beads

This project uses `bd` for issue tracking. Run `bd ready` at session start.

## Don't

- Don't add MCP-specific logic beyond protocol translation
- Don't add agent orchestration (that's the agent's job)
- Don't break JSON output format (agents depend on it)
- Don't add features without clear pain point (3+ occurrences)
