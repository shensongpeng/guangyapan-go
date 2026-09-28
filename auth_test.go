package guangyapan

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthRoutingAndHeaders(t *testing.T) {
	var deviceCalls, tokenCalls atomic.Int32
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceCalls.Add(1)
		if r.URL.Path != "/v1/auth/device/code" || r.Method != "POST" {
			t.Error("wrong device route")
		}
		if r.Header.Get("x-client-id") != "client" || r.Header.Get("x-project-id") != "project" || r.Header.Get("x-device-id") != "device" || r.Header.Get("Authorization") != "" {
			t.Error("wrong device headers")
		}
		ts := r.Header.Get("timestamp")
		n, err := strconv.ParseInt(ts, 10, 64)
		if err != nil || time.Now().Unix()-n > 5 {
			t.Error("bad timestamp")
		}
		sum := md5.Sum([]byte("client_id=client&timestamp=" + ts + "&secret=s&=中文"))
		if r.Header.Get("sign") != hex.EncodeToString(sum[:]) {
			t.Error("incorrect signing or unwanted URL encoding")
		}
		checkBody(t, r, `{"client_id":"client","project_id":"project","scope":""}`)
		io.WriteString(w, `{"device_code":"dc","user_code":"uc","expires_in":120,"interval":3,"verification_url":"https://example.com/","verification_uri_complete":"https://example.com/?user_code=uc"}`)
	})
	account := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := tokenCalls.Add(1)
		if r.URL.Path != "/v1/auth/token" || r.Method != "POST" || r.Header.Get("x-client-id") != "client" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong token request")
		}
		for _, k := range []string{"timestamp", "sign", "Authorization"} {
			if r.Header.Get(k) != "" {
				t.Errorf("token request leaked %s", k)
			}
		}
		if n == 1 {
			if r.Header.Get("x-device-id") != "device" || r.Header.Get("x-project-id") != "project" {
				t.Error("missing device context")
			}
			checkBody(t, r, `{"client_id":"client","grant_type":"urn:ietf:params:oauth:grant-type:device_code","device_code":"dc"}`)
		} else {
			if r.Header.Get("x-device-id") != "" || r.Header.Get("x-project-id") != "" {
				t.Error("unexpected device headers")
			}
			if n == 2 {
				checkBody(t, r, `{"client_id":"client","grant_type":"refresh_token","refresh_token":"old"}`)
			} else {
				checkBody(t, r, `{"client_id":"client","grant_type":"authorization_code","code":"code","code_verifier":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG","redirect_uri":"https://app.example/cb"}`)
			}
		}
		io.WriteString(w, `{"token_type":"Bearer","access_token":"new","refresh_token":"rotated","expires_in":1800,"sub":"u"}`)
	})
	c, err := NewClient(Config{ClientID: "client", AccessToken: "existing", APIURL: "https://api.example", AccountURL: "https://account.example", HTTPClient: &http.Client{Transport: handlerTransport{func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "api.example" {
			api(w, r)
		} else if r.URL.Host == "account.example" {
			account(w, r)
		} else {
			t.Error("unexpected host")
		}
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	device := DeviceContext{"device", "project"}
	code, err := c.RequestDeviceCode(ctx, device, "s&=中文", "")
	if err != nil {
		t.Fatal(err)
	}
	if code.ExpiresAt.IsZero() || code.Interval != 3 {
		t.Error("device expiry missing")
	}
	token, err := c.PollDeviceToken(ctx, device, "dc")
	if err != nil {
		t.Fatal(err)
	}
	if token.Subject != "u" || token.ExpiresAt.IsZero() || token.RefreshToken != "rotated" {
		t.Error("token fields missing")
	}
	if _, err := c.RefreshToken(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExchangeCode(ctx, "code", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", "https://app.example/cb"); err != nil {
		t.Fatal(err)
	}
	if c.accessToken != "existing" {
		t.Error("auth mutated client token")
	}
	if deviceCalls.Load() != 1 || tokenCalls.Load() != 3 {
		t.Error("wrong auth hosts")
	}
}

func TestPKCEAndCallback(t *testing.T) {
	// RFC 7636 Appendix B known vector.
	if got := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal(got)
	}
	p, err := GeneratePKCE()
	if err != nil || len(p.Verifier) != 43 || p.Challenge != PKCEChallenge(p.Verifier) {
		t.Fatalf("PKCE: %v", err)
	}
	state, err := GenerateState()
	if err != nil || len(state) != 43 || state == p.Verifier {
		t.Fatal("invalid state")
	}
	c, _ := NewClient(Config{ClientID: "a&b"})
	redirect := "https://example.com/cb?next=中文&value=one two"
	u, err := c.AuthorizationURL(redirect, state, p.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if q.Get("scope") != "user offline" || q.Get("client_id") != "a&b" || q.Get("redirect_uri") != redirect || q.Get("code_challenge_method") != "S256" {
		t.Fatal(u)
	}
	code, err := ParseOAuthCallback("https://example.com/cb?code=abc&state="+state, state)
	if err != nil || code != "abc" {
		t.Fatal(err)
	}
	for _, cb := range []string{"?state=wrong&code=a", "?error=access_denied", "?state=" + state + "&state=" + state + "&code=a"} {
		if _, err := ParseOAuthCallback(cb, state); !errors.Is(err, ErrStateMismatch) {
			t.Fatalf("%s: %v", cb, err)
		}
	}
	_, err = ParseOAuthCallback("?state="+state+"&error=access_denied&error_description=denied", state)
	var oe *OAuthError
	if !errors.As(err, &oe) || oe.ErrorCode != "access_denied" {
		t.Fatal(err)
	}
	for _, cb := range []string{"?state=" + state, "?state=" + state + "&code=a&code=b", "?state=" + state + "&code=%ZZ"} {
		if _, err := ParseOAuthCallback(cb, state); err == nil {
			t.Fatal("accepted invalid callback")
		}
	}
	appURL, _ := url.Parse(DeviceAppURL("https://example.com/?a=1&b=2"))
	if appURL.Query().Get("url") != "https://example.com/?a=1&b=2" {
		t.Fatal(appURL)
	}
}

func TestAuthErrors(t *testing.T) {
	for _, body := range []string{"authorization_pending", "  authorization_pending\n", `{"error":"authorization_pending"}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := c.PollDeviceToken(context.Background(), DeviceContext{"d", "p"}, "dc")
		if !errors.Is(err, ErrAuthorizationPending) {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"unknown_failure", `{}`, `null`, `{"access_token":"a","expires_in":0}`, `{"access_token":"a","expires_in":9223372036854775807}`, `{"expires_in":120}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := c.RefreshToken(context.Background(), "r")
		var pe *ProtocolError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: %v", body, err)
		}
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":116,"msg":"invalid signature"}`)
	})
	_, err := c.RequestDeviceCode(context.Background(), DeviceContext{"d", "p"}, "s", "")
	if !IsCode(err, 116) {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"error":"invalid_grant","error_description":"expired"}`)
	})
	_, err = c.RefreshToken(context.Background(), "r")
	var oe *OAuthError
	if !errors.As(err, &oe) || oe.ErrorCode != "invalid_grant" {
		t.Fatal(err)
	}
}

func TestDevicePolling(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			io.WriteString(w, "authorization_pending")
		} else {
			io.WriteString(w, `{"access_token":"a","expires_in":120}`)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.WaitDeviceToken(ctx, DeviceContext{"d", "p"}, &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresAt: time.Now().Add(4 * time.Second)})
	if err != nil || calls.Load() != 2 || time.Since(start) < 2*time.Second {
		t.Fatalf("polls=%d err=%v", calls.Load(), err)
	}
	_, err = c.WaitDeviceToken(ctx, DeviceContext{"d", "p"}, &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresAt: time.Now().Add(-time.Second)})
	if !errors.Is(err, ErrDeviceCodeExpired) || calls.Load() != 2 {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, err = c.WaitDeviceToken(ctx2, DeviceContext{"d", "p"}, &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresAt: time.Now().Add(time.Minute)})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = c.WaitDeviceToken(ctx, DeviceContext{"d", "p"}, &DeviceCode{DeviceCode: "dc", Interval: 1})
	if err == nil {
		t.Error("accepted unknown issue time")
	}
}

func TestAuthInputValidation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	ctx := context.Background()
	checks := []func() error{
		func() error { _, e := c.RequestDeviceCode(ctx, DeviceContext{}, "s", ""); return e },
		func() error { _, e := c.RequestDeviceCode(ctx, DeviceContext{"d", "p"}, "", ""); return e },
		func() error { _, e := c.PollDeviceToken(ctx, DeviceContext{"d", "p"}, ""); return e },
		func() error { _, e := c.RefreshToken(ctx, ""); return e },
		func() error { _, e := c.ExchangeCode(ctx, "c", "short", "https://example.com"); return e },
		func() error {
			_, e := c.ExchangeCode(ctx, "c", strings.Repeat("!", 43), "https://example.com")
			return e
		},
		func() error { _, e := c.AuthorizationURL("", "s", "c"); return e },
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Error("missing validation")
		}
	}
}
