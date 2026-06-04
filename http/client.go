package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hatami57/microjet/core"
)

// DefaultClientTimeout bounds every request made by a Client that was not given
// its own *http.Client, so a hung upstream cannot block a caller forever.
const DefaultClientTimeout = 30 * time.Second

// Client is a small JSON-over-HTTP client for calling external services
// (payment gateways, partner APIs, internal microservices). It marshals request
// bodies to JSON, decodes JSON responses into a caller-supplied value, and turns
// non-2xx responses into structured *core.Error values. Construct one per
// upstream with NewClient and reuse it; it is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	headers map[string]string
}

// ClientOption configures a Client at construction time.
type ClientOption func(*Client)

// WithHTTPClient sets the underlying *http.Client (for custom timeouts, proxies,
// or transport-level instrumentation).
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.http = h }
}

// WithTimeout sets the request timeout on the default client. Ignored if
// WithHTTPClient supplied a client.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		if c.http != nil {
			c.http.Timeout = d
		}
	}
}

// WithDefaultHeader adds a header sent on every request (e.g. an API key).
func WithDefaultHeader(key, value string) ClientOption {
	return func(c *Client) { c.headers[key] = value }
}

// NewClient creates a Client. baseURL may be empty, in which case request paths
// must be absolute URLs.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: DefaultClientTimeout},
		headers: map[string]string{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// RequestOption customises a single request (e.g. a per-call bearer token).
type RequestOption func(*http.Request)

// WithHeader sets a header on one request, overriding any default of the same key.
func WithHeader(key, value string) RequestOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// GetJSON issues a GET and decodes the JSON response into out (which may be nil
// to discard the body).
func (c *Client) GetJSON(ctx context.Context, path string, out any, opts ...RequestOption) error {
	return c.Do(ctx, http.MethodGet, path, nil, out, opts...)
}

// PostJSON marshals body to JSON, issues a POST, and decodes the JSON response
// into out (either may be nil).
func (c *Client) PostJSON(ctx context.Context, path string, body, out any, opts ...RequestOption) error {
	return c.Do(ctx, http.MethodPost, path, body, out, opts...)
}

// Do performs a request with an optional JSON body and decodes a JSON response.
// It returns a *core.Error (Internal) for transport failures and non-2xx
// responses; the response body is attached to the error's Params for diagnosis.
func (c *Client) Do(ctx context.Context, method, path string, body, out any, opts ...RequestOption) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return core.NewInternalError("http", "encoding request body failed").WithInner(err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path), reader)
	if err != nil {
		return core.NewInternalError("http", "building request failed").WithInner(err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for _, o := range opts {
		o(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return core.NewInternalError("http", "request failed").
			WithParams("url", req.URL.String()).WithInner(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return core.NewInternalError("http", fmt.Sprintf("upstream returned %d", resp.StatusCode)).
			WithParams("status", resp.StatusCode, "body", string(data), "url", req.URL.String())
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return core.NewInternalError("http", "decoding response body failed").
			WithParams("body", string(data)).WithInner(err)
	}
	return nil
}

// url joins the client base URL with path. An absolute path (http:// or
// https://) is used verbatim, so a Client with no base URL still works.
func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if c.baseURL == "" {
		return path
	}
	return c.baseURL + "/" + strings.TrimLeft(path, "/")
}
