// Package webingester provides a NATS consumer component for ingesting web pages
// into the knowledge graph for context assembly.
//
// # Overview
//
// The web-ingester fetches web pages, converts HTML to markdown, chunks the content
// for efficient retrieval, and publishes the resulting entities to the graph
// ingestion stream. It implements comprehensive security measures to prevent
// SSRF (Server-Side Request Forgery) attacks.
//
// # Architecture
//
// The package consists of several key components:
//
//   - Component: NATS consumer lifecycle management
//   - Fetcher: HTTP client with SSRF protection and ETag support
//   - Converter: HTML to markdown conversion with content extraction
//   - Handler: Orchestrates fetching, conversion, and entity creation
//
// # Security
//
// The fetcher implements multiple layers of SSRF protection:
//
//   - URL validation requiring HTTPS
//   - Private IP and localhost blocking
//   - DNS rebinding protection via custom dialer
//   - IPv6-mapped IPv4 address detection
//   - Redirect chain validation
//
// # Entity Model
//
// For each web source, the ingester creates:
//
//   - One parent entity with metadata (URL, title, ETag, timestamps)
//   - Multiple chunk entities for content retrieval
//
// All entities are published to the "graph.ingest.entity" subject.
//
// # Configuration
//
// Key configuration options:
//
//   - FetchTimeout: HTTP request timeout (default 30s)
//   - MaxContentSize: Maximum response body size (default 10MB)
//   - UserAgent: HTTP User-Agent header value
//   - ChunkConfig: Chunking parameters (target/max/min tokens)
//
// # Usage
//
// The component is registered via the factory and started by the semstreams
// component registry:
//
//	import webingester "github.com/c360studio/semspec/processor/web-ingester"
//
//	func main() {
//	    webingester.Register(registry)
//	    // Component started automatically when configured
//	}
//
// Messages are consumed from the configured stream/consumer and results
// published to the graph ingestion stream.
package webingester
