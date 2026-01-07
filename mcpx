#!/usr/bin/env python3
"""
mcp-cli - MCP HTTP/SSE Client for Agent Use

A lightweight CLI to call MCP servers on-demand, handling session management
and the SSE protocol properly.

Usage:
    mcp-cli --tools <server-name>           # List available tools
    mcp-cli --call <server-name> <tool> '{json-args}'  # Call a tool
    mcp-cli --servers                        # List configured servers
    mcp-cli --auth <server-name>            # OAuth login for a server

Daemon mode (fast):
    mcp-cli --daemon                         # Start daemon (background)
    mcp-cli --query <server> <tool> '{args}' # Fast query via daemon
    mcp-cli --daemon-stop                    # Stop daemon

Config: ~/.mcp-cli/servers.json
"""

import argparse
import json
import os
import sys
import uuid
import webbrowser
import threading
import time
import hashlib
import base64
import secrets
import socket
import signal
import atexit
from pathlib import Path
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs, urlencode

try:
    import httpx
except ImportError:
    print('{"ok": false, "error": {"code": "MISSING_DEP", "message": "httpx required: pip install httpx"}}')
    sys.exit(1)

CONFIG_DIR = Path.home() / ".mcp-cli"
CONFIG_FILE = CONFIG_DIR / "servers.json"
SESSION_FILE = CONFIG_DIR / "sessions.json"
TOKENS_FILE = CONFIG_DIR / "tokens.json"
SOCKET_PATH = CONFIG_DIR / "daemon.sock"
PID_FILE = CONFIG_DIR / "daemon.pid"
TOOLS_CACHE_TTL = 300  # 5 minutes

DEFAULT_HEADERS = {
    "Content-Type": "application/json",
    "Accept": "application/json, text/event-stream",
}


def load_config():
    """Load server configurations."""
    if not CONFIG_FILE.exists():
        return {"servers": {}}
    with open(CONFIG_FILE) as f:
        return json.load(f)


def save_sessions(sessions):
    """Persist session IDs."""
    CONFIG_DIR.mkdir(exist_ok=True)
    with open(SESSION_FILE, "w") as f:
        json.dump(sessions, f)


def load_sessions():
    """Load persisted session IDs."""
    if not SESSION_FILE.exists():
        return {}
    with open(SESSION_FILE) as f:
        return json.load(f)


def load_tokens():
    """Load OAuth tokens."""
    if not TOKENS_FILE.exists():
        return {}
    with open(TOKENS_FILE) as f:
        return json.load(f)


def save_tokens(tokens):
    """Save OAuth tokens."""
    CONFIG_DIR.mkdir(exist_ok=True)
    with open(TOKENS_FILE, "w") as f:
        json.dump(tokens, f, indent=2)
    # Secure the file
    os.chmod(TOKENS_FILE, 0o600)


def get_token_for_server(server_name):
    """Get stored OAuth token for a server."""
    tokens = load_tokens()
    if server_name not in tokens:
        return None

    token_data = tokens[server_name]

    # Check if token is expired
    if "expires_at" in token_data:
        if time.time() > token_data["expires_at"] - 60:  # 60s buffer
            # Try to refresh
            if "refresh_token" in token_data:
                new_token = refresh_oauth_token(server_name, token_data)
                if new_token:
                    return new_token.get("access_token")
            return None

    return token_data.get("access_token")


def refresh_oauth_token(server_name, token_data):
    """Refresh an OAuth token."""
    config = load_config()
    server_config = config.get("servers", {}).get(server_name, {})
    oauth_config = server_config.get("oauth", {})

    if not oauth_config.get("token_url"):
        return None

    try:
        with httpx.Client(timeout=30.0) as client:
            response = client.post(
                oauth_config["token_url"],
                data={
                    "grant_type": "refresh_token",
                    "refresh_token": token_data["refresh_token"],
                    "client_id": oauth_config.get("client_id", "mcp-cli"),
                },
                headers={"Content-Type": "application/x-www-form-urlencoded"}
            )

            if response.status_code == 200:
                new_token_data = response.json()
                new_token_data["expires_at"] = time.time() + new_token_data.get("expires_in", 3600)

                # Preserve refresh token if not returned
                if "refresh_token" not in new_token_data:
                    new_token_data["refresh_token"] = token_data["refresh_token"]

                tokens = load_tokens()
                tokens[server_name] = new_token_data
                save_tokens(tokens)
                return new_token_data
    except Exception:
        pass

    return None


