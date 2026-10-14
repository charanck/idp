package e2e

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"testing"
)

// perRequestIPTransport gives every request without an explicit
// X-Forwarded-For its own random client IP. The server's rate limiters
// (login and S2S API-key) bucket by client IP, so without this every test
// shares one loopback bucket and the suite's pass/fail depends on how many
// requests ran inside the limiter window - i.e. on test order and speed.
// Tests that exercise the limiter itself pin their own X-Forwarded-For (see
// pinnedClientIP) so they fill only their own bucket.
type perRequestIPTransport struct {
	next http.RoundTripper
}

func (p perRequestIPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("X-Forwarded-For") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("X-Forwarded-For", randomClientIP())
	}
	return p.next.RoundTrip(req)
}

func randomClientIP() string {
	return fmt.Sprintf("10.%d.%d.%d", rand.Intn(256), rand.Intn(256), 1+rand.Intn(254))
}

// pinnedClientIP returns a fresh random IP for a test to reuse across
// several requests (via an explicit X-Forwarded-For header).
func pinnedClientIP() string { return randomClientIP() }

func TestMain(m *testing.M) {
	http.DefaultTransport = perRequestIPTransport{next: http.DefaultTransport}
	os.Exit(m.Run())
}
