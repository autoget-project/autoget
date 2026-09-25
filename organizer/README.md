# AutoGet File Organizer

AutoGet Organizer is the core post-processing service in the automated media pipeline. It processes files downloaded by AutoGet (including movies, TV series, anime, books, music, photobooks, and JAV). Powered by a multi-stage LLM pipeline and authoritative metadata integrations (TMDB, MetaTube, and a local actress alias store), it intelligently categorizes, standardizes naming, and archives media into library hierarchies (such as Jellyfin-compatible structures), while providing atomic execution and human-in-the-loop replanning capabilities.

---

## Key Features

- **4-Stage Pipeline Architecture**:
  1. **Stage 1 Classifier**: Fast deterministic pattern screening (e.g., standard bango formats, audio/ebook extensions) with an LLM classification fallback for messy/ambiguous downloads.
  2. **Stage 2 Metadata Enricher**: Queries TMDB, MetaTube, and a file-locked (`flock`) local actress alias store (`actor.json`) to fetch authoritative titles and details with graceful degradation.
  3. **Stage 3 Domain Planners**: Dedicated LLM-powered domain planners (Movie, TV Series, Bango/JAV) and simple movers (Books, Music, Photobooks) generating Jellyfin-compliant relative paths via strict JSON Schema structured outputs (`temperature: 0.1`).
  4. **Stage 4 Post-Process & Safety**: Semantic subtitle-to-video pairing (language detection, ISO-639 codes, and naming synchronization), junk file skipping (`.nfo`, `.torrent`, `.url`), and pure-Go physical path traversal sanitization (blocking any `../` escapes).
- **Multi-Provider LLM Support**: Native support for Google Gemini and xAI Grok using strict Structured Outputs without free-form text parsing.
- **Physical Safety & Atomic Execution**: Aggregated error handling (individual failures do not abort legal operations) and automatic source directory isolation (`archive/`).
- **Human-in-the-Loop Replanning (`/v1/replan`)**: Allows users to correct an existing plan. Unlike the initial plan, the previous (flawed) result is supplied in a dedicated field and Stage 1 is re-run so a wrong category/domain can be fixed, while Stage 2 enrichment (authoritative metadata, canonical JAV actress directory) and Stage 4 (subtitle pairing + sanitization) still run.

---

## Getting Started

### Prerequisites

