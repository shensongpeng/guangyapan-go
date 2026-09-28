package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type User struct {
	Subject string `json:"sub"`
}
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	Subject      string    `json:"sub"`
	ExpiresAt    time.Time `json:"-"`
}
type Captcha struct {
	Token     string `json:"captcha_token"`
	ExpiresIn int64  `json:"expires_in"`
	URL       string `json:"url"`
	// Challenge retains unknown challenge fields without trying to solve them.
	Challenge json.RawMessage `json:"challenge,omitempty"`
}
type Verification struct {
	ID string `json:"verification_id"`
}
type VerifiedCode struct {
	Token string `json:"verification_token"`
}

func (c *Client) GetUserInfo(ctx context.Context) (*User, error) {
	r, e := account[User](ctx, c, http.MethodGet, "/v1/user/me", nil, true, "")
	if e == nil && r.Subject == "" {
		return nil, ErrInvalidResponse
	}
	return r, e
}
func validateToken(r *Token, started time.Time) (*Token, error) {
	if r.AccessToken == "" || r.ExpiresIn < 0 || r.ExpiresIn > int64((1<<63-1)/time.Second) {
		return nil, ErrInvalidResponse
	}
	if r.ExpiresIn > 0 {
		r.ExpiresAt = started.Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return r, nil
}

// RefreshToken returns credentials; callers explicitly persist and install them.
// A missing refresh_token in the response preserves the supplied refresh token.
func (c *Client) RefreshToken(ctx context.Context, refresh string) (*Token, error) {
	if e := required(refresh); e != nil {
		return nil, e
	}
	started := time.Now()
	r, e := account[Token](ctx, c, http.MethodPost, "/v1/auth/token", map[string]string{"client_id": c.clientID, "grant_type": "refresh_token", "refresh_token": refresh}, false, "")
	if e != nil {
		return nil, e
	}
	if r.RefreshToken == "" {
		r.RefreshToken = refresh
	}
	return validateToken(r, started)
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func phoneE164(phone string) (string, error) {
	p := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, phone)
	if !phonePattern.MatchString(p) {
		return "", errors.New("webapi: phone must include E.164 country code")
	}
	return p, nil
}

// InitCaptcha requests the service's captcha metadata. It does not solve or
// bypass challenges. If interaction is required, the caller must complete it.
func (c *Client) InitCaptcha(ctx context.Context, phone string) (*Captcha, error) {
	p, e := phoneE164(phone)
	if e != nil {
		return nil, e
	}
	r, e := account[Captcha](ctx, c, http.MethodPost, "/v1/shield/captcha/init", map[string]any{"client_id": c.clientID, "device_id": c.deviceID, "action": "POST:/v1/auth/verification", "meta": map[string]string{"username": p, "phone_number": p, "VERIFICATION_PHONE": p}}, false, "")
	if e == nil && r.Token == "" {
		return r, ErrCaptchaRequired
	}
	return r, e
}

// SendSMS sends a real SMS. It is never called automatically by NewClient.
func (c *Client) SendSMS(ctx context.Context, phone, captchaToken string) (*Verification, error) {
	p, e := phoneE164(phone)
	if e != nil {
		return nil, e
	}
	r, e := account[Verification](ctx, c, http.MethodPost, "/v1/auth/verification", map[string]string{"client_id": c.clientID, "phone_number": p, "target": "ANY"}, false, captchaToken)
	if e == nil && r.ID == "" {
		return nil, ErrInvalidResponse
	}
	return r, e
}
func (c *Client) VerifySMS(ctx context.Context, verificationID, code string) (*VerifiedCode, error) {
	if e := required(verificationID, code); e != nil {
		return nil, e
	}
	r, e := account[VerifiedCode](ctx, c, http.MethodPost, "/v1/auth/verification/verify", map[string]string{"client_id": c.clientID, "verification_id": verificationID, "verification_code": code}, false, "")
	if e == nil && r.Token == "" {
		return nil, ErrInvalidResponse
	}
	return r, e
}
func (c *Client) SignIn(ctx context.Context, phone, code, verificationToken string) (*Token, error) {
	p, e := phoneE164(phone)
	if e != nil {
		return nil, e
	}
	if e := required(code, verificationToken); e != nil {
		return nil, e
	}
	started := time.Now()
	r, e := account[Token](ctx, c, http.MethodPost, "/v1/auth/signin", map[string]string{"client_id": c.clientID, "username": p, "verification_code": code, "verification_token": verificationToken}, false, "")
	if e != nil {
		return nil, e
	}
	return validateToken(r, started)
}
