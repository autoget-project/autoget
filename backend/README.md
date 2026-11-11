# AutoGet Backend

A comprehensive torrent management backend service that unifies access to multiple torrent indexers, manages downloads, and integrates with file organization services.

## Overview

AutoGet Backend provides a centralized system for:
- **Torrent Indexing**: Monitor and search multiple torrent sources (M-Team, Nyaa, Sukebei)
- **Download Management**: Control downloaders with intelligent progress tracking and seeding policies
- **File Organization**: Integrate with organizer service to automatically move and organize downloaded files
- **API Interface**: RESTful API for frontend integration and automation

## Features

### 🎯 Multi-Indexer Support
- **M-Team**: Private tracker with normal and adult content
- **Nyaa**: Public anime/torrent tracker
- **Sukebei**: Public adult content tracker
- RSS feed monitoring and automatic discovery
- Category-based filtering and organization

### ⬇️ Smart Download Management
- **Transmission Integration**: Full RPC client support
- **Progress Tracking**: Real-time download progress and status updates
- **Seeding Policies**: Configurable seeding duration and upload requirements
- **Automatic File Management**: Copy files to finished directories and clean up torrents
- **State Management**: Started → Seeding → Stopped → Deleted lifecycle

### 📁 File Organization
- **Organizer Service Integration**: Plan and execute file organization workflows
- **Automatic File Movement**: Move downloaded files to appropriate directories
- **Metadata Support**: Rich metadata for better organization decisions
- **Error Handling**: Track and report failed operations

### 🔄 Automation & Scheduling
- **Cron Jobs**: Scheduled RSS monitoring and seeding checks
- **Daily Maintenance**: Automatic cleanup and status updates
- **Notification System**: Telegram integration for important events

### 🌐 RESTful API
- **Indexer Management**: List indexers, categories, and resources
- **Downloader Control**: Monitor and manage downloaders
- **Resource Discovery**: Search and download torrents
- **Status Monitoring**: Real-time status and progress tracking

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Indexers      │    │  Downloaders    │    │   Organizer     │
│                 │    │                 │    │   Service       │
│ • M-Team        │    │ • Transmission  │    │                 │
│ • Nyaa          │    │ • Progress      │    │ • Plan Files    │
│ • Sukebei       │    │ • Seeding       │    │ • Execute Moves │
│ • RSS Monitoring│    │ • File Copy     │    │ • Metadata      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────┐
                    │   Web API       │
                    │                 │
                    │ • REST Endpoints│
                    │ • Status API    │
                    │ • Image Proxy   │
                    └─────────────────┘
                                 │
                    ┌─────────────────┐
                    │   Database      │
                    │                 │
                    │ • PostgreSQL    │
                    │ • DownloadStatus│
                    │ • Progress      │
                    └─────────────────┘
```

## Quick Start

### Prerequisites
- Go 1.26+
- PostgreSQL
- Transmission (for downloader functionality)

### Configuare

See `example.config.yaml`. The `auth` section is **required**: the backend
refuses to start without it. Configure OAuth/OIDC login, or explicitly opt out
with `auth: {disabled: true}` (a loud warning is logged at startup, and the
whole API is then open to anyone who can reach the port).

> **Breaking change:** earlier versions disabled auth when the `auth` section
> was omitted. Existing configs without an `auth` section must now add
> `auth: {disabled: true}` to keep running unauthenticated.

#### Secrets and database TLS

`config.yaml` holds plaintext secrets (`pg_dsn` password, `client_secret`, mteam
`api_key`, Telegram token): `chmod 600` it, mount it read-only, keep it out of
git and images, and rotate the secrets if it leaks. Use `sslmode=require` (or
`verify-full`) in `pg_dsn` for a remote database; `sslmode=disable` is only
acceptable on a private network such as the bundled docker-compose one.

#### OAuth/OIDC login (`auth`)

```yaml
auth:
  issuer: https://auth.example.com/oauth2
  client_id: your_client_id
  client_secret: your_client_secret
  redirect_base_url: https://autoget.example.com
  # required_role: autoget # optional