- Go 1.24+ or Docker
- [just](https://github.com/casey/just) command runner (recommended)

### Environment Variables

Configure the following environment variables before starting the service:

| Variable | Required | Default / Example | Description |
| :--- | :---: | :--- | :--- |
| `PORT` | No | `8080` | HTTP service listening port |
| `DOWNLOAD_COMPLETED_DIR` | **Yes** | `/data/downloads/completed` | Path to downloaded files directory (source) |
| `TARGET_DIR` | **Yes** | `/data/media` | Target root media library directory (must be writable) |
| `JAV_ACTOR_FILE` | **Yes** | `/data/config/actor.json` | Path to the local actress alias mapping JSON |
| `FLARESOLVERR_URL` | **Yes** | `http://flaresolverr:8191` | FlareSolverr URL for anti-scraping bypass |
| `TMDB_API_KEY` | **Yes** | `your-tmdb-api-key` | TMDB API Key |
| `TMDB_RESPONSE_LANGUAGE` | No | `zh-CN` | Preferred language for TMDB metadata |
| `METATUBE_API_URL` | **Yes** | `http://metatube:8080` | MetaTube API service endpoint |
| `METATUBE_API_KEY` | No | `your-metatube-key` | MetaTube API key (if authentication is enabled) |
| `MODEL` | **Yes** | `gemini:gemini-2.5-flash` or `xai:grok-4` | Model identifier; use `gemini:` or `xai:` prefix to explicitly select provider |
| `GEMINI_API_KEY` | Conditional | `AIzaSy...` | Required when using Gemini models |
| `XAI_API_KEY` | Conditional | `xai-...` | Required when using Grok models |
| `TPDB_API_TOKEN` | No | (empty) | ThePornDB API token; when set, the porn category queries real scene names via LLM function calling, otherwise the local naming chain is used |

> **Note**: `TARGET_DIR` must contain required media subdirectories (e.g., `movie`, `tv_series`, `jav`, `book`, `music`, `photobook`, etc.). The startup routine validates that these directories exist and are writable.

### Building & Running Locally

Run from the monorepo root:

```bash
# 1. Run quality gates (tests and linting)
just test-organizer
just lint-organizer

# 2. Build the binary
just build-organizer

# 3. Start the server
just run-organizer
```

### Running with Docker

```bash
# Build the container image (from the monorepo root)
just build-images

# Run the container
docker run -d \
  --name autoget-organizer \
  -p 8080:8080 \
  -e DOWNLOAD_COMPLETED_DIR=/downloads \
  -e TARGET_DIR=/media \
  -e JAV_ACTOR_FILE=/config/actor.json \
  -e FLARESOLVERR_URL=http://flaresolverr:8191 \
  -e TMDB_API_KEY=your-tmdb-key \
  -e METATUBE_API_URL=http://metatube:8080 \
  -e MODEL=gemini:gemini-3.5-flash-lite \
  -e GEMINI_API_KEY=your-gemini-key \
  -v /path/to/downloads:/downloads \
  -v /path/to/media:/media \
  -v /path/to/config:/config \
  ghcr.io/autoget-project/autoget-organizer:latest
```

---

## Naming Conventions & Directory Layout

Destination paths follow standard media server (Jellyfin / Emby / Plex) conventions:

- **Movies**:
  `movie/{Language}/{Title (Year)}/{Title (Year)}.{ext}`
  *(e.g., `movie/Chinese/黑客帝国 (1999)/黑客帝国 (1999).mkv`)*
- **TV Series**:
  `tv_series/{Language}/{Title (Year)}/Season {XX}/{Title (Year)} S{XX}E{YY}.{ext}`
  *(e.g., `tv_series/English/Breaking Bad (2008)/Season 01/Breaking Bad (2008) S01E01.mkv`)*
- **JAV**:
  `jav/{Actress}/{BANGO}.{ext}`
  - Multi-part: `jav/{Actress}/{BANGO}.part.1.{ext}`
  - Chinese subtitled releases: `jav/{Actress}/{BANGO}-C.{ext}`
- **Porn (Western)**:
  - ThePornDB scene hit: `porn/{Scene Title}/{Scene Title}.{ext}`; when a performer directory already exists in the library, the scene is layered as `porn/{Performer}/{Scene Title}/{Scene Title}.{ext}`
  - Multi-part scenes of the same scene: `{Scene Title}.part.N.{ext}`; VR releases follow the same rules under the `porn_vr/` root
  - Miss (token unconfigured or scene not found on ThePornDB): local naming chain fallback `porn|porn_vr/{name}/{name}.{ext}`
- **Subtitles**:
  `<VideoBaseName>.<Language>.<ISO639-2>.<ext>`
  *(e.g., `黑客帝国 (1999).简体中文.chi.srt`, automatically co-located with the matching video)*

---

## API Reference

### 1. Create Plan: `POST /v1/plan`

Analyzes downloaded files and optional metadata to generate an organization plan.

#### Request Body
```json
{
  "dir": "The.Matrix.1999.2160p.UHD",
  "files": [
    "The.Matrix.1999.2160p.UHD.mkv",
    "The.Matrix.1999.chs.srt",
    "www.example.com.nfo"
  ],
  "metadata": {
    "title": "The Matrix",
    "year": 1999
  }
}
```

#### Response (`200 OK`)
```json
{
  "plan": [
    {
      "file": "The.Matrix.1999.2160p.UHD.mkv",
      "action": "move",
      "target": "movie/English/黑客帝国 (1999)/黑客帝国 (1999).mkv"
    },
    {
      "file": "The.Matrix.1999.chs.srt",
      "action": "move",
      "target": "movie/English/黑客帝国 (1999)/黑客帝国 (1999).简体中文.chi.srt"
    },
    {
      "file": "www.example.com.nfo",
      "action": "skip",
      "target": null
    }
  ],
  "error": null
}
```

---

### 2. Execute Plan: `POST /v1/execute`

Executes the actions in a previously approved plan. Moves files atomically and archives the source directory if all moves succeed.

#### Request Body
```json
{
  "dir": "The.Matrix.1999.2160p.UHD",
  "plan": [
    {
      "file": "The.Matrix.1999.2160p.UHD.mkv",
      "action": "move",
      "target": "movie/English/黑客帝国 (1999)/黑客帝国 (1999).mkv"
    },
    {
      "file": "The.Matrix.1999.chs.srt",
      "action": "move",
      "target": "movie/English/黑客帝国 (1999)/黑客帝国 (1999).简体中文.chi.srt"
    },
    {
      "file": "www.example.com.nfo",
      "action": "skip",
      "target": null
    }
  ]
}
```

#### Response
- **Success (`200 OK`)**:
  ```json
  {
    "failed_move": []
  }
  ```
  Upon success, `{DOWNLOAD_COMPLETED_DIR}/{dir}` is moved to `{DOWNLOAD_COMPLETED_DIR}/archive/{dir}`.
- **Partial or Complete Failure (`400 Bad Request`)**:
  Returns all failed moves with reasons without archiving the source folder.
  ```json
  {
    "failed_move": [
      {
        "file": "The.Matrix.1999.2160p.UHD.mkv",
        "action": "move",
        "target": "movie/English/黑客帝国 (1999)/黑客帝国 (1999).mkv",
        "reason": "file not found"
      }
    ]
  }
  ```

---

### 3. Replan: `POST /v1/replan`

Re-plans an existing (flawed) plan. It re-runs Stage 1 classification (rules + LLM, cheap) and Stage 2 metadata enrichment so a wrong category/domain is corrected with authoritative facts (TMDB/MetaTube, and the canonical JAV actress directory from the actress store), then finalizes through Stage 4 (companion subtitle pairing + physical security sanitization). The previous result is carried in a dedicated `previous_result` field and treated as suspect context — it is never merged into `metadata`.

- The upstream `organizer_category` hint is dropped: a replan exists because the previous classification was wrong, so the coarse hint must not be trusted again.
- Authoritative rules (a `dmm_id`, a standard bango filename) still apply.
- When no rule hits, the LLM re-classifies from the files and metadata, with the previous plan and the user hint attached as explicitly suspect context.
- Stage 2 then resolves authoritative facts, and re-planning routes to a domain prompt matching the **new** category with the user hint and the flawed previous plan. For JAV, the actress directory is a Go/ActorStore decision (`actor_dir` in the payload), not an LLM transliteration, so a replan lands in the same folder a normal plan would.
- `user_hint` is optional: a plain replan (no hint) still reclassifies and re-plans instead of reproducing the same result.

#### Request Body
```json
{
  "dir": "6b19f6ef7871e7773262bb9bbb7fa2315d049569",
  "files": ["NAAC-076.mp4"],
  "metadata": {
    "dmm_id": "n_1541naac076tk",
    "actors": ["YUUKA"],
    "title": "NAAC-076 【数量限定】Best naked/YUUKA チェキ付き"
  },
  "previous_result": {
    "plan": [
      { "file": "NAAC-076.mp4", "action": "move", "target": "porn/NAAC-076/NAAC-076.mp4" }
    ],
    "error": null
  },
  "user_hint": "this is a JAV, not porn"
}
```

#### Response (`200 OK`)
```json
{
  "plan": [
    { "file": "NAAC-076.mp4", "action": "move", "target": "jav/YUUKA/NAAC-076.mp4" }
  ],
  "error": null
}
```

---

### 4. Health Check: `GET /healthz`

- Response: `200 OK`, body: `ok`.

---

## Development Tasks (`Justfile`)

Organizer recipes live in the monorepo root `Justfile`:

```bash
just build-organizer    # Build the Go binary
just test-organizer     # Run unit and integration tests
just fmt-organizer      # Format code with goimports
just lint-organizer     # Run static analysis with golangci-lint
just test-e2e           # Run end-to-end (E2E) test suite
just run-organizer      # Run the service locally
just build-images       # Build the production Docker images (autoget + organizer)
```
