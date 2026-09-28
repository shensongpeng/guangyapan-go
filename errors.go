package guangyapan

import (
	"errors"
	"fmt"
)

const (
	CodeSuccess               = 0
	CodeInternal              = 101
	CodeAccessDenied          = 111
	CodeInvalidParameter      = 112
	CodeInvalidSignature      = 116
	CodeInvalidToken          = 117
	CodeInvalidClient         = 120
	CodeCapabilityDenied      = 123
	CodeTaskNotFound          = 145
	CodeFileNotFound          = 146
	CodeUploadProcessing      = 147
	CodeFileDeleted           = 149
	CodeUploadTaskNotFound    = 152
	CodeUploadCompleted       = 156
	CodeUploadExpired         = 163
	CodeResolutionRequiresVIP = 164
	CodeFileUnavailable       = 167
	CodeRequiresVIP           = 430
)

var (
	ErrAuthorizationPending = errors.New("guangyapan: authorization_pending")
	ErrDeviceCodeExpired    = errors.New("guangyapan: device code expired")
	ErrTaskFailed           = errors.New("guangyapan: asynchronous task failed")
	ErrStateMismatch        = errors.New("guangyapan: OAuth state mismatch")
)

// APIError is a non-success business code, including errors returned with HTTP 200.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("guangyapan: API code %d: %s", e.Code, e.Message)
}

// IsCode supports wrapped API errors.
func IsCode(err error, code int) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == code
}

// HTTPError deliberately omits response bodies, which may contain credentials.
type HTTPError struct {
	StatusCode int
	RetryAfter string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("guangyapan: HTTP status %d", e.StatusCode) }

type OAuthError struct {
	ErrorCode   string
	Description string
}

func (e *OAuthError) Error() string {
	return fmt.Sprintf("guangyapan: OAuth %s: %s", e.ErrorCode, e.Description)
}

// ProtocolError indicates a malformed or unexpected server response.
type ProtocolError struct{ Reason string }

func (e *ProtocolError) Error() string { return "guangyapan: invalid response: " + e.Reason }

func invalid(field, reason string) error { return fmt.Errorf("guangyapan: %s %s", field, reason) }
