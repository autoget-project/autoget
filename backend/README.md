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
- Go 1.25+
- PostgreSQL
- Transmission (for downloader functionality)

### Configuare

See `example.config.yaml`.

## API Documentation

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
GET /indexers/{indexer}/resources/{resource_id}/download
```

### Downloader Endpoints

#### List Downloaders
```http
GET /downloaders
```

#### Get Downloader Statuses
```http
GET /downloaders/{downloader}?state={state}
```

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