class OAuthCallbackHandler(BaseHTTPRequestHandler):
    """HTTP handler to catch OAuth callback."""

    auth_code = None
    state = None
    error = None

    def do_GET(self):
        """Handle OAuth callback."""
        parsed = urlparse(self.path)
        params = parse_qs(parsed.query)

        if "code" in params:
            OAuthCallbackHandler.auth_code = params["code"][0]
            OAuthCallbackHandler.state = params.get("state", [None])[0]
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(b"""
                <html><body style="font-family: system-ui; text-align: center; padding: 50px;">
                <h1>Authorization Successful!</h1>
                <p>You can close this window and return to your terminal.</p>
                </body></html>
            """)
        elif "error" in params:
            OAuthCallbackHandler.error = params["error"][0]
            self.send_response(400)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(f"""
                <html><body style="font-family: system-ui; text-align: center; padding: 50px;">
                <h1>Authorization Failed</h1>
                <p>Error: {params['error'][0]}</p>
                </body></html>
            """.encode())
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        """Suppress HTTP logging."""
        pass


def discover_oauth_endpoints(server_url):
    """Discover OAuth endpoints from MCP server using RFC 9728."""
    parsed = urlparse(server_url)
    base_url = f"{parsed.scheme}://{parsed.netloc}"

    print(f"Discovering OAuth configuration for {base_url}...")

    try:
        with httpx.Client(timeout=30.0, follow_redirects=True) as client:
            # Step 1: Try to get protected resource metadata
            # First try path-aware well-known
            well_known_urls = [
                f"{base_url}/.well-known/oauth-protected-resource{parsed.path}",
                f"{base_url}/.well-known/oauth-protected-resource",
            ]

            resource_metadata = None
            for url in well_known_urls:
                try:
                    resp = client.get(url, headers={"Accept": "application/json"})
                    if resp.status_code == 200:
                        resource_metadata = resp.json()
                        print(f"  Found resource metadata at {url}")
                        break
                except Exception:
                    continue

            if not resource_metadata:
                # Try getting 401 to extract WWW-Authenticate
                resp = client.post(
                    server_url,
                    json={"jsonrpc": "2.0", "method": "initialize", "id": "1"},
                    headers=DEFAULT_HEADERS
                )
                if resp.status_code == 401:
                    www_auth = resp.headers.get("WWW-Authenticate", "")
                    if "resource_metadata=" in www_auth:
                        import re
                        match = re.search(r'resource_metadata="([^"]+)"', www_auth)
                        if match:
                            metadata_url = match.group(1)
                            resp = client.get(metadata_url)
                            if resp.status_code == 200:
                                resource_metadata = resp.json()

            if not resource_metadata:
                print("  Could not discover OAuth metadata")
                return None

            # Step 2: Get authorization server from resource metadata
            auth_servers = resource_metadata.get("authorization_servers", [])
            if not auth_servers:
                print("  No authorization servers found in metadata")
                return None

            auth_server_issuer = auth_servers[0]
            print(f"  Authorization server: {auth_server_issuer}")

            # Step 3: Discover auth server endpoints
            parsed_issuer = urlparse(auth_server_issuer)
            auth_well_known_urls = [
                f"{parsed_issuer.scheme}://{parsed_issuer.netloc}/.well-known/oauth-authorization-server{parsed_issuer.path}",
                f"{parsed_issuer.scheme}://{parsed_issuer.netloc}/.well-known/oauth-authorization-server",
                f"{parsed_issuer.scheme}://{parsed_issuer.netloc}/.well-known/openid-configuration",
            ]

            auth_metadata = None
            for url in auth_well_known_urls:
                try:
                    resp = client.get(url, headers={"Accept": "application/json"})
                    if resp.status_code == 200:
                        auth_metadata = resp.json()
                        print(f"  Found auth server metadata")
                        break
                except Exception:
                    continue

            if not auth_metadata:
                print("  Could not discover auth server metadata")
                return None

            return {
                "auth_url": auth_metadata.get("authorization_endpoint"),
                "token_url": auth_metadata.get("token_endpoint"),
                "registration_url": auth_metadata.get("registration_endpoint"),
                "scopes": resource_metadata.get("scopes_supported", []),
                "resource": server_url,
            }

    except Exception as e:
        print(f"  Discovery error: {e}")
        return None


