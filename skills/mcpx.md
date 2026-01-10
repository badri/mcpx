---
name: mcpx
description: Query MCP servers (databases, logs, APIs). Matches requests about Supabase, BetterStack, database queries, log searches, or any configured MCP server.
user-invocable: true
---

# MCP Server Access via mcpx

Use this skill when the user wants to interact with MCP servers - databases, logging services, APIs, or any tool accessible via MCP protocol.

## Prerequisites

- mcpx daemon must be running (`mcpx --daemon`)
- Servers configured in `~/.mcpx/servers.json`

## Workflow

### 1. Discover available servers

```bash
mcpx --servers
```

### 2. Explore tools on a server

```bash
mcpx --daemon-tools <server-name>
```

### 3. Execute queries via subagent

**Important:** Spawn a subagent for actual queries to keep the main context clean.

```
Use the Task tool with subagent_type="general-purpose" to run:
mcpx --query <server> <tool> '{"arg": "value"}'
```

### 4. Return results

Parse the JSON response and return a human-readable summary to the user. Only include relevant data, not raw JSON unless requested.

## Example

User: "Check my Supabase for users created today"

1. Confirm server exists: `mcpx --servers`
2. Find the right tool: `mcpx --daemon-tools supabase`
3. Spawn subagent to run: `mcpx --query supabase execute_sql '{"query": "SELECT * FROM users WHERE created_at > CURRENT_DATE"}'`
4. Return: "Found 12 users created today: [summary]"

## Error Handling

- If daemon not running: Tell user to run `mcpx --daemon`
- If server not found: Show available servers from `mcpx --servers`
- If tool not found: Show available tools from `mcpx --daemon-tools <server>`

## Model

Use `haiku` for simple queries to reduce cost. Use `sonnet` for complex multi-step operations.
