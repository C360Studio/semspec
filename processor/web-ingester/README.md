# Web Ingester

NATS consumer component for ingesting web pages into the knowledge graph.

## Overview

The web-ingester fetches web pages, converts HTML to markdown, chunks the content for efficient retrieval, and publishes entities to the graph ingestion stream.

## Features

- **SSRF Protection**: Multiple layers of security including URL validation, private IP blocking, and DNS rebinding protection
- **ETag Support**: Conditional fetching for efficient content refresh
- **Smart Extraction**: Documentation-focused HTML extraction prioritizing `<main>`, `<article>`, and content areas
- **Chunking**: Intelligent markdown chunking respecting section boundaries
- **Auto-refresh**: Optional periodic content updates with configurable intervals

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Web Ingester                              │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐          │
│  │   Fetcher   │───▶│  Converter  │───▶│   Chunker   │          │
│  │             │    │             │    │             │          │
│  │ HTTPS only  │    │ HTML → MD   │    │ Section-    │          │
│  │ SSRF checks │    │ Content     │    │ aware       │          │
│  │ ETag        │    │ extraction  │    │ splitting   │          │
│  └─────────────┘    └─────────────┘    └─────────────┘          │
│         │                                      │                 │
│         ▼                                      ▼                 │
│  ┌─────────────┐                      ┌─────────────────────┐   │
│  │   Handler   │─────────────────────▶│  Entity Publisher   │   │
│  │             │                      │                     │   │
│  │ Orchestrate │                      │ graph.ingest.entity │   │
│  │ Build       │                      │                     │   │
│  │ entities    │                      └─────────────────────┘   │
│  └─────────────┘                                                │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

## Configuration

```json
{
  "type": "web-ingester",
  "config": {
    "stream_name": "AGENT",
    "consumer_name": "web-ingester",
    "fetch_timeout": "30s",
    "max_content_size": 10485760,
    "user_agent": "SemSpec/1.0 (Web Ingester)",
    "chunk_config": {
      "target_tokens": 512,
      "max_tokens": 1024,
      "min_tokens": 128
    }
  }
}
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `stream_name` | string | `AGENT` | JetStream stream name |
| `consumer_name` | string | `web-ingester` | Consumer name |
| `fetch_timeout` | duration | `30s` | HTTP request timeout |
| `max_content_size` | int | `10485760` | Max response size (10MB) |
| `user_agent` | string | `SemSpec/1.0` | HTTP User-Agent |
| `chunk_config.target_tokens` | int | `512` | Target chunk size |
| `chunk_config.max_tokens` | int | `1024` | Maximum chunk size |
| `chunk_config.min_tokens` | int | `128` | Minimum chunk size |

## Security

### SSRF Protection

The fetcher implements comprehensive SSRF protection:

1. **URL Validation**: Only HTTPS URLs allowed
2. **Localhost Blocking**: Rejects `localhost`, `127.0.0.1`, `::1`
3. **Private IP Blocking**: Blocks RFC 1918 ranges, CGNAT, link-local
4. **Local Domain Blocking**: Rejects `.local` and `.internal` domains
5. **DNS Rebinding Protection**: Validates resolved IPs before connection
6. **IPv6 Handling**: Detects IPv6-mapped IPv4 addresses
7. **Redirect Validation**: Validates each redirect target

### Entity ID Validation

Entity IDs are validated to prevent NATS subject injection attacks.

## Entity Model

### Parent Entity

```
source.web.example-com-docs-guide
├── source.type = "web"
├── source.web.url = "https://example.com/docs/guide"
├── source.web.title = "Documentation Guide"
├── source.web.content_type = "text/html"
├── source.web.etag = "abc123"
├── source.web.last_fetched = "2024-01-15T10:30:00Z"
├── source.web.chunk_count = 5
└── source.status = "ready"
```

### Chunk Entities

```
source.web.example-com-docs-guide.chunk.1
├── code.belongs = "source.web.example-com-docs-guide"
├── source.web.type = "chunk"
├── source.web.content = "# Introduction\n\n..."
├── source.web.chunk_index = 1
└── source.web.section = "Introduction"
```

## NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `source.web.ingest.*` | Input | Ingestion requests |
| `source.web.refresh.*` | Input | Refresh requests |
| `graph.ingest.entity` | Output | Published entities |

## Dependencies

- `source/weburl`: Shared URL validation and ID generation
- `source/chunker`: Content chunking
- `github.com/JohannesKaufmann/html-to-markdown`: HTML conversion

## Testing

```bash
go test ./processor/web-ingester/... -v
```

## Related Packages

- [`source/weburl`](../../source/weburl/): URL validation and entity ID generation
- [`source/chunker`](../../source/chunker/): Markdown content chunking
- [`vocabulary/source`](../../vocabulary/source/): Source vocabulary predicates
