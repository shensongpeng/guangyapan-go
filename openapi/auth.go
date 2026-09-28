package openapi

import (
	"context"
	"crypto/md5" // Required by the platform's device-code signature protocol.
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DeviceContext is required only for device code creation and device token polling.
type DeviceContext struct{ DeviceID, ProjectID string }

func (d DeviceContext) headers() (http.Header, error) {
	if d.DeviceID == "" || d.ProjectID == "" {
		return nil, invalid("DeviceID and ProjectID", "are required")
	}
	h := make(http.Header)
	h.Set("x-device-id", d.DeviceID)
	h.Set("x-project-id", d.ProjectID)
	return h, nil
}

type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	VerificationURL         string `json:"verification_url"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresAt is set when RequestDeviceCode returns. Preserve it across handoffs.
	ExpiresAt time.Time `json:"-"`
}

type Token struct {
	TokenType    string    `json:"token_type"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	Subject      string    `json:"sub"`
	ExpiresAt    time.Time `json:"-"`
}

// DeviceSignature implements the exact unescaped MD5 signing string. Use only
// on a trusted application server; never embed signSecret in distributed apps.
func DeviceSignature(clientID, timestamp, signSecret string) string {
	sum := md5.Sum([]byte("client_id=" + clientID + "&timestamp=" + timestamp + "&secret=" + signSecret))
	return hex.EncodeToString(sum[:])
}

// RequestDeviceCode is the sole method requiring a signing secret. The server's
// outbound IP must be allowlisted by the platform. No secret is retained.
func (c *Client) RequestDeviceCode(ctx context.Context, device DeviceContext, signSecret, scope string) (*DeviceCode, error) {
	h, err := device.headers()
	if err != nil {
		return nil, err
	}
	if signSecret == "" {
		return nil, invalid("signSecret", "is required")
	}
	started := time.Now()
	ts := strconv.FormatInt(started.Unix(), 10)
	h.Set("timestamp", ts)
	h.Set("sign", DeviceSignature(c.clientID, ts, signSecret))
	data, err := c.request(ctx, http.MethodPost, c.apiURL, "/v1/auth/device/code", nil,
		map[string]string{"scope": scope, "client_id": c.clientID, "project_id": device.ProjectID}, h, false)
	if err != nil {
		return nil, err
	}
	var result DeviceCode
	if err := decodeAuth(data, &result); err != nil {
		return nil, err
	}
	if result.DeviceCode == "" || !validSeconds(result.ExpiresIn) || !validSeconds(result.Interval) {
		return nil, &ProtocolError{Reason: "device code, positive expires_in and interval are required"}
	}
	result.ExpiresAt = started.Add(time.Duration(result.ExpiresIn) * time.Second)
	return &result, nil
}

func validSeconds(n int64) bool { return n > 0 && n <= int64((1<<63-1)/time.Second) }

