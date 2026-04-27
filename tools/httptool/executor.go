// Package httptool implements the http_request agent tool. It fetches URLs
// (SSRF-checked, DNS-pinned), runs Readability on HTML responses, renders
// the result via a caller-selected format (summary, markdown, links,
// headings, raw), and publishes a chunked entity graph to
// graph.ingest.entity for future graph_search lookups.
package httptool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	xhttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/source/chunker"
	"github.com/c360studio/semspec/source/webingest"
)

const (
	// maxResponseSize caps the raw HTTP response read to prevent runaway
	// allocations.
	maxResponseSize = 1 * 1024 * 1024 // 1 MB

	// defaultMaxChars caps the agent-facing output for any format that
	// honours max_chars. Agents can override per call.
	defaultMaxChars = 20000

	// minPersistLength skips graph persistence for responses that are too
	// short to be useful (error pages, redirects, empty stubs).
	minPersistLength = 500

	// requestTimeout is the HTTP client deadline for each fetch.
	requestTimeout = 30 * time.Second
)

// NATSClient is the subset of natsclient.Client that Executor needs. The
// interface keeps the executor testable without a live NATS connection.
// Compatible with webingest.NATSClient (same single method).
type NATSClient interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// Executor handles http_request tool calls.
type Executor struct {
	natsClient NATSClient // nil disables graph persistence; tool still fetches.
	converter  *webingest.Converter
	chunker    *chunker.Chunker
	logger     *slog.Logger
	timeout    time.Duration // 0 means use requestTimeout const
}

// Option configures an http_request Executor.
type Option func(*Executor)

// WithRequestTimeout overrides the default HTTP request timeout (30s).
// 0 means use the builtin default.
func WithRequestTimeout(d time.Duration) Option {
	return func(e *Executor) { e.timeout = d }
}

// NewExecutor creates an HTTP request executor.
// natsClient is optional — if nil, graph persistence is disabled and the
// tool still fetches and converts HTML.
func NewExecutor(nc NATSClient, opts ...Option) *Executor {
	chk, err := chunker.New(chunker.DefaultConfig())
	if err != nil {
		// chunker.DefaultConfig() is always valid; the error is unreachable.
		panic(fmt.Sprintf("httptool: default chunker config invalid: %v", err))
	}
	e := &Executor{
		natsClient: nc,
		converter:  webingest.NewConverter(),
		chunker:    chk,
		logger:     slog.Default().With("component", "http-request"),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// effectiveTimeout returns the configured timeout or the default.
func (e *Executor) effectiveTimeout() time.Duration {
	if e.timeout > 0 {
		return e.timeout
	}
	return requestTimeout
}

// ListTools returns the http_request tool definition.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name: "http_request",
			Description: "Fetch a URL and return a curated view of its content. " +
				"HTML pages run through Readability so the response is the main " +
				"article, not boilerplate. Every fetch is also chunked and stored " +
				"in the knowledge graph for future agents to find via graph_search " +
				"without re-fetching. " +
				"Default format is `summary` — title, outline, top links, and a " +
				"short content excerpt — designed to answer 'is this page worth " +
				"reading more of?' in under 2K chars. " +
				"Use format=markdown for the full cleaned article (capped at " +
				"max_chars), format=links to get just the URLs, format=headings " +
				"for an outline, or format=raw for the original body.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Full URL including scheme, e.g. https://pkg.go.dev/net/http",
					},
					"method": map[string]any{
						"type":        "string",
						"description": "HTTP method: GET or POST (default: GET)",
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Response shape: summary (default), markdown, links, headings, or raw",
						"enum":        []string{"summary", "markdown", "links", "headings", "raw"},
					},
					"max_chars": map[string]any{
						"type":        "integer",
						"description": "Override the per-format character cap (default 20000)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// Execute handles an http_request tool call.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	rawURL, ok := call.Arguments["url"].(string)
	if !ok || rawURL == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "url is required"}, nil
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "url must start with http:// or https://",
		}, nil
	}
	if err := checkSSRF(rawURL); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}

	method := "GET"
	if m, ok := call.Arguments["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}
	if method != "GET" && method != "POST" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "method must be GET or POST",
		}, nil
	}

	format := FormatSummary
	if rawFmt, ok := call.Arguments["format"].(string); ok {
		format = parseFormat(rawFmt)
	}

	maxChars := 0
	if v, ok := call.Arguments["max_chars"].(float64); ok && v > 0 {
		maxChars = int(v)
	} else if v, ok := call.Arguments["max_chars"].(int); ok && v > 0 {
		maxChars = v
	}

	reqCtx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()

	req, err := xhttp.NewRequestWithContext(reqCtx, method, rawURL, nil)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("create request: %v", err),
		}, nil
	}
	req.Header.Set("User-Agent", "semspec-agent/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	client := e.buildPinnedClient(rawURL)
	resp, err := client.Do(req)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("read response: %v", err),
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateChars(string(body), 500)),
		}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "application/xhtml")

	// Non-HTML responses bypass conversion: agents asking for json or text
	// want the body verbatim. raw format also skips the converter.
	if !isHTML || format == FormatRaw {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: truncateChars(string(body), pickMaxChars(maxChars)),
		}, nil
	}

	convResult, err := e.converter.Convert(body, rawURL)
	if err != nil {
		// Conversion failure shouldn't kill the call — return the raw body
		// so the agent can salvage something.
		e.logger.Debug("HTML conversion failed, falling back to raw", "url", rawURL, "error", err)
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: truncateChars(string(body), pickMaxChars(maxChars)),
		}, nil
	}

	rendered := formatResponse(format, convResult, body, rawURL, maxChars)

	// Persist to graph for any format that received curated content.
	// links/headings views ran on the raw body too, but persisting them
	// would write the same parent + chunks repeatedly with no new value.
	// raw was already returned above. summary and markdown both share the
	// converted markdown — same chunks, persist once.
	if (format == FormatSummary || format == FormatMarkdown) &&
		len(convResult.Markdown) >= minPersistLength &&
		e.natsClient != nil {
		go e.persistAsync(rawURL, contentType, resp.Header.Get("ETag"), body)
	}

	return agentic.ToolResult{CallID: call.ID, Content: rendered}, nil
}

