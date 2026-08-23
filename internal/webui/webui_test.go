package webui_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/cache"
	"controlplane/internal/config"
	"controlplane/internal/crypto"
	"controlplane/internal/ratelimit"
	"controlplane/internal/security"
	"controlplane/internal/session"
	"controlplane/internal/testutil"
	"controlplane/internal/webui"
)

// setupWebUI wires a *webui.Deps against the ephemeral test Postgres +
// miniredis, and registers every route on a fresh *echo.Echo with the same
// session/CSRF middleware stack main.go uses for non-API routes.
func setupWebUI(t *testing.T) (*gorm.DB, *echo.Echo, *webui.Deps) {
	t.Helper()
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	encryption := crypto.NewEncryptionService(masterKey)
	noopCache := cache.NewNoopCache()

	deps := &webui.Deps{
		DB:            gdb,
		AuthService:   auth.NewAuthService(gdb),
		OAuthService:  auth.NewOAuthService(gdb),
		ConfigService: config.NewConfigService(gdb, encryption, noopCache, time.Minute),
		FlagService:   config.NewFeatureFlagService(gdb, noopCache, time.Minute),
		Activity:      activity.NewLogger(gdb),
		RateLimiter:   ratelimit.NewLimiter(rdb),
		Sessions:      session.NewStore(rdb, "test-session-secret", 0),
		Encryption:    encryption,

		AuthRateLimit:              1000,
		AuthRateLimitWindowSeconds: 60,
	}

	e := echo.New()
	e.Use(deps.Sessions.Middleware())
	e.Use(webui.CSRFProtect())
	webui.RegisterRoutes(e, deps)

	return gdb, e, deps
}

// newTestClient returns an http.Client with a cookie jar (for session
// persistence across requests) that does not follow redirects, matching
// Django's test Client returning the redirect response itself rather than
// the page it points to.
func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var csrfTokenRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func fetchCSRFToken(t *testing.T, client *http.Client, baseURL, path string) string {
	t.Helper()
	resp, err := client.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	m := csrfTokenRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf_token found in %s body: %s", path, body)
	}
	return string(m[1])
}

// createAdminUser inserts an active, staff user directly (bypassing
// AuthService.RegisterUser, which always creates inactive users) with a
// known plaintext password for logging in through the real /login/ flow.
func createAdminUser(t *testing.T, gdb *gorm.DB, email, password string) *auth.User {
	t.Helper()
	hashed, err := security.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &auth.User{Email: email, Username: "admin", Password: hashed, IsStaff: true, IsActive: true}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	return user
}

// loginAs drives the real /login/ form (GET for the CSRF token, then POST
// credentials), asserting the login itself succeeded.
func loginAs(t *testing.T, client *http.Client, baseURL, email, password string) {
	t.Helper()
	csrf := fetchCSRFToken(t, client, baseURL, "/login/")

	form := url.Values{"csrf_token": {csrf}, "username": {email}, "password": {password}}
	resp, err := client.PostForm(baseURL+"/login/", form)
	if err != nil {
		t.Fatalf("POST /login/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: status = %d, body = %s", resp.StatusCode, body)
	}
}

func postForm(t *testing.T, client *http.Client, baseURL, path, csrfToken string, extra url.Values) *http.Response {
	t.Helper()
	form := url.Values{"csrf_token": {csrfToken}}
	for k, vs := range extra {
		form[k] = vs
	}
	resp, err := client.PostForm(baseURL+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func newTestServer(t *testing.T, e *echo.Echo) string {
	t.Helper()
	ts := httptest.NewServer(e)
	t.Cleanup(ts.Close)
	return ts.URL
}
