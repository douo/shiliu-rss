<div align="center">

<img src="assets/icon.svg" alt="Shiliu RSS Logo" width="80" height="80" />

# 拾流 RSS · Shiliu

**A lightweight, self-hosted personal RSS reader with intelligent morning briefings and a conversational AI assistant.**

Built as an efficient single-process application with Go and SQLite, paired with a modern Svelte 5 frontend. Designed to run smoothly on homeservers and low-power NAS devices with a minimal resource footprint.

<p align="center">
  <strong>English</strong> •
  <a href="README_zh.md">简体中文</a>
</p>

<p align="center">
  <img src="assets/screenshots/morning-brief.png" alt="Shiliu RSS Morning Briefing and Conversational AI Assistant" width="100%" />
</p>

</div>

---

## Highlights

- **⚡ Lightweight Single-Binary Architecture**: Pure Go backend embedding a pre-compiled Svelte 5 static single-page application (SPA). Zero external runtime dependencies—no Node.js or separate database servers needed in production.
- **☀️ Daily Morning Briefings**: Automated daily executive digests summarizing unread feeds by category, source, and importance. Inline citations allow seamless one-click jumps directly to source articles, with full reading position retention.
- **🤖 Built-in Conversational AI Agent**: Chat with your feeds and morning briefings using a bounded lexical RAG engine. Native compatibility with standard OpenAI API / Chat Completions protocols. Features strict token budgets, scheduled windows, and zero overhead when disabled.
- **📖 Focused Reading Experience**: Distraction-free typography with responsive dark/light theme matching, fluid navigation, and full keyboard shortcuts designed to avoid conflicts with tools like Vimium.
- **🔄 Reliable Feed Synchronization**: Full support for RSS 2.0, Atom, and JSON Feed. Implements HTTP conditional requests (`ETag`, `Last-Modified`), jittered intervals, exponential backoff, and persistent background job queues with crash recovery.
- **🧹 Granular Data Retention**: Customizable retention policies per subscription. Automatically clean up older article bodies (e.g., 90-day retention) while keeping metadata, notes, and starred entries intact indefinitely.
- **🔍 Full-Text Search & Extraction**: Integrated one-click readability parser and server-side HTML sanitization. Embedded SQLite FTS5 full-text search with CJK tokenization support.
- **🚚 Frictionless Migration**: Lossless import from Miniflux (feeds, categories, read states, and bookmarks) and standard OPML import/export.

---

## Interface Preview

<div align="center">

### Focused Article Reading View
<img src="assets/screenshots/reading-view.png" alt="Distraction-Free Article Reading View" width="100%" />

### Stream & Feed Management
<img src="assets/screenshots/article-list.png" alt="Organized Article Stream & Feed Management" width="100%" />

</div>

---

## Tech Stack

| Layer | Technologies | Purpose |
|---|---|---|
| **Backend & HTTP** | Go (standard library `net/http`) | Clean concurrency, low memory footprint, and straightforward cross-compilation |
| **Database** | SQLite WAL (`modernc.org/sqlite`, `goose`) | Pure Go driver without CGO dependencies; transactional schema migrations |
| **Search Engine** | SQLite FTS5 | In-database CJK tokenization and phrase searching; transactional consistency |
| **Frontend** | Svelte 5, SvelteKit, TypeScript | Reactive, component-driven UI compiled to static assets and embedded via `go:embed` |
| **Styling & UI** | Tailwind CSS 4, Lucide Icons | Responsive layout, modern aesthetic, and fluid reading interactions |
| **API Contract** | OpenAPI 3.1.0 (`oapi-codegen`, `openapi-fetch`) | Single source of truth generating both Go handlers and TypeScript API clients |
| **Parsing & Cleanup** | `gofeed`, `go-readability`, `bluemonday` | Safe feed parsing and strict server-side HTML sanitization |

---

## Quick Start (Local Development)

### Prerequisites

- **Go**: 1.27+
- **pnpm**: 11.0.9+ (Node.js runtime is managed locally by pnpm)

### Setup & Run

1. **Install dependencies and test browsers**:
   ```sh
   make setup
   ```

2. **Initialize local database & credentials** (first run only):
   ```sh
   make init
   ```
   > An initial administrative password will be generated into `.local/admin-password`. No username is required for login.