def do_dynamic_client_registration(registration_url, redirect_uri, scopes=None):
    """Register client dynamically per RFC 7591."""
    print(f"Performing dynamic client registration...")

    try:
        with httpx.Client(timeout=30.0) as client:
            registration_data = {
                "client_name": "mcp-cli",
                "redirect_uris": [redirect_uri],
                "grant_types": ["authorization_code", "refresh_token"],
                "response_types": ["code"],
            }
            # Include scopes in registration if provided
            if scopes:
                registration_data["scope"] = scopes

            response = client.post(
                registration_url,
                json=registration_data,
                headers={"Content-Type": "application/json"}
            )

            if response.status_code in (200, 201):
                data = response.json()
                client_id = data.get("client_id")
                client_secret = data.get("client_secret")
                print(f"  Registered client: {client_id}")
                # Return both client_id and client_secret
                return {"client_id": client_id, "client_secret": client_secret}
            else:
                print(f"  Registration failed: {response.status_code}")
                print(f"  Response: {response.text}")
                return None

    except Exception as e:
        print(f"  Registration error: {e}")
        return None


def load_client_registrations():
    """Load registered client IDs."""
    reg_file = CONFIG_DIR / "registrations.json"
    if not reg_file.exists():
        return {}
    with open(reg_file) as f:
        return json.load(f)


def save_client_registration(server_name, client_data):
    """Save a client registration (client_id and optionally client_secret)."""
    reg_file = CONFIG_DIR / "registrations.json"
    registrations = load_client_registrations()
    registrations[server_name] = client_data
    CONFIG_DIR.mkdir(exist_ok=True)
    with open(reg_file, "w") as f:
        json.dump(registrations, f, indent=2)
    # Secure the file since it may contain secrets
    os.chmod(reg_file, 0o600)


