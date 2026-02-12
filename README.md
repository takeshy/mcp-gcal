# mcp-gcal

A standalone MCP (Model Context Protocol) server for Google Calendar. Supports two modes:

- **Stdio mode** (single-user): Direct MCP client integration via JSON-RPC over stdin/stdout
- **HTTP mode** (multi-user): HTTP server with per-user Google OAuth authentication

## Setup

### 1. Create Google Cloud Credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (or use an existing one)
3. Enable the **Google Calendar API**
4. Go to **Credentials** → **Create Credentials** → **OAuth 2.0 Client IDs**
5. Choose **Desktop app** (stdio) or **Web application** (HTTP) as the type
6. For HTTP mode, add your callback URI (default: `http://localhost:8080/auth/callback`) as an authorized redirect URI
7. Download the credentials JSON file

### 2. Install Credentials

```bash
mkdir -p ~/.config/mcp-gcal
cp ~/Downloads/client_secret_*.json ~/.config/mcp-gcal/credentials.json
```

### 3. Build

```bash
make build
```

## Stdio Mode (Single-User)

### Authenticate

```bash
./mcp-gcal auth
```

Opens browser for Google OAuth. Token is saved to SQLite.

### Run

```bash
./mcp-gcal
```

Reads JSON-RPC 2.0 from stdin, writes to stdout. The `authenticate` tool is also available to re-auth from within an MCP client.

### Claude Desktop Configuration

```json
{
  "mcpServers": {
    "google-calendar": {
      "command": "/path/to/mcp-gcal"
    }
  }
}
```

## HTTP Mode (Multi-User)

### Run

```bash
./mcp-gcal --mode=http --addr=:8080
```

### Authentication Flow

1. User visits `http://localhost:8080/auth/login`
2. Redirected to Google OAuth consent screen (with `calendar` + `userinfo.email` scopes)
3. After authorization, redirected to `/auth/callback`
4. Server identifies user by Google email, stores token in SQLite
5. User receives an API key (displayed on the callback page)
6. A new API key is issued on each successful login

### MCP Requests

Include the API key as a Bearer token:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer gcal_xxxx..." \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

### Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/auth/login` | GET | Start Google OAuth flow |
| `/auth/callback` | GET | OAuth callback (automatic) |
| `/health` | GET | Health check |
| `/mcp` | POST | MCP JSON-RPC (requires Bearer token) |

## CLI Flags

```
--db=PATH               SQLite database path (default: ~/.config/mcp-gcal/mcp-gcal.db)
--credentials-file=PATH OAuth2 credentials JSON (default: ~/.config/mcp-gcal/credentials.json)
--mode=stdio|http       Server mode (default: stdio)
--addr=:8080            HTTP listen address (http mode only)
--base-url=URL          Public base URL for OAuth callback (http mode; default derived from --addr)
```

## Tools

| Tool | Description | Required Parameters |
|---|---|---|
| `authenticate` | Start Google OAuth2 login (stdio only) | (none) |
| `list-calendars` | List all accessible calendars | (none) |
| `list-events` | List upcoming events | (none) |
| `get-event` | Get event details | `event_id` |
| `search-events` | Search events by text | `query` |
| `create-event` | Create a new event | `summary`, `start`, `end` |
| `update-event` | Update an existing event | `event_id` |
| `delete-event` | Delete an event | `event_id` |
| `respond-to-event` | Respond to an invitation | `event_id`, `response` |

## Architecture

```
Stdio Mode (single-user):
  stdin (JSON-RPC) → Server → Tool Dispatch → Google Calendar API
                       ↓
                    SQLite (oauth_tokens table)

HTTP Mode (multi-user):
  Browser → /auth/login → Google OAuth → /auth/callback → Save user + token
  Client  → POST /mcp (Bearer token) → Auth middleware → Per-user Calendar API
                                           ↓
                                        SQLite (users table)
```

### Files

- **main.go** - Entry point, mode selection
- **server.go** - Stdio MCP server (single-user)
- **http.go** - HTTP MCP server (multi-user, OAuth login)
- **tools.go** - Tool definitions, shared dispatch logic
- **auth.go** - OAuth2 flow, token management
- **calendar.go** - Google Calendar API operations
- **db.go** - SQLite storage (single-user tokens + multi-user table)
