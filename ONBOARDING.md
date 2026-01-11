# mcpx Onboarding Guide for Claude Code

This guide walks you through setting up mcpx with Claude Code. After setup, Claude Code will automatically handle MCP server queries when you ask about databases, logs, or any configured server.

**Time required:** ~5 minutes

## Prerequisites

- Claude Code installed and working
- Go 1.21+ (for building from source) or a pre-built binary

## Step 1: Install mcpx

### Option A: From source (recommended)

```bash
git clone https://github.com/badri/mcpx.git
cd mcpx
CGO_ENABLED=0 go build -o mcpx .
sudo cp mcpx /usr/local/bin/
```

### Option B: Via go install

```bash
go install github.com/badri/mcpx@latest
```

### Verify installation

```bash
mcpx --help
```

You should see the usage information.

## Step 2: Install the Claude Code skill

This installs a skill that teaches Claude Code how to use mcpx:

```bash
mcpx --init-skill
```

Output:
```
Installed Claude Code skill: /Users/you/.claude/skills/mcpx.md
```

The skill tells Claude Code to:
- Recognize MCP-related requests (databases, logs, server names)
- Discover available servers and tools
- Execute queries via subagents (to keep context clean)
- Return human-readable results

## Step 3: Configure your MCP servers

Initialize the config directory:

```bash
mcpx --init
```

Edit `~/.mcpx/servers.json` with your servers:

```json
{
  "servers": {
    "supabase": {
      "url": "https://mcp.supabase.com/mcp?project_ref=YOUR_PROJECT_REF&read_only=true"
    },
    "betterstack": {
      "url": "https://mcp.betterstack.com"
    }
  }
}
```

### Finding your server URLs

| Service | URL Format |
|---------|------------|
| Supabase | `https://mcp.supabase.com/mcp?project_ref=<ref>&read_only=true` |
| BetterStack | `https://mcp.betterstack.com` |
| Custom | Check your MCP server documentation |

## Step 4: Authenticate with OAuth servers

Most MCP servers use OAuth. Authenticate with each server:

```bash
mcpx --auth supabase
```

This opens your browser for OAuth authorization. After approving, tokens are saved to `~/.mcpx/tokens.json`.

Repeat for each server:

```bash
mcpx --auth betterstack
```

### Verify authentication

```bash
mcpx --tools supabase
```

You should see a JSON list of available tools.

## Step 5: Start the daemon

The daemon keeps connections alive for fast queries:

```bash
mcpx --daemon
```

Output:
```
Daemon started (pid 12345)
```

The daemon runs in the background. To check status:

```bash
mcpx --daemon-status
```

### Auto-start daemon (optional)

Add to your shell profile (`~/.zshrc` or `~/.bashrc`):

```bash
# Start mcpx daemon if not running
mcpx --daemon-status >/dev/null 2>&1 || mcpx --daemon
```

## Step 6: Start using Claude Code

You're done! Now just talk to Claude Code naturally:

### Example queries

| You say | Claude Code does |
|---------|------------------|
| "Show me users created today from Supabase" | Queries `supabase` server with SQL |
| "Search BetterStack for errors in the last hour" | Searches logs via `betterstack` |
| "What tables are in my database?" | Lists tables via `supabase` |
| "Check my logs for 500 errors" | Searches with appropriate filters |

### What happens behind the scenes

```
You: "Show me recent errors from BetterStack"
        |
        v
Claude Code recognizes "BetterStack" -> invokes /mcpx skill
        |
        v
Skill runs: mcpx --servers (confirms betterstack exists)
        |
        v
Skill runs: mcpx --daemon-tools betterstack (discovers search_logs tool)
        |
        v
Skill spawns subagent to run:
  mcpx --query betterstack search_logs '{"query": "level:error", "range": "1h"}'
        |
        v
Claude Code: "Found 3 errors in the last hour: [formatted summary]"
```

## Manual skill invocation

If Claude doesn't auto-detect your intent, invoke the skill directly:

```
/mcpx
```

Then describe what you want.

## Troubleshooting

### "Daemon not running" error

Start the daemon:

```bash
mcpx --daemon
```

### "Server not configured" error

Check your servers:

```bash
mcpx --servers
```

Add missing servers to `~/.mcpx/servers.json`.

### "Auth expired" error

Re-authenticate:

```bash
mcpx --auth <server-name>
```

### Check daemon logs

```bash
cat ~/.mcpx/daemon.log
```

### Reset everything

```bash
mcpx --daemon-stop
mcpx --clear-sessions
mcpx --clear-tokens
mcpx --daemon
mcpx --auth <server-name>
```

## Customizing the skill

The skill is a markdown file you can edit:

```bash
cat ~/.claude/skills/mcpx.md
```

You can:
- Add server-specific instructions
- Customize error handling
- Add example queries for your use case

## Next steps

- Add more servers to `~/.mcpx/servers.json`
- Explore available tools: `mcpx --daemon-tools <server>`
- Check the [README](README.md) for advanced usage