def do_oauth_flow(server_name, server_config):
    """Perform OAuth authorization flow."""
    oauth_config = server_config.get("oauth", {})

    # Try auto-discovery if no oauth config
    if not oauth_config or not oauth_config.get("auth_url"):
        print("No OAuth config found, attempting auto-discovery...")
        discovered = discover_oauth_endpoints(server_config["url"])
        if discovered:
            oauth_config = discovered
        else:
            print(f"Error: Could not discover OAuth endpoints for '{server_name}'")
            print("Add 'oauth' section to server config with auth_url, token_url")
            return False

    # Required OAuth config
    auth_url = oauth_config.get("auth_url")
    token_url = oauth_config.get("token_url")
    registration_url = oauth_config.get("registration_url")
    resource = oauth_config.get("resource", server_config["url"])

    # Handle scopes - prefer explicit config, otherwise request all available
    # Note: read_only=true in URL is enforced server-side, not by filtering scopes
    all_scopes = oauth_config.get("scopes", [])
    configured_scope = oauth_config.get("scope", "") or server_config.get("scope", "")

    if configured_scope:
        scope = configured_scope
    else:
        scope = " ".join(all_scopes)

    if not auth_url or not token_url:
        print("Error: OAuth config requires 'auth_url' and 'token_url'")
        return False

    # Callback setup (needed for registration)
    callback_port = 8085
    redirect_uri = f"http://localhost:{callback_port}/callback"

    # Get or create client credentials
    client_id = oauth_config.get("client_id")
    client_secret = oauth_config.get("client_secret")

    if not client_id:
        # Check if we have a registered client
        registrations = load_client_registrations()
        reg_data = registrations.get(server_name)
        if reg_data:
            if isinstance(reg_data, dict):
                client_id = reg_data.get("client_id")
                client_secret = reg_data.get("client_secret")
            else:
                # Legacy format - just client_id string
                client_id = reg_data

    if not client_id and registration_url:
        # Try dynamic registration with scopes
        reg_result = do_dynamic_client_registration(registration_url, redirect_uri, scopes=scope)
        if reg_result:
            client_id = reg_result.get("client_id")
            client_secret = reg_result.get("client_secret")
            save_client_registration(server_name, reg_result)

    if not client_id:
        print("Error: No client_id and dynamic registration failed")
        print("Either configure 'client_id' in oauth config or create an OAuth app manually")
        return False

    # Generate PKCE challenge
    code_verifier = secrets.token_urlsafe(32)
    code_challenge = base64.urlsafe_b64encode(
        hashlib.sha256(code_verifier.encode()).digest()
    ).decode().rstrip("=")

    # Generate state for CSRF protection
    state = secrets.token_urlsafe(16)

    # Build auth URL
    auth_params = {
        "response_type": "code",
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "state": state,
        "code_challenge": code_challenge,
        "code_challenge_method": "S256",
        "resource": resource,  # RFC 8707 - required by MCP
    }
    if scope:
        auth_params["scope"] = scope

    full_auth_url = f"{auth_url}?{urlencode(auth_params)}"

    # Reset handler state
    OAuthCallbackHandler.auth_code = None
    OAuthCallbackHandler.state = None
    OAuthCallbackHandler.error = None

    # Start server in background
    server = HTTPServer(("localhost", callback_port), OAuthCallbackHandler)
    server_thread = threading.Thread(target=server.handle_request)
    server_thread.start()

    print(f"Opening browser for authorization...")
    print(f"If browser doesn't open, visit: {full_auth_url}")
    webbrowser.open(full_auth_url)

    # Wait for callback (timeout 120s)
    server_thread.join(timeout=120)
    server.server_close()

    if OAuthCallbackHandler.error:
        print(f"Authorization error: {OAuthCallbackHandler.error}")
        return False

    if not OAuthCallbackHandler.auth_code:
        print("Authorization timed out or was cancelled")
        return False

    if OAuthCallbackHandler.state != state:
        print("State mismatch - possible CSRF attack")
        return False

    # Exchange code for token
    print("Exchanging authorization code for token...")
    try:
        with httpx.Client(timeout=30.0) as client:
            token_data_request = {
                "grant_type": "authorization_code",
                "code": OAuthCallbackHandler.auth_code,
                "redirect_uri": redirect_uri,
                "client_id": client_id,
                "code_verifier": code_verifier,
            }
            # Add client_secret if we have one (confidential client)
            if client_secret:
                token_data_request["client_secret"] = client_secret

            response = client.post(
                token_url,
                data=token_data_request,
                headers={"Content-Type": "application/x-www-form-urlencoded"}
            )

            if response.status_code not in (200, 201):
                print(f"Token exchange failed: {response.status_code}")
                print(response.text)
                return False

            token_data = response.json()
            token_data["expires_at"] = time.time() + token_data.get("expires_in", 3600)

            # Save token
            tokens = load_tokens()
            tokens[server_name] = token_data
            save_tokens(tokens)

            print(f"Authorization successful! Token saved for '{server_name}'")
            return True

    except Exception as e:
        print(f"Token exchange error: {e}")
        return False


def ok(data):
    """Return success response."""
    print(json.dumps({"ok": True, "data": data}, indent=2))
    sys.exit(0)


def err(code, message):
    """Return error response."""
    print(json.dumps({"ok": False, "error": {"code": code, "message": message}}, indent=2))
    sys.exit(1)


def parse_sse_response(text):
    """Parse SSE response to extract JSON data."""
    lines = text.strip().split('\n')
    for line in lines:
        if line.startswith('data:'):
            data_str = line[5:].strip()
            if data_str:
                try:
                    return json.loads(data_str)
                except json.JSONDecodeError:
                    continue
    # Try parsing as plain JSON
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


def mcp_request(server_config, method, params=None, session_id=None, oauth_token=None):
    """Make an MCP JSON-RPC request."""
    url = server_config["url"]
    headers = {**DEFAULT_HEADERS}

    # Add server-specific headers (e.g., Authorization)
    if "headers" in server_config:
        headers.update(server_config["headers"])

    # Add OAuth token if available (overrides static headers)
    if oauth_token:
        headers["Authorization"] = f"Bearer {oauth_token}"

    # Add session ID if we have one
    if session_id:
        headers["Mcp-Session-Id"] = session_id

    payload = {
        "jsonrpc": "2.0",
        "method": method,
        "id": str(uuid.uuid4()),
    }
    if params:
        payload["params"] = params

    try:
        with httpx.Client(timeout=30.0) as client:
            response = client.post(url, json=payload, headers=headers)

            # Extract session ID from response headers
            new_session_id = response.headers.get("Mcp-Session-Id")

            # Parse response (might be SSE or JSON)
            content_type = response.headers.get("content-type", "")

            if "text/event-stream" in content_type:
                result = parse_sse_response(response.text)
            else:
                result = response.json()

            return result, new_session_id

    except httpx.TimeoutException:
        return {"error": {"code": -32000, "message": "Request timeout"}}, None
    except httpx.RequestError as e:
        return {"error": {"code": -32000, "message": f"Request failed: {str(e)}"}}, None


