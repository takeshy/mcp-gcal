# mcp-gcal

A standalone MCP (Model Context Protocol) server for Google Calendar and Gmail. Supports two modes:

- **Stdio mode** (single-user): Direct MCP client integration via JSON-RPC over stdin/stdout
- **HTTP mode** (multi-user): HTTP server with per-user Google OAuth authentication

[日本語版 README](README_ja.md)

## Setup

### 1. Create Google Cloud Credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (or use an existing one)
3. Enable the **Google Calendar API** and **Gmail API**
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
2. Redirected to Google OAuth consent screen (with `calendar` + `gmail.modify` + `userinfo.email` scopes)
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

### Calendar Tools

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
| `show-calendar` | Interactive calendar UI (MCP Apps) | (none) |

### Gmail Tools

| Tool | Description | Required Parameters |
|---|---|---|
| `search-emails` | Search emails using Gmail query syntax | `query` |
| `read-email` | Read full content of an email | `message_id` |
| `send-email` | Send an email | `to`, `subject`, `body` |
| `draft-email` | Create a draft email | `to`, `subject`, `body` |
| `modify-email` | Add or remove labels on an email | `message_id` |
| `delete-email` | Move an email to trash | `message_id` |
| `list-email-labels` | List all Gmail labels | (none) |

### MCP Apps UI

The `show-calendar` tool supports [MCP Apps](https://github.com/anthropics/mcp-apps) UI. When used with a compatible MCP client, it renders an interactive calendar view with the ability to browse, add, and delete events.

## Deployment (GCP)

Deploy to Google Cloud Run with HTTPS load balancer and automatic CI/CD.

### Prerequisites

- [Terraform](https://www.terraform.io/) installed
- `gcloud` CLI authenticated
- OAuth credentials JSON file (Web application type)

### Initial Setup

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your project settings
cp /path/to/your/credentials.json credentials.json

terraform init
terraform apply
```

Build and push the initial Docker image:

```bash
cd ..
gcloud builds submit \
  --tag=asia-northeast1-docker.pkg.dev/YOUR_PROJECT_ID/mcp-gcal/mcp-gcal:latest \
  --project=YOUR_PROJECT_ID
```

Then apply again to create the Cloud Run service:

```bash
cd terraform
terraform apply
```

### CI/CD

A Cloud Build trigger is configured to automatically build and deploy on every push to `main`:

1. Builds Docker image with `SHORT_SHA` and `latest` tags
2. Pushes to Artifact Registry
3. Deploys new revision to Cloud Run

Set up the trigger by connecting your GitHub repository via Cloud Build:

```bash
# Create GitHub connection (requires browser OAuth)
gcloud builds connections create github CONNECTION_NAME \
  --region=asia-northeast1 --project=YOUR_PROJECT_ID

# Link repository
gcloud builds repositories create mcp-gcal \
  --connection=CONNECTION_NAME \
  --remote-uri=https://github.com/OWNER/mcp-gcal.git \
  --region=asia-northeast1 --project=YOUR_PROJECT_ID

# Create trigger
gcloud builds triggers create github \
  --name=mcp-gcal-deploy \
  --repository=projects/YOUR_PROJECT_ID/locations/asia-northeast1/connections/CONNECTION_NAME/repositories/mcp-gcal \
  --branch-pattern='^main$' \
  --build-config=cloudbuild.yaml \
  --region=asia-northeast1 \
  --project=YOUR_PROJECT_ID \
  --service-account=projects/YOUR_PROJECT_ID/serviceAccounts/YOUR_BUILD_SA
```

### Infrastructure

| Resource | Description |
|---|---|
| Cloud Run | Application server (max 1 instance for SQLite) |
| Artifact Registry | Docker image repository |
| Secret Manager | OAuth credentials storage |
| Global HTTPS LB | Load balancer with Google-managed SSL certificate |
| Cloud DNS | A record for custom domain |
| Cloud Build | CI/CD pipeline |

## Architecture

```
Stdio Mode (single-user):
  stdin (JSON-RPC) → Server → Tool Dispatch → Google Calendar API
                                             → Gmail API
                       ↓
                    SQLite (oauth_tokens table)

HTTP Mode (multi-user):
  Browser → /auth/login → Google OAuth → /auth/callback → Save user + token
  Client  → POST /mcp (Bearer token) → Auth middleware → Per-user Calendar/Gmail API
                                           ↓
                                        SQLite (users table)

GCP Deployment:
  GitHub push → Cloud Build → Artifact Registry → Cloud Run
  Client → Cloud DNS → HTTPS LB (Google-managed SSL) → Cloud Run
```

### Files

- **main.go** - Entry point, mode selection
- **server.go** - Stdio MCP server (single-user)
- **http.go** - HTTP MCP server (multi-user, OAuth login)
- **tools.go** - Tool definitions, shared dispatch logic
- **auth.go** - OAuth2 flow, token management
- **calendar.go** - Google Calendar API operations
- **gmail.go** - Gmail API operations
- **ui.go** - MCP Apps UI resource handling
- **db.go** - SQLite storage (single-user tokens + multi-user table)
- **templates/calendar.html** - Interactive calendar UI template
- **Dockerfile** - Multi-stage Docker build
- **cloudbuild.yaml** - Cloud Build pipeline (build, push, deploy)
- **terraform/** - GCP infrastructure as code