3. **Start local development servers**:
   ```sh
   make dev
   ```
   - Web UI: **http://127.0.0.1:41002** (Vite dev server with hot reload)
   - API Server: **http://127.0.0.1:41001**
   - Port configurations are stored in `config/dev.env`.

4. **Run the embedded single binary**:
   To test the production-style single binary locally:
   ```sh
   make run
   # Open http://127.0.0.1:41001
   ```

---

## Production Deployment

Shiliu RSS is optimized for deployment as a single container via **Docker Compose** behind a reverse proxy (such as Nginx, Caddy, or Traefik) providing HTTPS.

### 1. Docker Compose Example

```yaml
# deploy/compose.yaml
name: shiliu-rss

services:
  reader:
    image: ${SHILIU_IMAGE}
    restart: unless-stopped
    read_only: true
    user: "65532:65532"
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./data:/data
      - ./backup:/backup
    environment:
      SHILIU_ADDR: ":8080"
      SHILIU_DB: /data/reader.db
      SHILIU_ORIGIN: "https://reader.example.com"
      SHILIU_COOKIE_SECURE: "true"
      SHILIU_BACKUP_DIR: /backup
    tmpfs:
      - /tmp:size=16m,noexec,nosuid,nodev,mode=1777
    healthcheck:
      test: ["CMD", "/reader", "healthcheck"]
      interval: 60s
      timeout: 6s
      retries: 3
```

### 2. Initialization & Secrets

Initialize the admin account using a secure password file without exposing plaintext credentials in shell history:

```sh
docker compose run --rm --no-deps -T \
  -v /path/to/password-file:/run/secrets/password:ro \
  -e SHILIU_PASSWORD_FILE=/run/secrets/password reader init
```

For complete instructions on Nginx TLS configuration, automated database backups, systemd units, and zero-downtime updates, please refer to the [Production Deployment Guide](docs/production.md).

---

## Keyboard Shortcuts

Designed for high-efficiency reading, keyboard navigation avoids conflicts with common browser extensions such as Vimium:

| Key | Description |
|:---:|---|
| `↑` / `↓` | Navigate feed list / Previous & Next article in reader view |
| `c` | Toggle sidebar navigation |
| `a` | Toggle read / unread status |
| `s` | Star / bookmark article |
| `e` | Open original article in a new tab |
| `q` | Focus search bar |
| `Esc` | Close modal / return to feed list / dismiss overlays |

---

## AI Agent Integration

Configure your preferred LLM in **Settings → Agent Configuration**:

- **Protocols**: Supports OpenAI-compatible endpoints (Chat Completions / Responses).
- **Security**: API keys are write-only in the UI and never exposed back to the client. You can also specify keys via a protected file (`SHILIU_AGENT_API_KEY_FILE`).
- **Morning Briefing**: Defaults to a daily summary (e.g. 08:00 Asia/Shanghai) analyzing unread content across feeds.
- **Bounded Resources**: Strictly enforce daily token limits, execution time windows, and concurrency budgets. Unconfigured agents remain completely inert with zero background overhead.

---

## Verification & Testing

```sh
make check-generated  # Check OpenAPI schema drift for Go & TypeScript
make check            # Run go vet & Svelte / TypeScript type-checks
make test             # Run Go unit & integration tests with -race
make test-web         # Execute Playwright E2E browser test suite
make build-linux      # Cross-compile release binaries for Linux AMD64
```

---

## Documentation

Detailed technical and operational documentation is available in the [`docs/`](docs/) directory:

| Document | Focus Area |
|---|---|
| [Documentation Index](docs/README.md) | Map of all documentation resources and reading paths |
| [Product Requirements](docs/product.md) | Product scope, functional requirements, and acceptance criteria |
| [Architecture](docs/architecture.md) | Modular design, lifecycle rules, transactions, and storage |
| [Technology Decisions](docs/technology.md) | Tech stack evaluation, rationale, and engineering constraints |
| [Implementation Log](docs/implementation.md) | Validation status, test evidence, and engineering metrics |
| [Agent Design](docs/agent-design.md) | Morning briefs, RAG design, citations, and conversation flows |
| [Agent API](docs/agent-api.md) | Shared HTTP API contract for both web client and external agents |
| [Operations Guide](docs/operations.md) | Backup, restore, and Miniflux migration procedures |
| [Production Deployment](docs/production.md) | Container setup, TLS reverse proxy, and maintenance runbooks |
| [Security Policy](docs/security.md) | Secrets management, input sanitization, and data isolation |