def initialize_session(server_name, server_config, oauth_token=None):
    """Initialize MCP session and get session ID."""
    sessions = load_sessions()

    # Check if we have a valid session
    if server_name in sessions:
        return sessions[server_name]

    # Initialize new session
    result, session_id = mcp_request(
        server_config,
        "initialize",
        {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "mcp-cli", "version": "0.1.0"}
        },
        oauth_token=oauth_token
    )

    if result is None or "error" in result:
        return None

    # Use session ID from response header or generate one
    if session_id:
        sessions[server_name] = session_id
        save_sessions(sessions)
        return session_id

    return None


def list_tools(server_name, server_config):
    """List available tools on an MCP server."""
    oauth_token = get_token_for_server(server_name)
    session_id = initialize_session(server_name, server_config, oauth_token=oauth_token)

    result, _ = mcp_request(server_config, "tools/list", session_id=session_id, oauth_token=oauth_token)

    if "error" in result:
        err("MCP_ERROR", result["error"].get("message", "Unknown error"))

    if "result" in result:
        tools = result["result"].get("tools", [])
        ok({
            "server": server_name,
            "tools": [
                {
                    "name": t.get("name"),
                    "description": t.get("description", ""),
                    "parameters": t.get("inputSchema", {})
                }
                for t in tools
            ]
        })
    else:
        err("PARSE_ERROR", "Unexpected response format")


def call_tool(server_name, server_config, tool_name, arguments):
    """Call a tool on an MCP server."""
    oauth_token = get_token_for_server(server_name)
    session_id = initialize_session(server_name, server_config, oauth_token=oauth_token)

    result, _ = mcp_request(
        server_config,
        "tools/call",
        {"name": tool_name, "arguments": arguments},
        session_id=session_id,
        oauth_token=oauth_token
    )

    if "error" in result:
        err("MCP_ERROR", result["error"].get("message", "Unknown error"))

    if "result" in result:
        ok({
            "server": server_name,
            "tool": tool_name,
            "result": result["result"]
        })
    else:
        err("PARSE_ERROR", "Unexpected response format")


def list_servers():
    """List configured servers."""
    config = load_config()
    servers = config.get("servers", {})

    ok({
        "servers": [
            {"name": name, "url": cfg.get("url", ""), "has_auth": "headers" in cfg}
            for name, cfg in servers.items()
        ]
    })


def init_config():
    """Initialize config directory and file."""
    CONFIG_DIR.mkdir(exist_ok=True)
    if not CONFIG_FILE.exists():
        default_config = {
            "servers": {
                "example": {
                    "url": "https://mcp.example.com",
                    "headers": {
                        "Authorization": "Bearer YOUR_TOKEN"
                    }
                }
            }
        }
        with open(CONFIG_FILE, "w") as f:
            json.dump(default_config, f, indent=2)
        print(f"Created config file: {CONFIG_FILE}")
        print("Edit this file to add your MCP servers.")
    else:
        print(f"Config already exists: {CONFIG_FILE}")


# =============================================================================
# Daemon Mode - Keep connections alive for fast queries
# =============================================================================

