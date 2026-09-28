// Package openapi implements Guangyapan OpenAPI v1.3 (2026-09-20).
//
// All network methods accept a context. Clients may be shared by goroutines.
// Only RequestDeviceCode uses a signing secret; token and business requests
// never send it. Token persistence and refresh scheduling belong to the caller.
// Object-storage byte transfer is outside the published OpenAPI contract.
package openapi
