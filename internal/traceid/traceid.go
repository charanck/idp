// Package traceid extracts a request's trace ID from its headers (or mints a
// W3C-compatible one) and carries it on a context.Context, so the request
// logger, activity log and notification pipeline can all correlate to the
// caller's trace without threading an extra parameter.
package traceid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// ResponseHeader is the header the resolved trace ID is echoed back in.
const ResponseHeader = "X-Trace-Id"

// fallbackHeaders are consulted, in order, after traceparent.
var fallbackHeaders = []string{"X-Trace-Id", "X-Request-Id"}

type ctxKey struct{}

// WithTraceID returns a copy of ctx carrying id.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the trace ID stored by WithTraceID, or "" if none.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// New generates a random, non-zero W3C-compatible trace ID (32 lowercase hex).
func New() string {
	var b [16]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		if id := hex.EncodeToString(b[:]); isValid(id) {
			return id
		}
	}
}

// Parse extracts a trace ID from h, trying the W3C traceparent header first,
// then X-Trace-Id, then X-Request-Id. A header whose value isn't a valid
// 32-hex, non-zero trace ID is skipped in favor of the next one. ok is false
// if none yielded a valid ID.
func Parse(h http.Header) (id string, ok bool) {
	if id, ok := parseTraceparent(h.Get("traceparent")); ok {
		return id, true
	}
	for _, name := range fallbackHeaders {
		v := strings.ToLower(strings.TrimSpace(h.Get(name)))
		if isValid(v) {
			return v, true
		}
	}
	return "", false
}

// Resolve returns Parse's result, or a newly generated ID if absent/invalid.
func Resolve(h http.Header) string {
	if id, ok := Parse(h); ok {
		return id
	}
	return New()
}

// parseTraceparent validates version-traceid-parentid-flags
// (00-<32hex>-<16hex>-<2hex>) and returns the trace ID field.
func parseTraceparent(v string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) < 4 || len(parts[0]) != 2 || !isHex(parts[0]) || parts[0] == "ff" ||
		len(parts[2]) != 16 || !isHex(parts[2]) || parts[2] == "0000000000000000" ||
		len(parts[3]) != 2 || !isHex(parts[3]) {
		return "", false
	}
	// Version 00 must have exactly four fields; future versions may add more.
	if parts[0] == "00" && len(parts) != 4 {
		return "", false
	}
	id := strings.ToLower(parts[1])
	return id, isValid(id)
}

func isValid(id string) bool {
	return len(id) == 32 && isHex(id) && id != "00000000000000000000000000000000"
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