// persistAsync ingests + publishes graph entities in the background so the
// agent's response isn't blocked on chunk publish latency.
//
// Failures are logged at debug level — graph persistence is best-effort,
// the agent already received its answer. context.Background is intentional:
// the tool-call context is cancelled by the time we run.
func (e *Executor) persistAsync(rawURL, contentType, etag string, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := webingest.Ingest(webingest.IngestRequest{
		URL:         rawURL,
		ContentType: contentType,
		ETag:        etag,
	}, body, e.converter, e.chunker, time.Now())
	if err != nil {
		e.logger.Debug("ingest failed", "url", rawURL, "error", err)
		return
	}
	if err := webingest.PublishGraphEntities(ctx, e.natsClient, result); err != nil {
		e.logger.Debug("publish graph entities failed",
			"url", rawURL, "entity_id", result.EntityID, "chunks", result.ChunkCount, "error", err)
		return
	}
	e.logger.Debug("ingested",
		"url", rawURL, "entity_id", result.EntityID, "chunks", result.ChunkCount)
}

// pickMaxChars resolves an explicit max_chars or returns the default.
func pickMaxChars(explicit int) int {
	if explicit > 0 {
		return explicit
	}
	return defaultMaxChars
}

// buildPinnedClient constructs an HTTP client that pins the resolved IP
// address for the given URL, preventing DNS rebinding attacks (TOCTOU
// between checkSSRF and the actual dial). If IP resolution fails the
// standard client is returned — checkSSRF has already validated the host.
func (e *Executor) buildPinnedClient(rawURL string) *xhttp.Client {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return &xhttp.Client{Timeout: e.effectiveTimeout()}
	}
	host := parsed.Hostname()

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		// Fallback — SSRF check already passed; let the HTTP stack handle it.
		return &xhttp.Client{Timeout: e.effectiveTimeout()}
	}

	pinnedIP := ips[0]
	if v4 := pinnedIP.To4(); v4 != nil {
		pinnedIP = v4
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &xhttp.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil || port == "" {
				port = "443"
				if strings.HasPrefix(rawURL, "http://") {
					port = "80"
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedIP.String(), port))
		},
	}

	return &xhttp.Client{
		Transport: transport,
		Timeout:   e.effectiveTimeout(),
		CheckRedirect: func(req *xhttp.Request, via []*xhttp.Request) error {
			if err := checkSSRF(req.URL.String()); err != nil {
				return err
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// checkSSRF blocks requests to private/loopback/link-local IP ranges.
// DNS is resolved before the request to prevent SSRF via hostname rebinding.
// DNS failure is treated as a block — an unresolvable host cannot be trusted.
func checkSSRF(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}

	host := parsed.Hostname()

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}

	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("blocked: %s resolves to private/reserved IP %s", host, ip)
		}
	}
	return nil
}
