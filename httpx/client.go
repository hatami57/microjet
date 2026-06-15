package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/httpx/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies the client instrumentation in exported spans.
const tracerName = "github.com/hatami57/microjet/httpx"

// DefaultClientTimeout bounds every request made by a Client that was not given
// its own *http.Client, so a hung upstream cannot block a caller forever.
const DefaultClientTimeout = 30 * time.Second

// DefaultMaxBackoff caps the per-attempt retry wait.
const DefaultMaxBackoff = 30 * time.Second

// Client is a small JSON-over-HTTP client for calling external services
// (payment gateways, partner APIs, internal microservices). It marshals request
// bodies to JSON, decodes JSON responses into a caller-supplied value, and turns
// non-2xx responses into structured *errorx.Error values. Construct one per
// upstream with NewClient and reuse it; it is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	headers map[string]string
	retry   retryPolicy
	breaker *circuitBreaker
}

// retryPolicy controls automatic retries. With max == 0 (the default) a request
// is attempted exactly once, preserving simple call semantics.
type retryPolicy struct {
	max        int
	base       time.Duration
	maxBackoff time.Duration
	methods    map[string]bool
	statuses   map[int]bool
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

// WithRetry enables automatic retries: up to maxRetries extra attempts on
// transport errors and retryable status codes, using exponential backoff with
// jitter starting at baseBackoff. Only idempotent methods are retried by default
// (GET, HEAD, PUT, DELETE, OPTIONS); customize with WithRetryableMethods and
// WithRetryableStatuses. Retries always honor the request context.
func WithRetry(maxRetries int, baseBackoff time.Duration) ClientOption {
	return func(c *Client) {
		c.retry.max = maxRetries
		if baseBackoff > 0 {
			c.retry.base = baseBackoff
		}
	}
}

// WithCircuitBreaker enables a per-client circuit breaker: after threshold
// consecutive server-side failures (transport errors and 5xx; a 4xx does not
// count) requests fail fast with a "circuit breaker open" error for cooldown,
// after which one trial request probes recovery. Pass 0 for either argument to
// use the defaults (DefaultBreakerThreshold, DefaultBreakerCooldown). It pairs
// well with WithRetry: retries smooth over blips, the breaker sheds load when an
// upstream is genuinely down.
func WithCircuitBreaker(threshold int, cooldown time.Duration) ClientOption {
	return func(c *Client) { c.breaker = newCircuitBreaker(threshold, cooldown) }
}

// WithRetryableMethods overrides which HTTP methods are eligible for retry.
func WithRetryableMethods(methods ...string) ClientOption {
	return func(c *Client) {
		c.retry.methods = map[string]bool{}
		for _, m := range methods {
			c.retry.methods[strings.ToUpper(m)] = true
		}
	}
}

// WithRetryableStatuses overrides which response status codes trigger a retry.
func WithRetryableStatuses(statuses ...int) ClientOption {
	return func(c *Client) {
		c.retry.statuses = map[int]bool{}
		for _, s := range statuses {
			c.retry.statuses[s] = true
		}
	}
}

// NewClient creates a Client. baseURL may be empty, in which case request paths
// must be absolute URLs.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: DefaultClientTimeout},
		headers: map[string]string{},
		retry: retryPolicy{
			max:        0,
			base:       100 * time.Millisecond,
			maxBackoff: DefaultMaxBackoff,
			methods:    map[string]bool{"GET": true, "HEAD": true, "PUT": true, "DELETE": true, "OPTIONS": true},
			statuses:   map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true},
		},
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
// It returns a *errorx.Error (Internal) for transport failures and non-2xx
// responses; the response body is attached to the error's Params for diagnosis.
// When retries are enabled (WithRetry) it transparently re-attempts retryable
// failures, returning the last error if all attempts fail.
func (c *Client) Do(ctx context.Context, method, path string, body, out any, opts ...RequestOption) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return errorx.NewInternalError("http", "encoding request body failed").WithInner(err)
		}
		bodyBytes = b
	}
	url := c.url(path)

	// Fail fast when the breaker is open, before touching the network.
	if c.breaker != nil && !c.breaker.allow() {
		return errorx.NewInternalError("http", "upstream circuit breaker is open").WithParams("url", url)
	}

	status, err := c.attempt(ctx, method, url, bodyBytes, body != nil, out, opts...)
	if c.breaker != nil {
		c.breaker.record(!serverFailed(status, err))
	}
	return err
}

// attempt runs the request with the configured retry policy and returns the
// final attempt's status (0 on a transport failure) and error.
func (c *Client) attempt(ctx context.Context, method, url string, bodyBytes []byte, hasBody bool, out any, opts ...RequestOption) (int, error) {
	var (
		lastErr    error
		lastStatus int
	)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, c.backoff(attempt)); err != nil {
				return lastStatus, lastErr // ctx cancelled while waiting; surface the failure that triggered the retry
			}
		}
		status, err := c.doOnce(ctx, method, url, bodyBytes, hasBody, out, opts...)
		if err == nil {
			return status, nil
		}
		lastErr, lastStatus = err, status
		if attempt >= c.retry.max || !c.shouldRetry(method, status) {
			return status, err
		}
	}
}

// doOnce performs a single attempt and returns the response status code (0 for a
// transport-level failure) together with any error.
func (c *Client) doOnce(ctx context.Context, method, url string, bodyBytes []byte, hasBody bool, out any, opts ...RequestOption) (status int, err error) {
	var reader io.Reader
	if hasBody {
		reader = bytes.NewReader(bodyBytes)
	}

	// One client span per attempt, so retries are visible as separate spans. A
	// no-op without a global tracer provider (see the otelx module).
	ctx, span := otel.Tracer(tracerName).Start(ctx, method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(method),
			semconv.URLFull(url),
		),
	)
	defer func() {
		if status != 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, errorx.NewInternalError("http", "building request failed").WithInner(err)
	}
	// Carry the trace across the wire (W3C traceparent; a no-op until otelx
	// installs the global propagator).
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	// Propagate the inbound correlation id so the upstream can join the trace.
	if id := middleware.RequestIDFromContext(ctx); id != "" {
		req.Header.Set(middleware.DefaultRequestIDHeader, id)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for _, o := range opts {
		o(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, errorx.NewInternalError("http", "request failed").
			WithParams("url", req.URL.String()).WithInner(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, errorx.NewInternalError("http", fmt.Sprintf("upstream returned %d", resp.StatusCode)).
			WithParams("status", resp.StatusCode, "body", string(data), "url", req.URL.String())
	}
	if out == nil || len(data) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, errorx.NewInternalError("http", "decoding response body failed").
			WithParams("body", string(data)).WithInner(err)
	}
	return resp.StatusCode, nil
}

// shouldRetry reports whether a failed attempt with the given status (0 == a
// transport error) is eligible for another try.
func (c *Client) shouldRetry(method string, status int) bool {
	if c.retry.max == 0 || !c.retry.methods[strings.ToUpper(method)] {
		return false
	}
	if status == 0 {
		return true // transport/network error
	}
	return c.retry.statuses[status]
}

// backoff returns the wait before retry attempt n (n >= 1): exponential growth
// from the base delay, capped at maxBackoff, with equal jitter to avoid
// thundering-herd retries.
func (c *Client) backoff(attempt int) time.Duration {
	d := c.retry.maxBackoff
	if attempt-1 < 31 {
		if grown := c.retry.base << (attempt - 1); grown > 0 && grown < c.retry.maxBackoff {
			d = grown
		}
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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
