# mcpx - Claude Code Instructions

## What This Is

MCP protocol bridge for AI agents. Eliminates context bloat by letting agents call MCP servers via CLI instead of loading tool schemas.

## Current State

- **Go implementation**: Main implementation (single binary)
- **Python implementation**: POC only (`mcpx.py`), do not modify

## Key Files

- `main.go` - CLI entry point and argument handling
- `mcp.go` - MCP client and JSON-RPC protocol
- `daemon.go` - Daemon mode with connection pooling
- `config.go` - Configuration types and persistence
- `oauth.go` - OAuth flow and token management
- `errors.go` - Structured error responses
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
3. Single binary distribution - Go enables this

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
