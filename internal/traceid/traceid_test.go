package traceid_test

import (
	"context"
	"net/http"
	"testing"

	"controlplane/internal/traceid"
)

const (
	tid  = "4bf92f3577b34da6a3ce929d0e0e4736"
	tid2 = "0af7651916cd43dd8448eb211c80319c"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		want   string
		ok     bool
	}{
		{"traceparent", map[string]string{"traceparent": "00-" + tid + "-00f067aa0ba902b7-01"}, tid, true},
		{"traceparent wins", map[string]string{"traceparent": "00-" + tid + "-00f067aa0ba902b7-01", "X-Trace-Id": tid2}, tid, true},
		{"x-trace-id", map[string]string{"X-Trace-Id": tid}, tid, true},
		{"x-trace-id beats x-request-id", map[string]string{"X-Trace-Id": tid, "X-Request-Id": tid2}, tid, true},
		{"x-request-id", map[string]string{"X-Request-Id": tid}, tid, true},
		{"uppercase normalized", map[string]string{"X-Trace-Id": "4BF92F3577B34DA6A3CE929D0E0E4736"}, tid, true},
		{"invalid traceparent falls through", map[string]string{"traceparent": "garbage", "X-Trace-Id": tid}, tid, true},
		{"zero traceparent falls through", map[string]string{"traceparent": "00-00000000000000000000000000000000-00f067aa0ba902b7-01", "X-Request-Id": tid}, tid, true},
		{"v00 extra field rejected", map[string]string{"traceparent": "00-" + tid + "-00f067aa0ba902b7-01-extra"}, "", false},
		{"non-hex ignored", map[string]string{"X-Trace-Id": "not-a-trace-id"}, "", false},
		{"short ignored", map[string]string{"X-Request-Id": "abc123"}, "", false},
		{"none", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.header {
				h.Set(k, v)
			}
			got, ok := traceid.Parse(h)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Parse = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestResolveGeneratesWhenMissing(t *testing.T) {
	id := traceid.Resolve(http.Header{})
	if len(id) != 32 {
		t.Fatalf("generated id %q is not 32 chars", id)
	}
	if other := traceid.New(); other == id {
		t.Fatal("generated ids should differ")
	}
}

func TestContextRoundTrip(t *testing.T) {
	if got := traceid.FromContext(context.Background()); got != "" {
		t.Fatalf("empty ctx = %q", got)
	}
	ctx := traceid.WithTraceID(context.Background(), tid)
	if got := traceid.FromContext(ctx); got != tid {
		t.Fatalf("FromContext = %q, want %q", got, tid)
	}
}
