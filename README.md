# Dispatchoor

> **Note:** This is still a work in progress and should not be used on a production system. Use at your own risk.

A GitHub Actions workflow orchestrator that dispatches jobs based on runner availability, not blind schedules.

## Overview

Dispatchoor solves a common problem with self-hosted GitHub Actions runners: you have expensive infrastructure sitting idle while jobs queue up, or you're paying for runners that aren't being utilized efficiently.

Instead of triggering workflows on a schedule and hoping runners are available, dispatchoor:

- **Monitors** your self-hosted runner pools for availability via the GitHub API
- **Maintains** its own job queue with visibility and priority ordering
- **Dispatches** `workflow_dispatch` events only when matching runners are idle
- **Provides** a real-time dashboard for queue management and monitoring

## Features

- **Smart Dispatching**: Triggers jobs only when runners with matching labels are available
- **Queue Management**: Drag-and-drop reordering, priority support, job history
- **Real-time Updates**: WebSocket-based live updates for runner status and job state
- **Multi-group Support**: Organize runners into groups with different label requirements
- **Authentication**: Basic auth and GitHub OAuth with role-based access control
- **Metrics**: Prometheus endpoint for monitoring and alerting
- **Database Support**: SQLite (default) or PostgreSQL

## Getting Started

### Prerequisites