class MCPDaemon:
    """Long-running daemon that keeps MCP connections alive."""

    def __init__(self):
        self.clients = {}  # server_name -> httpx.Client
        self.sessions = {}  # server_name -> session_id
        self.tools_cache = {}  # server_name -> {"tools": [...], "expires": timestamp}
        self.tokens = {}  # server_name -> token
        self.config = load_config()
        self.running = False

    def get_client(self, server_name):
        """Get or create persistent httpx client for a server."""
        if server_name not in self.clients:
            self.clients[server_name] = httpx.Client(timeout=30.0)
        return self.clients[server_name]

    def get_token(self, server_name):
        """Get OAuth token, refreshing if needed."""
        if server_name not in self.tokens:
            self.tokens[server_name] = get_token_for_server(server_name)
        return self.tokens[server_name]

    def get_session(self, server_name, server_config):
        """Get or initialize MCP session."""
        if server_name not in self.sessions:
            oauth_token = self.get_token(server_name)
            session_id = initialize_session(server_name, server_config, oauth_token=oauth_token)
            if session_id:
                self.sessions[server_name] = session_id
        return self.sessions.get(server_name)

    def get_tools(self, server_name, server_config):
        """Get tools with caching."""
        now = time.time()
        if server_name in self.tools_cache:
            cached = self.tools_cache[server_name]
            if now < cached["expires"]:
                return cached["tools"]

        # Fetch fresh tools
        oauth_token = self.get_token(server_name)
        session_id = self.get_session(server_name, server_config)
        client = self.get_client(server_name)

        url = server_config["url"]
        headers = {**DEFAULT_HEADERS}
        if "headers" in server_config:
            headers.update(server_config["headers"])
        if oauth_token:
            headers["Authorization"] = f"Bearer {oauth_token}"
        if session_id:
            headers["Mcp-Session-Id"] = session_id

        payload = {
            "jsonrpc": "2.0",
            "method": "tools/list",
            "id": str(uuid.uuid4()),
        }

        try:
            response = client.post(url, json=payload, headers=headers)
            content_type = response.headers.get("content-type", "")
            if "text/event-stream" in content_type:
                result = parse_sse_response(response.text)
            else:
                result = response.json()

            if "result" in result:
                tools = result["result"].get("tools", [])
                self.tools_cache[server_name] = {
                    "tools": tools,
                    "expires": now + TOOLS_CACHE_TTL
                }
                return tools
        except Exception as e:
            pass
        return []

    def call_tool(self, server_name, server_config, tool_name, arguments):
        """Call a tool using persistent connection."""
        oauth_token = self.get_token(server_name)
        session_id = self.get_session(server_name, server_config)
        client = self.get_client(server_name)

        url = server_config["url"]
        headers = {**DEFAULT_HEADERS}
        if "headers" in server_config:
            headers.update(server_config["headers"])
        if oauth_token:
            headers["Authorization"] = f"Bearer {oauth_token}"
        if session_id:
            headers["Mcp-Session-Id"] = session_id

        payload = {
            "jsonrpc": "2.0",
            "method": "tools/call",
            "id": str(uuid.uuid4()),
            "params": {"name": tool_name, "arguments": arguments}
        }

        try:
            response = client.post(url, json=payload, headers=headers)
            content_type = response.headers.get("content-type", "")
            if "text/event-stream" in content_type:
                result = parse_sse_response(response.text)
            else:
                result = response.json()
            return result
        except Exception as e:
            return {"error": {"code": -32000, "message": str(e)}}

    def handle_command(self, cmd):
        """Handle a daemon command."""
        action = cmd.get("action")
        server_name = cmd.get("server")

        servers = self.config.get("servers", {})

        if action == "ping":
            return {"ok": True, "data": "pong"}

        if action == "reload":
            self.config = load_config()
            return {"ok": True, "data": "config reloaded"}

        if action == "servers":
            return {"ok": True, "data": {
                "servers": [
                    {"name": name, "url": cfg.get("url", "")}
                    for name, cfg in servers.items()
                ]
            }}

        if server_name not in servers:
            return {"ok": False, "error": {"code": "NOT_FOUND", "message": f"Server '{server_name}' not configured"}}

        server_config = servers[server_name]

        if action == "tools":
            tools = self.get_tools(server_name, server_config)
            return {"ok": True, "data": {
                "server": server_name,
                "tools": [
                    {"name": t.get("name"), "description": t.get("description", ""), "parameters": t.get("inputSchema", {})}
                    for t in tools
                ]
            }}

        if action == "call":
            tool_name = cmd.get("tool")
            arguments = cmd.get("arguments", {})
            result = self.call_tool(server_name, server_config, tool_name, arguments)
            if "error" in result:
                return {"ok": False, "error": {"code": "MCP_ERROR", "message": result["error"].get("message", "Unknown error")}}
            if "result" in result:
                return {"ok": True, "data": {"server": server_name, "tool": tool_name, "result": result["result"]}}
            return {"ok": False, "error": {"code": "PARSE_ERROR", "message": "Unexpected response"}}

        return {"ok": False, "error": {"code": "UNKNOWN_ACTION", "message": f"Unknown action: {action}"}}

    def cleanup(self):
        """Close all connections."""
        for client in self.clients.values():
            try:
                client.close()
            except Exception:
                pass
        self.clients.clear()

    def run(self):
        """Run the daemon server."""
        CONFIG_DIR.mkdir(exist_ok=True)

        # Remove stale socket
        if SOCKET_PATH.exists():
            SOCKET_PATH.unlink()

        # Write PID file
        with open(PID_FILE, "w") as f:
            f.write(str(os.getpid()))

        # Create Unix socket
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        server.bind(str(SOCKET_PATH))
        server.listen(5)
        server.settimeout(1.0)  # Allow checking self.running

        self.running = True

        def cleanup_on_exit():
            self.cleanup()
            if SOCKET_PATH.exists():
                SOCKET_PATH.unlink()
            if PID_FILE.exists():
                PID_FILE.unlink()

        atexit.register(cleanup_on_exit)

        def handle_signal(signum, frame):
            self.running = False

        signal.signal(signal.SIGTERM, handle_signal)
        signal.signal(signal.SIGINT, handle_signal)

        print(f"MCP daemon started (pid {os.getpid()})")
        print(f"Socket: {SOCKET_PATH}")

        while self.running:
            try:
                conn, _ = server.accept()
                try:
                    data = b""
                    while True:
                        chunk = conn.recv(4096)
                        if not chunk:
                            break
                        data += chunk
                        # Check for complete JSON
                        try:
                            cmd = json.loads(data.decode())
                            break
                        except json.JSONDecodeError:
                            continue

                    if data:
                        cmd = json.loads(data.decode())
                        if cmd.get("action") == "shutdown":
                            self.running = False
                            response = {"ok": True, "data": "shutting down"}
                        else:
                            response = self.handle_command(cmd)
                        conn.sendall(json.dumps(response).encode())
                finally:
                    conn.close()
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"Daemon error: {e}", file=sys.stderr)

        server.close()
        cleanup_on_exit()
        print("MCP daemon stopped")