```

- `issuer` must support OIDC discovery (`/.well-known/openid-configuration`).
- `required_role`: optional. If set, API requests must carry a JWT whose `roles` claim
  (space-separated string) contains this role. Omit to skip role validation. A
  valid token without the role is rejected with `403` and the browser is sent to
  `/auth/denied` rather than back through the login flow.
- Register `{redirect_base_url}/auth/callback` as an allowed redirect URI with
  the provider.
- `redirect_base_url` is the externally reachable base URL of this app. In the
  local dev setup it is `http://127.0.0.1:8880` (the Caddy entrypoint), and
  Caddy must forward `/auth/*` to the backend.
- The backend only stores the client secret and a short-lived CSRF state; tokens
  live in the browser.

To run without authentication (not recommended):

```yaml
auth:
  disabled: true
```

All other `auth` fields are ignored in that case.

#### OIDC provider requirements

The backend verifies the **access token** itself (as a JWT) on every API call,
so the provider must:

- support OIDC discovery and the `offline_access` scope (needed for refresh
  tokens; refresh uses the standard token endpoint, and rotating, single-use
  refresh tokens are supported);
- issue **JWT access tokens signed with keys from its JWKS, whose `aud` claim
  includes `client_id`**. Opaque access tokens, or JWTs with another audience,
  make every API call fail with 401;
- if `required_role` is used, put a space-separated `roles` claim in the access
  token.

Serve the app over HTTPS (`redirect_base_url: https://...`); this also marks the
login state cookie `Secure`.

#### Token storage trade-off

Access and refresh tokens are kept in the browser's `localStorage`, so any
script injection (XSS) could read the long-lived refresh token. This is a known,
accepted trade-off; moving the refresh token to an HttpOnly cookie behind a
backend-for-frontend is deferred. The mitigation is the strict
Content-Security-Policy the backend sends: do not relax it (for example, do not
add `unsafe-inline` or third-party script origins), including in a reverse proxy.

#### Refresh endpoint limits

`POST /auth/refresh` is unauthenticated, so it is rate limited: 10 requests/min
per client IP (burst 10) plus a global backstop of 60 requests/min (burst 60),
answering `429` with `Retry-After`; request bodies over 8 KiB are rejected. The
client IP comes from gin's `ClientIP()`; behind a reverse proxy make sure it
overwrites (not appends to) `X-Forwarded-For`.

## API Documentation

The full machine-readable spec (paths, parameters, request and response schemas, error codes, bearer auth) is [openapi.yaml](openapi.yaml). A test (`cmd/openapi_test.go`) fails if a registered route is missing from it. The summary below is informal.

### Base URL
```
http://localhost:8080/api/v1
```

### Indexer Endpoints

#### List Indexers
```http
GET /indexers
```

#### Get Indexer Categories
```http
GET /indexers/{indexer}/categories
```

#### List Resources
```http
GET /indexers/{indexer/resources?category={cat}&page={page}&limit={limit}
```

#### Get Resource Details
```http
GET /indexers/{indexer}/resources/{resource_id}
```

#### Download Resource
```http
POST /indexers/{indexer}/resources/{resource_id}/download
```

Returns `409 Conflict` if the resource has already been downloaded (duplicate).

### Downloader Endpoints

#### List Downloaders
```http
GET /downloaders
```

#### Get Downloader Statuses
```http
GET /downloaders/{downloader}?state={state}
```

#### Organize Download
```http
POST /download/{download_id}/organize?action={action}
```

Actions:
- `accept_plan` - Execute the organization plan using the organizer service
- `manual_organized` - Mark the download as manually organized

### Utility Endpoints

#### Image Proxy
```http
GET /image?url={encoded_url}
```

## Database Schema

The application uses PostgreSQL with the following main entity:

### DownloadStatus
- **Basic Info**: Hash, timestamps, downloader name
- **State Management**: Started, seeding, stopped, deleted
- **Progress Tracking**: Download progress, upload histories
- **Resource Metadata**: Title, category, indexer info
- **File Management**: File lists, move states
- **Organization Plans**: Organizer plans, execution states

Automatic data cleanup occurs after 30 days.

## Development

### Project Structure
```
backend/
├── cmd/               # Application entry points
├── internal/
│   ├── config/        # Configuration management
│   ├── db/            # Database models and migrations
│   ├── handlers/      # HTTP handlers
│   └── notify/        # Notification services
├── downloaders/       # Downloader implementations
├── indexers/          # Indexer implementations
├── organizer/         # Organizer service client
└── justfile           # Build and development tasks
```

### Using Just (Task Runner)

```bash
just test       # Run tests
just alltest    # Run all tests includes integration tests
```