func decodeAuth(data []byte, target any) error {
	if strings.TrimSpace(string(data)) == "authorization_pending" {
		return ErrAuthorizationPending
	}
	var e struct {
		Code        *int   `json:"code"`
		Message     string `json:"msg"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return &ProtocolError{Reason: "expected auth JSON or authorization_pending"}
	}
	if e.Code != nil && *e.Code != CodeSuccess {
		return &APIError{Code: *e.Code, Message: e.Message}
	}
	if e.Error != "" {
		if e.Error == "authorization_pending" {
			return ErrAuthorizationPending
		}
		return &OAuthError{ErrorCode: e.Error, Description: e.Description}
	}
	if err := json.Unmarshal(data, target); err != nil {
		return &ProtocolError{Reason: "auth data has unexpected shape"}
	}
	return nil
}

func (c *Client) token(ctx context.Context, body map[string]string, headers http.Header) (*Token, error) {
	body["client_id"] = c.clientID
	started := time.Now()
	data, err := c.request(ctx, http.MethodPost, c.accountURL, "/v1/auth/token", nil, body, headers, false)
	if err != nil {
		return nil, err
	}
	var result Token
	if err := decodeAuth(data, &result); err != nil {
		return nil, err
	}
	if result.AccessToken == "" || !validSeconds(result.ExpiresIn) {
		return nil, &ProtocolError{Reason: "access_token and positive expires_in are required"}
	}
	result.ExpiresAt = started.Add(time.Duration(result.ExpiresIn) * time.Second)
	return &result, nil
}

// PollDeviceToken performs one poll. Use errors.Is(err, ErrAuthorizationPending)
// to distinguish pending authorization from terminal errors.
func (c *Client) PollDeviceToken(ctx context.Context, device DeviceContext, deviceCode string) (*Token, error) {
	h, err := device.headers()
	if err != nil {
		return nil, err
	}
	if deviceCode == "" {
		return nil, invalid("deviceCode", "is required")
	}
	return c.token(ctx, map[string]string{"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "device_code": deviceCode}, h)
}

// WaitDeviceToken waits at least the returned interval before each poll. It stops
// at ExpiresAt, context cancellation, or any error except authorization_pending.
// For a deserialized DeviceCode, set ExpiresAt to its original issue-time deadline.
func (c *Client) WaitDeviceToken(ctx context.Context, device DeviceContext, code *DeviceCode) (*Token, error) {
	if code == nil || code.DeviceCode == "" || !validSeconds(code.Interval) || code.ExpiresAt.IsZero() {
		return nil, invalid("DeviceCode", "must include device_code, positive interval and original ExpiresAt")
	}
	if _, err := device.headers(); err != nil {
		return nil, err
	}
	pollCtx, cancel := context.WithDeadline(ctx, code.ExpiresAt)
	defer cancel()
	for {
		if err := wait(pollCtx, time.Duration(code.Interval)*time.Second); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrDeviceCodeExpired
		}
		token, err := c.PollDeviceToken(pollCtx, device, code.DeviceCode)
		if pollCtx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrDeviceCodeExpired
		}
		if !errors.Is(err, ErrAuthorizationPending) {
			return token, err
		}
	}
}

// RefreshToken does not mutate the client. Preserve the previous refresh token
// if the response omits it, then persist and install the returned access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	if refreshToken == "" {
		return nil, invalid("refreshToken", "is required")
	}
	return c.token(ctx, map[string]string{"grant_type": "refresh_token", "refresh_token": refreshToken}, nil)
}

type PKCE struct{ Verifier, Challenge string }

func GeneratePKCE() (*PKCE, error) {
	verifier, err := randomString()
	if err != nil {
		return nil, err
	}
	return &PKCE{Verifier: verifier, Challenge: PKCEChallenge(verifier)}, nil
}

func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func GenerateState() (string, error) { return randomString() }

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthorizationURL creates the browser navigation URL with fixed scope user offline.
func (c *Client) AuthorizationURL(redirectURI, state, challenge string) (string, error) {
	if redirectURI == "" || state == "" || challenge == "" {
		return "", invalid("redirectURI, state and challenge", "are required")
	}
	u, _ := url.Parse(c.authorizeURL) // Validated by NewClient.
	u.RawQuery = url.Values{"client_id": {c.clientID}, "response_type": {"code"}, "scope": {"user offline"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "redirect_uri": {redirectURI}, "state": {state}}.Encode()
	return u.String(), nil
}

// ParseOAuthCallback checks state before accepting either success or failure.
// The application must bind expectedState to the initiating user's session.
func ParseOAuthCallback(callbackURL, expectedState string) (string, error) {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return "", invalid("callbackURL", "is invalid")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", invalid("callbackURL", "has invalid query encoding")
	}
	if expectedState == "" || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(expectedState)) != 1 {
		return "", ErrStateMismatch
	}
	if len(q["error"]) > 1 || len(q["code"]) > 1 {
		return "", invalid("callbackURL", "has duplicate OAuth parameters")
	}
	if q.Get("error") != "" {
		return "", &OAuthError{ErrorCode: q.Get("error"), Description: q.Get("error_description")}
	}
	if q.Get("code") == "" {
		return "", invalid("callbackURL", "has no authorization code")
	}
	return q.Get("code"), nil
}

// ExchangeCode exchanges a callback code after the caller has validated state.
func (c *Client) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	if code == "" || redirectURI == "" {
		return nil, invalid("code and redirectURI", "are required")
	}
	if len(verifier) < 43 || len(verifier) > 128 || strings.IndexFunc(verifier, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r))
	}) >= 0 {
		return nil, invalid("verifier", "must contain 43-128 RFC 7636 unreserved characters")
	}
	return c.token(ctx, map[string]string{"grant_type": "authorization_code", "code": code, "code_verifier": verifier, "redirect_uri": redirectURI}, nil)
}

func DeviceAppURL(verificationURIComplete string) string {
	return "gyp://auth?" + url.Values{"url": {verificationURIComplete}}.Encode()
}

func wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