- Go 1.23+
- Node.js 22+
- A GitHub [PAT](https://github.com/settings/personal-access-tokens) with at least the following scopes:
  - Repo : Actions - Read/Write
  - Organization: Self-hosted runners - Read/Write

### Quick Start

1. **Clone the repository**

   ```bash
   git clone https://github.com/ethpandaops/dispatchoor.git
   cd dispatchoor
   ```

2. **Create a configuration file**

   ```bash
   cp config.example.yaml config.yaml
   ```

3. **Edit the configuration**

   Set your GitHub token and configure at least one group:

   ```yaml
   github:
     token: ${GITHUB_TOKEN}  # Or paste token directly

   auth:
     basic:
       enabled: true
       users:
         - username: admin
           password: changeme
           role: admin

   groups:
     github:
       - id: my-runners
         name: My Runners
         runner_labels:
           - self-hosted
           - linux
         workflow_dispatch_templates:
           - id: my-job
             name: My Workflow
             owner: my-org
             repo: my-repo
             workflow_id: my-workflow.yml
             ref: main
             inputs:
               param1: "value1"
   ```

4. **Set environment variables**

   ```bash
   export GITHUB_TOKEN="ghp_your_token_here"
   export ADMIN_PASSWORD="your_secure_password"
   ```

5. **Build and run**

   ```bash
   # Build everything
   make build

   # Run database migrations
   make migrate

   # Start the server
   ./bin/dispatchoor server --config config.yaml
   ```

6. **Access the dashboard**

   Open http://localhost:3001 in your browser and log in with your configured credentials.

### Using Docker

```bash
# Build the Docker image
make docker-build

# Run with your config
docker run -d \
  -p 9090:9090 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -e GITHUB_TOKEN \
  dispatchoor:latest
```

## Configuration

### Database

SQLite (default):
```yaml
database:
  driver: sqlite
  sqlite:
    path: ./dispatchoor.db
```

PostgreSQL:
```yaml
database:
  driver: postgres
  postgres:
    host: localhost
    port: 5432
    user: dispatchoor
    password: ${DB_PASSWORD}
    database: dispatchoor
    sslmode: disable
```

### Authentication

Basic auth:
```yaml
auth:
  session_ttl: 24h
  basic:
    enabled: true
    users:
      - username: admin
        password: ${ADMIN_PASSWORD}
        role: admin
      - username: viewer
        password: ${VIEWER_PASSWORD}
        role: readonly
```

GitHub OAuth:
```yaml
auth:
  github:
    enabled: true
    client_id: ${GITHUB_CLIENT_ID}
    client_secret: ${GITHUB_CLIENT_SECRET}
    allowed_orgs:
      - my-org
    role_mapping:
      my-org: admin
```

### Groups and Templates

Groups define pools of runners identified by labels. Each group can have multiple workflow dispatch templates defined inline or loaded from a separate file:

```yaml
groups:
  github:
    - id: sync-tests
      name: Sync Tests
      description: Ethereum sync testing jobs
      runner_labels:
        - self-hosted
        - sync-test
      # Option 1: Inline templates
      workflow_dispatch_templates:
        - id: sync-geth-prysm
          name: Sync Test geth/prysm
          owner: ethpandaops
          repo: syncoor-tests
          workflow_id: syncoor.yaml
          ref: master
          inputs:
            el-client: "geth"
            cl-client: "prysm"
            config: '{"network": "mainnet"}'
      # Option 2: Load templates from files (paths relative to config file)
      # workflow_dispatch_templates_files:
      #   - templates/hoodi.yaml
      #   - templates/mainnet.yaml
```

Template file format (`templates/sync-tests.yaml`):
```yaml
- id: sync-geth-prysm
  name: Sync Test geth/prysm
  owner: ethpandaops
  repo: syncoor-tests
  workflow_id: syncoor.yaml
  ref: master
  inputs:
    el-client: "geth"
    cl-client: "prysm"

- id: sync-geth-lighthouse
  name: Sync Test geth/lighthouse
  owner: ethpandaops
  repo: syncoor-tests
  workflow_id: syncoor.yaml
  ref: master
  inputs:
    el-client: "geth"
    cl-client: "lighthouse"
```

Both inline templates and file templates can be used together - file templates are appended to inline templates.

### Workflow Best Practices

When creating GitHub Actions workflows to be dispatched by dispatchoor, it's recommended to make `runs-on` and `timeout-minutes` configurable via inputs. This allows you to control runner selection and timeouts from dispatchoor without modifying the workflow file.

See [`examples/workflows/example.yaml`](examples/workflows/example.yaml) for a reference implementation:

```yaml
on:
  workflow_dispatch:
    inputs:
      runs-on:
        description: On which runner we want to run the workflow
        required: false
        default: '{"group": "your-runner-group", "labels": "XXL"}'
        type: string
      timeout-minutes:
        description: 'Timeout in minutes'
        required: false
        default: '1800'
        type: string
      message:
        description: A message that we want to print
        default: Hello world
        type: string

jobs:
  example:
    timeout-minutes: ${{ fromJSON(inputs.timeout-minutes) }}
    runs-on: ${{ fromJSON(inputs.runs-on) }}
    steps:
      - name: Print message
        run: echo "${{ inputs.message }}"
```

This pattern allows you to:
- Override which runner pool executes the job via the `runs-on` input
- Set custom timeouts per job dispatch
- Pass any additional parameters your workflow needs

### Dispatcher

```yaml
dispatcher:
  enabled: true
  interval: 10s      # How often to check for idle runners
  max_concurrent: 5  # Max concurrent jobs per group
```

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | - | Health check |
| GET | `/metrics` | - | Prometheus metrics |
| POST | `/api/v1/auth/login` | - | Login with username/password |
| GET | `/api/v1/auth/github` | - | Initiate GitHub OAuth |
| GET | `/api/v1/groups` | User | List all groups |
| GET | `/api/v1/groups/{id}` | User | Get group details |
| GET | `/api/v1/groups/{id}/queue` | User | Get queued jobs |
| POST | `/api/v1/groups/{id}/queue` | Admin | Add job to queue |
| PUT | `/api/v1/groups/{id}/queue/reorder` | Admin | Reorder queue |
| GET | `/api/v1/runners` | User | List all runners |
| DELETE | `/api/v1/jobs/{id}` | Admin | Cancel/delete job |

## Development

```bash
# Run API in development mode
make dev-api

# Run UI in development mode (separate terminal)
make dev-ui

# Run tests
make test

# Run linter
make lint
```

### UI Configuration

The UI loads its configuration from `config.json` at runtime, making it easy to deploy the same build to different environments.

**Configuration file (`ui/dist/config.json` or `ui/public/config.json`):**
```json
{
  "apiUrl": "/api/v1"
}
```

**Development (default):**
- Vite dev server runs on `http://localhost:3000`
- Proxies `/api`, `/health`, `/metrics` to `http://localhost:9090`
- Configure proxy target in `ui/vite.config.ts`

**Production (same origin):**
- Default `config.json` uses relative path `/api/v1`
- Serve UI static files and API from the same domain
- Use a reverse proxy (nginx, Caddy) or embed UI in the Go server

**Production (separate origins):**
- Update `config.json` in the deployed UI:
  ```json
  {
    "apiUrl": "https://api.example.com/api/v1"
  }
  ```
- Ensure CORS is configured on the API:
  ```yaml
  server:
    cors_origins:
      - https://ui.example.com
  ```

**Docker/Kubernetes:**
- Mount a custom `config.json` at `/app/ui/dist/config.json`
- Or use an init container to generate `config.json` from environment variables

## Architecture

```
dispatchoor/
├── cmd/dispatchoor/     # CLI entry point
├── pkg/
│   ├── api/             # HTTP server, WebSocket hub
│   ├── auth/            # Authentication (basic, GitHub OAuth)
│   ├── config/          # YAML config loader
│   ├── dispatcher/      # Core dispatch loop
│   ├── github/          # GitHub API client
│   ├── metrics/         # Prometheus metrics
│   ├── queue/           # Job queue management
│   └── store/           # Database (SQLite, PostgreSQL)
└── ui/                  # React + Tailwind frontend
```

## Metrics

Prometheus metrics are exposed at `/metrics`:

- `dispatchoor_jobs_created_total` - Jobs created by group
- `dispatchoor_jobs_completed_total` - Jobs completed by group
- `dispatchoor_jobs_failed_total` - Jobs failed by group
- `dispatchoor_queue_size` - Current queue size by group and status
- `dispatchoor_runners_online` - Online runners by group
- `dispatchoor_runners_busy` - Busy runners by group
- `dispatchoor_dispatcher_cycles_total` - Dispatcher loop cycles
- `dispatchoor_github_rate_limit_remaining` - GitHub API rate limit

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
