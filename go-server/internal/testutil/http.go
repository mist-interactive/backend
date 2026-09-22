package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ptr returns a pointer to the passed value. Convenient for constructing test models with pointer fields.
func Ptr[T any](v T) *T {
	return &v
}

// DecodeJSON unmarshals a test HTTP response body into target type T.
func DecodeJSON[T any](t testing.TB, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode response JSON: %v. Body: %s", err, rec.Body.String())
	}
	return out
}

// RequestOption configures an outgoing test HTTP request.
type RequestOption func(*http.Request)

// WithAuth sets the Authorization header on the test request.
func WithAuth(token string) RequestOption {
	return func(req *http.Request) {
		if token != "" {
			req.Header.Set("Authorization", token)
		}
	}
}

// WithHeader sets an arbitrary HTTP header on the test request.
func WithHeader(key, val string) RequestOption {
	return func(req *http.Request) {
		req.Header.Set(key, val)
	}
}

// WithCookie adds an HTTP cookie to the test request.
func WithCookie(cookie *http.Cookie) RequestOption {
	return func(req *http.Request) {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
}

// DoJSONRequest constructs and executes an HTTP request with an optional JSON body
// against the provided http.Handler, returning the recorded response.
func DoJSONRequest(handler http.Handler, method, target string, payload any, opts ...RequestOption) *httptest.ResponseRecorder {
	var body *bytes.Buffer
	if payload != nil {
		jsonBytes, _ := json.Marshal(payload)
		body = bytes.NewBuffer(jsonBytes)
	} else {
		body = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, target, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