def is_daemon_running():
    """Check if daemon is running."""
    if not SOCKET_PATH.exists():
        return False
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(str(SOCKET_PATH))
        sock.sendall(json.dumps({"action": "ping"}).encode())
        response = sock.recv(4096)
        sock.close()
        return json.loads(response.decode()).get("ok", False)
    except Exception:
        return False


def daemon_send(cmd):
    """Send command to daemon and return response."""
    if not SOCKET_PATH.exists():
        return {"ok": False, "error": {"code": "DAEMON_NOT_RUNNING", "message": "Daemon not running. Start with --daemon"}}
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(30.0)
        sock.connect(str(SOCKET_PATH))
        sock.sendall(json.dumps(cmd).encode())
        data = b""
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            data += chunk
            try:
                result = json.loads(data.decode())
                break
            except json.JSONDecodeError:
                continue
        sock.close()
        return json.loads(data.decode())
    except Exception as e:
        return {"ok": False, "error": {"code": "DAEMON_ERROR", "message": str(e)}}


def daemon_start():
    """Start daemon in background."""
    if is_daemon_running():
        print("Daemon already running")
        return

    # Fork to background
    pid = os.fork()
    if pid > 0:
        # Parent - wait briefly and check if daemon started
        time.sleep(0.5)
        if is_daemon_running():
            print(f"Daemon started (pid {pid})")
        else:
            print("Failed to start daemon")
        return

    # Child - become daemon
    os.setsid()
    os.chdir("/")

    # Redirect stdout/stderr to log
    log_file = CONFIG_DIR / "daemon.log"
    sys.stdout = open(log_file, "a")
    sys.stderr = sys.stdout

    daemon = MCPDaemon()
    daemon.run()
    sys.exit(0)


def daemon_stop():
    """Stop the daemon."""
    if not is_daemon_running():
        print("Daemon not running")
        return

    response = daemon_send({"action": "shutdown"})
    if response.get("ok"):
        print("Daemon stopped")
    else:
        print(f"Error: {response.get('error', {}).get('message', 'Unknown')}")


def daemon_query(server_name, tool_name, arguments):
    """Query daemon for tool call."""
    response = daemon_send({
        "action": "call",
        "server": server_name,
        "tool": tool_name,
        "arguments": arguments
    })
    print(json.dumps(response, indent=2))
    sys.exit(0 if response.get("ok") else 1)


