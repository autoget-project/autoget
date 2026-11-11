# AutoGet

A comprehensive torrent management system that unifies access to multiple torrent indexers, manages downloads, and integrates with file organization services.

AutoGet consists of a modern web frontend and Go backend service that allows you to search, download, and organize torrents from various indexers (M-Team, Nyaa, Sukebei) through a single interface.

## Features

- **Multi-Indexer Search**: Search across multiple torrent sources with advanced filtering
- **Smart Download Management**: Control Transmission downloads with progress tracking and seeding policies
- **Modern Web Interface**: Responsive frontend built with Lit, TypeScript, and Tailwind CSS
- **Automatic File Organization**: Integrate with organizer service to manage downloaded files
- **RSS Monitoring**: Automated torrent discovery with cron-based scheduling
- **RESTful API**: Complete API for automation and third-party integration

## Details

For detailed setup and API documentation, see:
- [Frontend README](frontend/README.md)
- [Backend README](backend/README.md)

## Authentication

The backend requires an `auth` section in its config and refuses to start without one. Configure OIDC login, or explicitly opt out with `auth: {disabled: true}` (the API is then unauthenticated). See [Backend README](backend/README.md#oauthoidc-login-auth) for provider requirements and the token-storage trade-off.
