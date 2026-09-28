package guangyapan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	ProductionAPIURL       = "https://openapi.guangyapan.com"
	ProductionAccountURL   = "https://openapi-account.guangyapan.com"
	ProductionAuthorizeURL = "https://www.guangyapan.com/oauth/"
	TestAPIURL             = "https://openapi-test.guangyapan.com"
	TestAccountURL         = "https://openapi-account-test.guangyapan.com"
)

// Config is copied by NewClient. Configure HTTPClient before sharing it.
// Custom URLs are useful for the platform's test environment and local mocks.
type Config struct {
	ClientID     string
	AccessToken  string
	APIURL       string
	AccountURL   string
	AuthorizeURL string
	HTTPClient   *http.Client
	// MaxResponseBytes defaults to 16 MiB; oversized JSON responses are rejected.
	MaxResponseBytes int64
}

type Client struct {
	clientID         string
	apiURL           string
	accountURL       string
	authorizeURL     string
	http             *http.Client
	maxResponseBytes int64
	mu               sync.RWMutex
	accessToken      string
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, invalid("ClientID", "is required")
	}
	if cfg.APIURL == "" {
		cfg.APIURL = ProductionAPIURL
	}
	if cfg.AccountURL == "" {
		cfg.AccountURL = ProductionAccountURL
	}
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = ProductionAuthorizeURL
	}
	for _, endpoint := range []string{cfg.APIURL, cfg.AccountURL, cfg.AuthorizeURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, invalid("endpoint", "must be an absolute HTTP(S) URL without credentials, query or fragment")
		}
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == 1<<63-1 {
		return nil, invalid("MaxResponseBytes", "is out of range")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 16 << 20
	}
	h := http.Client{Timeout: 30 * time.Second}
	if cfg.HTTPClient != nil {
		h = *cfg.HTTPClient
	}
	// Prevent credentials from being forwarded to redirect destinations. Fixed
	// OpenAPI endpoints should not redirect; surface the status to the caller.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{clientID: cfg.ClientID, accessToken: cfg.AccessToken,
		apiURL: strings.TrimRight(cfg.APIURL, "/"), accountURL: strings.TrimRight(cfg.AccountURL, "/"),
		authorizeURL: cfg.AuthorizeURL, http: &h, maxResponseBytes: cfg.MaxResponseBytes}, nil
}

// SetAccessToken changes the token for subsequent business calls, concurrency-safely.
// Auth methods return tokens without silently replacing the client's token.
func (c *Client) SetAccessToken(token string) { c.mu.Lock(); c.accessToken = token; c.mu.Unlock() }

type traceKey struct{}

// WithTraceparent adds an optional caller-managed W3C traceparent header.
func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceparent)
}

func (c *Client) request(ctx context.Context, method, base, path string, query url.Values, body any, headers http.Header, business bool) ([]byte, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("guangyapan: encode request: %w", err)
		}
	}
	endpoint := base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("guangyapan: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-client-id", c.clientID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if trace, ok := ctx.Value(traceKey{}).(string); ok && trace != "" {
		req.Header.Set("traceparent", trace)
	}
	for k, vv := range headers {
		req.Header[k] = append([]string(nil), vv...)
	}
	if business {
		c.mu.RLock()
		token := c.accessToken
		c.mu.RUnlock()
		if token == "" {
			return nil, invalid("AccessToken", "is required for business calls")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("guangyapan: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("guangyapan: read response: %w", err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return nil, &ProtocolError{Reason: "response exceeds MaxResponseBytes"}
	}
	return data, nil
}

// Response retains the platform status, including upload completion code 156.
type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    T      `json:"data"`
}

func call[T any](ctx context.Context, c *Client, method, path string, query url.Values, body any, completedOK bool, noData bool) (*Response[T], error) {
	data, err := c.request(ctx, method, c.apiURL, path, query, body, nil, true)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Code == nil {
		return nil, &ProtocolError{Reason: "expected JSON business envelope with code"}
	}
	r := &Response[T]{Code: *envelope.Code, Message: envelope.Message}
	if r.Code != CodeSuccess && !(completedOK && r.Code == CodeUploadCompleted) {
		return r, &APIError{Code: r.Code, Message: r.Message}
	}
	if len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		if noData {
			return r, nil
		}
		return r, &ProtocolError{Reason: "missing business data"}
	}
	if err := json.Unmarshal(envelope.Data, &r.Data); err != nil {
		return r, &ProtocolError{Reason: "business data has unexpected shape"}
	}
	return r, nil
}
