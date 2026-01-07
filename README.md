# mcpx

MCP protocol bridge for AI agents. Zero context bloat.

## Problem

MCP (Model Context Protocol) servers provide powerful integrations, but loading tool schemas into agent context wastes tokens:

```
Before: Agent loads 46k tokens of MCP tool schemas (even if unused)
After:  Agent calls mcpx via bash (zero schema overhead)
```

## Solution

mcpx is a CLI bridge that speaks MCP so your agent doesn't have to:

```
Agent → mcpx (CLI) → MCP Server
  ↑         ↑            ↑
Bash    Translates    Full MCP
only    to JSON-RPC   protocol
```

**What stays the same:** MCP servers unchanged, protocol preserved, OAuth/auth handled.

**What changes:** Agent speaks CLI, not MCP. Context stays clean.

## Usage

```bash
# List configured servers
mcpx --servers

# List tools on a server
mcpx --tools supabase

# Call a tool (one-shot)
mcpx --call supabase execute_sql '{"query": "SELECT * FROM users LIMIT 5"}'

# OAuth login
mcpx --auth supabase

# Daemon mode (fast, keeps connections alive)
mcpx --daemon                    # Start daemon
mcpx --query supabase execute_sql '{"query": "..."}'  # Fast query
mcpx --daemon-stop               # Stop daemon
```

## Configuration

`~/.mcpx/servers.json`:

```json
{
  "servers": {
    "supabase": {
      "url": "https://mcp.supabase.com/mcp?project_ref=xxx&read_only=true"
    },
    "betterstack": {
      "url": "https://mcp.betterstack.com"
    }
  }
}
```

## Architecture

### Core Principle: Delegated MCP

MCP principles preserved - we add a delegation layer for token efficiency:

```
Native MCP:
┌─────────┐         ┌────────────┐
│  Agent  │──MCP───▶│ MCP Server │
└─────────┘         └────────────┘
     ↑
  Tool schemas in context (bloat)


Delegated via mcpx:
┌─────────┐         ┌─────────┐         ┌────────────┐
│  Agent  │──CLI───▶│  mcpx   │──MCP───▶│ MCP Server │
└─────────┘         └─────────┘         └────────────┘
     ↑                   ↑
  Just bash tool    Full MCP protocol
  (minimal bloat)   (preserved)
```

### Daemon Mode

For fast queries, mcpx runs as a daemon with:
- Persistent HTTP connections (connection pooling)
- Session management
- Tool schema caching (5-min TTL)
- OAuth token refresh

## Prior Art

| Project | Description | Comparison |
|---------|-------------|------------|
| [Anthropic MCP-CLI](https://github.com/anthropics/claude-code/issues/12836) | Built into Claude Code | Tied to Claude Code ecosystem |
| [lastmile-ai/mcp-agent](https://github.com/lastmile-ai/mcp-agent) | Full agent framework | Heavier, includes orchestration |
| [llmc](https://github.com/vmlinuzx/llmc) | Progressive disclosure | Focuses on tool context reduction |
| [ComposioHQ/Rube](https://github.com/ComposioHQ/Rube) | MCP server with lazy loading | Server-side solution |

### Rube Comparison

Rube solves context bloat at the *server* level:
- `RUBE_SEARCH_TOOLS` meta-tool discovers tools on-demand
- Nothing frontloaded, 15-min caching
- 500+ integrations consolidated

mcpx solves it at the *client* level:
- No tool schemas loaded ever (CLI bridge)
- Works with any MCP server
- Standalone, portable

**Potential hybrid:** Use Rube as backend MCP server through mcpx.

### Empirical Results

User-reported results with similar approach (Anthropic's experimental MCP-CLI):

| Metric | MCP-CLI Off | MCP-CLI On | Savings |
|--------|-------------|------------|---------|
| MCP tools | 46.6k tokens | 0 tokens | **100%** |
| Total context | 74k | 25k | **62%** |

Source: [GitHub Issue #12836](https://github.com/anthropics/claude-code/issues/12836#issuecomment-3629052941)

## Design Decisions

### Why CLI, not library?

- Agents already have bash access
- Zero integration overhead
- Language agnostic
- Simple JSON I/O

### Why daemon mode?

- HTTP connection overhead is real (~100ms per request)
- Session management is stateful
- Tool caching reduces MCP round-trips

### Why Go?

Written in Go for:
- Single binary distribution (no dependencies)
- Native concurrency via goroutines for parallel agent queries
- Fast startup
- Robust daemon lifecycle management

## Roadmap

### Done
- [x] Basic CLI (--tools, --call, --servers)
- [x] OAuth with PKCE and dynamic registration
- [x] Daemon mode with connection pooling
- [x] Tool schema caching (5-min TTL)
- [x] Structured error codes
- [x] Go rewrite with single binary
- [x] Goroutines for parallel queries
- [x] Request logging in daemon mode

### Planned
- [ ] `--add` / `--remove` commands for server management
- [ ] Local MCP server process management (start/stop with daemon)
- [ ] Onboarding skill for Claude Code / Codex / Cursor
- [ ] Retry with backoff
- [ ] `--search` mode (Rube-style tool discovery)

## Installation

### From source (Go)

```bash
git clone https://github.com/badri/mcpx.git
cd mcpx
CGO_ENABLED=0 go build -o mcpx .
cp mcpx /usr/local/bin/
```

### From Go

```bash
go install github.com/badri/mcpx@latest
```

### Initialize config

```bash
mcpx --init
# Edit ~/.mcpx/servers.json with your servers
```

## License

Apache 2.0

## Related

- [MCP Specification](https://spec.modelcontextprotocol.io/)
- [Your MCP Servers Are Eating Your Context](https://lakshminp.substack.com/p/your-mcp-servers-are-eating-your) - Blog post on the problem
