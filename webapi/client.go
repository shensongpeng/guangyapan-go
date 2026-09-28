// Package webapi implements Guangyapan's consumer WebAPI independently of
// the client in the openapi subpackage. Credentials are not interchangeable.
package webapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	APIURL     = "https://api.guangyapan.com"
	AccountURL = "https://account.guangyapan.com"
	// DefaultClientID is the public Web client identifier used by AList, not an
	// OpenAPI application ID or secret.
	DefaultClientID = "aMe-8VSlkrbQXpUR"
)

type Config struct {
	ClientID    string
	AccessToken string
	// DeviceID should be persisted and reused with the account's tokens. If empty,
	// a random ID is generated; retrieve it with Client.DeviceID.
	DeviceID         string
	APIURL           string
	AccountURL       string
	HTTPClient       *http.Client
	MaxResponseBytes int64
	// DisableRateLimit disables the default per-endpoint 500ms request spacing.
	DisableRateLimit bool
}

type Client struct {
	clientID, deviceID, apiURL, accountURL string
	http                                   *http.Client
	maxBytes                               int64
	mu                                     sync.RWMutex
	accessToken                            string
	rateMu                                 sync.Mutex
	next                                   map[string]time.Time
	disableRateLimit                       bool
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.APIURL == "" {
		cfg.APIURL = APIURL
	}
	if cfg.AccountURL == "" {
		cfg.AccountURL = AccountURL
	}
	for _, endpoint := range []string{cfg.APIURL, cfg.AccountURL} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("webapi: invalid endpoint")
		}
	}
	cfg.DeviceID = strings.ReplaceAll(strings.TrimSpace(cfg.DeviceID), "-", "")
	if cfg.DeviceID == "" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			return nil, e
		}
		cfg.DeviceID = hex.EncodeToString(b)
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == 1<<63-1 {
		return nil, errors.New("webapi: invalid MaxResponseBytes")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 16 << 20
	}
	h := http.Client{Timeout: 30 * time.Second}
	if cfg.HTTPClient != nil {
		h = *cfg.HTTPClient
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{clientID: cfg.ClientID, deviceID: cfg.DeviceID, apiURL: strings.TrimRight(cfg.APIURL, "/"), accountURL: strings.TrimRight(cfg.AccountURL, "/"), http: &h, maxBytes: cfg.MaxResponseBytes, accessToken: strings.TrimSpace(cfg.AccessToken), next: make(map[string]time.Time), disableRateLimit: cfg.DisableRateLimit}, nil
}
func (c *Client) DeviceID() string { return c.deviceID }
func (c *Client) SetAccessToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = strings.TrimSpace(token)
}

// APIError and AccountError retain machine-readable status. Error strings omit
// server descriptions because they may echo user identifiers or credentials.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string     { return fmt.Sprintf("webapi: business code %d", e.Code) }
func IsCode(err error, code int) bool { var e *APIError; return errors.As(err, &e) && e.Code == code }

type AccountError struct {
	StatusCode             int
	ErrorCode              int
	ErrorName, Description string
}

func (e *AccountError) Error() string {
	return fmt.Sprintf("webapi: account HTTP %d, code %d", e.StatusCode, e.ErrorCode)
}

type HTTPError struct {
	StatusCode int
	RetryAfter string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("webapi: HTTP %d", e.StatusCode) }

var ErrTaskFailed = errors.New("webapi: task failed")
var ErrInvalidResponse = errors.New("webapi: unexpected response structure")
var ErrCaptchaRequired = errors.New("webapi: interactive captcha required")

func required(values ...string) error {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return errors.New("webapi: required value is empty")
		}
	}
	return nil
}
func wait(ctx context.Context, d time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *Client) throttle(ctx context.Context, path string) error {
	if c.disableRateLimit {
		return ctx.Err()
	}
	c.rateMu.Lock()
	now := time.Now()
	when := c.next[path]
	if when.Before(now) {
		when = now
	}
	c.next[path] = when.Add(500 * time.Millisecond)
	c.rateMu.Unlock()
	return wait(ctx, time.Until(when))
}

func (c *Client) request(ctx context.Context, account, authorized bool, method, path string, body any, captcha string) ([]byte, int, http.Header, error) {
	var b []byte
	var err error
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	base := c.apiURL
	if account {
		base = c.accountURL
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(b))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	if account {
		for k, v := range map[string]string{"X-Client-Id": c.clientID, "X-Client-Version": "0.0.1", "X-Device-Id": c.deviceID, "X-Device-Model": "chrome%2F147.0.0.0", "X-Device-Name": "PC-Chrome", "X-Device-Sign": "wdi10." + c.deviceID + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "X-Net-Work-Type": "NONE", "X-OS-Version": "MacIntel", "X-Platform-Version": "1", "X-Protocol-Version": "301", "X-Provider-Name": "NONE", "X-SDK-Version": "9.0.2"} {
			req.Header.Set(k, v)
		}
		if captcha != "" {
			req.Header.Set("X-Captcha-Token", captcha)
		}
	} else {
		req.Header.Set("Did", c.deviceID)
		req.Header.Set("Dt", "4")
	}
	if authorized {
		c.mu.RLock()
		token := c.accessToken
		c.mu.RUnlock()
		if token == "" {
			return nil, 0, nil, errors.New("webapi: access token required")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if e := c.throttle(ctx, base+path); e != nil {
		return nil, 0, nil, e
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	b, err = io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	if int64(len(b)) > c.maxBytes {
		return nil, resp.StatusCode, resp.Header, ErrInvalidResponse
	}
	return b, resp.StatusCode, resp.Header, nil
}

type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    T      `json:"data"`
}

func post[T any](ctx context.Context, c *Client, path string, body any, completeOK, noData bool) (*Response[T], error) {
	b, status, h, e := c.request(ctx, false, true, http.MethodPost, path, body, "")
	if e != nil {
		return nil, e
	}
	if status < 200 || status >= 300 {
		return nil, &HTTPError{status, h.Get("Retry-After")}
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &envelope) != nil || envelope.Code == nil {
		return nil, ErrInvalidResponse
	}
	r := &Response[T]{Code: *envelope.Code, Message: envelope.Message}
	if r.Code != 0 && !(completeOK && r.Code == 156) {
		return r, &APIError{r.Code, r.Message}
	}
	if len(envelope.Data) == 0 || string(bytes.TrimSpace(envelope.Data)) == "null" {
		if noData {
			return r, nil
		}
		return r, ErrInvalidResponse
	}
	if json.Unmarshal(envelope.Data, &r.Data) != nil {
		return r, ErrInvalidResponse
	}
	return r, nil
}
func account[T any](ctx context.Context, c *Client, method, path string, body any, authorized bool, captcha string) (*T, error) {
	b, status, h, e := c.request(ctx, true, authorized, method, path, body, captcha)
	if e != nil {
		return nil, e
	}
	var er struct {
		Error       string `json:"error"`
		Code        int    `json:"error_code"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(b, &er) == nil && (er.Error != "" || er.Code != 0) {
		return nil, &AccountError{status, er.Code, er.Error, er.Description}
	}
	if status < 200 || status >= 300 {
		return nil, &HTTPError{status, h.Get("Retry-After")}
	}
	var result *T
	if json.Unmarshal(b, &result) != nil || result == nil {
		return nil, ErrInvalidResponse
	}
	return result, nil
}