def daemon_tools(server_name):
    """List tools via daemon."""
    response = daemon_send({
        "action": "tools",
        "server": server_name
    })
    print(json.dumps(response, indent=2))
    sys.exit(0 if response.get("ok") else 1)


def main():
    parser = argparse.ArgumentParser(
        description="MCP CLI - Call MCP servers on-demand",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  mcp-cli --servers                          # List configured servers
  mcp-cli --tools betterstack                # List tools on betterstack
  mcp-cli --call betterstack search_logs '{"query": "error"}'
  mcp-cli --init                             # Create config file

Daemon mode (fast queries):
  mcp-cli --daemon                           # Start daemon in background
  mcp-cli --query betterstack telemetry_query '{"query": "..."}'  # Fast query
  mcp-cli --daemon-tools betterstack         # List tools via daemon
  mcp-cli --daemon-stop                      # Stop daemon
        """
    )

    parser.add_argument("--servers", action="store_true", help="List configured servers")
    parser.add_argument("--tools", metavar="SERVER", help="List tools on a server")
    parser.add_argument("--call", nargs=3, metavar=("SERVER", "TOOL", "ARGS"),
                        help="Call a tool: --call <server> <tool> '<json-args>'")
    parser.add_argument("--auth", metavar="SERVER", help="OAuth login for a server")
    parser.add_argument("--init", action="store_true", help="Initialize config file")
    parser.add_argument("--clear-sessions", action="store_true", help="Clear cached sessions")
    parser.add_argument("--clear-tokens", action="store_true", help="Clear stored OAuth tokens")

    # Daemon mode arguments
    parser.add_argument("--daemon", action="store_true", help="Start daemon in background")
    parser.add_argument("--daemon-stop", action="store_true", help="Stop the daemon")
    parser.add_argument("--daemon-status", action="store_true", help="Check daemon status")
    parser.add_argument("--query", nargs=3, metavar=("SERVER", "TOOL", "ARGS"),
                        help="Fast query via daemon: --query <server> <tool> '<json-args>'")
    parser.add_argument("--daemon-tools", metavar="SERVER", help="List tools via daemon")

    args = parser.parse_args()

    if args.init:
        init_config()
        return

    if args.clear_sessions:
        if SESSION_FILE.exists():
            SESSION_FILE.unlink()
            print("Sessions cleared.")
        return

    if args.clear_tokens:
        if TOKENS_FILE.exists():
            TOKENS_FILE.unlink()
            print("OAuth tokens cleared.")
        return

    # Daemon mode handlers
    if args.daemon:
        daemon_start()
        return

    if args.daemon_stop:
        daemon_stop()
        return

    if args.daemon_status:
        if is_daemon_running():
            print("Daemon is running")
            if PID_FILE.exists():
                print(f"PID: {PID_FILE.read_text().strip()}")
        else:
            print("Daemon is not running")
        return

    if args.query:
        server_name, tool_name, args_json = args.query
        try:
            arguments = json.loads(args_json)
        except json.JSONDecodeError as e:
            err("INVALID_JSON", f"Invalid JSON arguments: {e}")
        daemon_query(server_name, tool_name, arguments)
        return

    if args.daemon_tools:
        daemon_tools(args.daemon_tools)
        return

    config = load_config()
    servers = config.get("servers", {})

    if args.auth:
        server_name = args.auth
        if server_name not in servers:
            print(f"Error: Server '{server_name}' not configured.")
            return
        success = do_oauth_flow(server_name, servers[server_name])
        sys.exit(0 if success else 1)

    if args.servers:
        list_servers()
        return

    if args.tools:
        server_name = args.tools
        if server_name not in servers:
            err("NOT_FOUND", f"Server '{server_name}' not configured. Run --servers to list.")
        list_tools(server_name, servers[server_name])
        return

    if args.call:
        server_name, tool_name, args_json = args.call
        if server_name not in servers:
            err("NOT_FOUND", f"Server '{server_name}' not configured. Run --servers to list.")
        try:
            arguments = json.loads(args_json)
        except json.JSONDecodeError as e:
            err("INVALID_JSON", f"Invalid JSON arguments: {e}")
        call_tool(server_name, servers[server_name], tool_name, arguments)
        return

    parser.print_help()


if __name__ == "__main__":
    main()
