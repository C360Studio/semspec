package httptool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	xhttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/source/weburl"
)

const (
	// maxResponseSize caps the raw HTTP response read to prevent runaway allocations.
	maxResponseSize = 100 * 1024 // 100 KB

	// maxTextSize caps the HTML-to-text output presented to the agent.
	maxTextSize = 20000 // chars

	// minPersistLength skips graph persistence for responses that are too short
	// to be useful (error pages, redirects, etc.).
	minPersistLength = 500

	// requestTimeout is the HTTP client deadline for each fetch.
	requestTimeout = 30 * time.Second

	// ingestPublishTimeout is the deadline for the async ingestion-request
	// publish to web-ingester. Short by design — failure to enqueue should
	// not delay the agent, and a dropped enqueue is recoverable on the
	// next fetch of the same URL.
	ingestPublishTimeout = 5 * time.Second
)

// NATSClient is the subset of natsclient.Client that Executor needs.
// Depending on this interface keeps the executor testable without a live NATS
// connection — the same pattern used by spawn.NATSClient.
type NATSClient interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// Executor handles http_request tool calls.
type Executor struct {
	natsClient NATSClient // nil means graph persistence is disabled.
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
// natsClient is optional — if nil, graph persistence is disabled and the tool
// still fetches and converts HTML.
func NewExecutor(nc NATSClient, opts ...Option) *Executor {
	e := &Executor{
		natsClient: nc,
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
			Name:        "http_request",
			Description: "Fetch a URL. HTML is converted to clean readable text. Results are saved to the knowledge graph so future agents can find this content without re-fetching.",
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
			Error:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 500)),
		}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(contentType, "text/html")

	var content, title string
	if isHTML {
		content, _ = htmlToText(bytes.NewReader(body), maxTextSize)
		title = extractTitle(bytes.NewReader(body))
	} else {
		content = string(body)
		if len(content) > maxTextSize {
			content = content[:maxTextSize]
		}
	}

	// Enqueue the URL for proper ingestion via web-ingester (SSRF-checked
	// fetch, HTML→markdown conversion, chunking, optional classification).
	// Fire-and-forget — the agent has already received its response. Web-
	// ingester refetches; the small duplicate fetch cost is the price of
	// keeping httptool's tool-call shape synchronous while still feeding the
	// graph through one canonical pipeline.
	if isHTML && len(content) >= minPersistLength && e.natsClient != nil {
		go e.requestIngestion(rawURL)
	}

	if title != "" {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("# %s\n\n%s", title, content),
		}, nil
	}
	return agentic.ToolResult{CallID: call.ID, Content: content}, nil
}

// requestIngestion enqueues an AddWebSourceRequest on web-ingester's input
// subject so the URL gets the full ingestion pipeline (SSRF check, ETag,
// chunked HTML→markdown, optional classification). Mirrors the publish
// shape used by source/http.go's POST /api/sources/web handler.
//
// Fire-and-forget: the agent already has its response. A failed enqueue
// is logged but does not surface to the caller — the next fetch of the
// same URL will re-enqueue.
func (e *Executor) requestIngestion(rawURL string) {
	req := source.AddWebSourceRequest{URL: rawURL}
	data, err := json.Marshal(req)
	if err != nil {
		e.logger.Debug("Failed to marshal web ingestion request", "url", rawURL, "error", err)
		return
	}

	subject := fmt.Sprintf("source.web.ingest.%s", weburl.GenerateEntityID(rawURL))

	ctx, cancel := context.WithTimeout(context.Background(), ingestPublishTimeout)
	defer cancel()

	if err := e.natsClient.PublishToStream(ctx, subject, data); err != nil {
		e.logger.Debug("Failed to enqueue web ingestion request", "url", rawURL, "subject", subject, "error", err)
	}
}

// buildPinnedClient constructs an HTTP client that pins the resolved IP address
// for the given URL, preventing DNS rebinding attacks (TOCTOU between checkSSRF
// and the actual dial). If IP resolution fails the standard client is returned —
// checkSSRF has already validated the host at call time.
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
	// Normalize to IPv4 if possible so port joining works correctly.
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
		// Normalize IPv6-mapped IPv4 addresses (e.g. ::ffff:192.168.1.1) so
		// that the private/loopback checks apply correctly.
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

// truncate returns at most maxLen bytes of s, appending "..." if trimmed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
