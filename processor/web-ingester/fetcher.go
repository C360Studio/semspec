package webingester

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/c360studio/semspec/source/weburl"
)

// FetchResult contains the result of fetching a web page.
type FetchResult struct {
	Body         []byte
	ContentType  string
	ETag         string
	LastModified time.Time
	StatusCode   int
}

// Fetcher fetches web content with security checks.
type Fetcher struct {
	client         *http.Client
	userAgent      string
	maxContentSize int64
}

// NewFetcher creates a new web fetcher.
func NewFetcher(timeout time.Duration, userAgent string, maxContentSize int64) *Fetcher {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Custom DialContext that validates resolved IPs to prevent DNS rebinding attacks
	safeDialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address: %w", err)
		}

		// Resolve DNS and validate IPs
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS lookup failed: %w", err)
		}

		for _, ipAddr := range ips {
			if weburl.IsPrivateIP(ipAddr.IP) {
				return nil, fmt.Errorf("connection to private IP %s is not allowed", ipAddr.IP)
			}
		}

		// Connect to the first valid IP
		for _, ipAddr := range ips {
			connAddr := net.JoinHostPort(ipAddr.IP.String(), port)
			conn, err := dialer.DialContext(ctx, network, connAddr)
			if err == nil {
				return conn, nil
			}
		}

		return nil, fmt.Errorf("failed to connect to any resolved IP")
	}

	transport := &http.Transport{
		DialContext:           safeDialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}

	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects (max 5)")
				}
				// Validate redirect target is not to private IP
				if err := weburl.ValidateURL(req.URL.String()); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				return nil
			},
		},
		userAgent:      userAgent,
		maxContentSize: maxContentSize,
	}
}

// Fetch retrieves content from the given URL.
func (f *Fetcher) Fetch(ctx context.Context, urlStr string) (*FetchResult, error) {
	return f.FetchWithETag(ctx, urlStr, "")
}

// FetchWithETag retrieves content with conditional fetch support.
// If etag is provided, returns 304 Not Modified if content hasn't changed.
func (f *Fetcher) FetchWithETag(ctx context.Context, urlStr string, etag string) (*FetchResult, error) {
	// Validate URL
	if err := weburl.ValidateURL(urlStr); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	result := &FetchResult{
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        resp.Header.Get("ETag"),
		StatusCode:  resp.StatusCode,
	}

	// Parse Last-Modified header
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			result.LastModified = t
		}
	}

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		return result, nil
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Read body with size limit
	limitReader := io.LimitReader(resp.Body, f.maxContentSize+1)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if int64(len(body)) > f.maxContentSize {
		return nil, fmt.Errorf("content too large (exceeds %d bytes)", f.maxContentSize)
	}

	result.Body = body
	return result, nil
}
