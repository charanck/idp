package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"
)

var (
	brandingProductNameRe        = regexp.MustCompile(`(?s)id="product_name".*?value="([^"]*)"`)
	brandingLogoURLRe            = regexp.MustCompile(`(?s)id="logo_url".*?value="([^"]*)"`)
	brandingAccentColorRe        = regexp.MustCompile(`(?s)id="accent_color".*?value="([^"]*)"`)
	brandingBackgroundImageURLRe = regexp.MustCompile(`(?s)id="background_image_url".*?value="([^"]*)"`)
	loginAccentStyleRe           = regexp.MustCompile(`--accent:([^;"]*);`)
)

// brandingFormValues is the current (or submitted-back-on-error) state of the
// Branding settings form's four fields.
type brandingFormValues struct {
	ProductName        string
	LogoURL            string
	AccentColor        string
	BackgroundImageURL string
}

// currentBranding scrapes the persisted branding form fields off GET
// /branding/, mirroring currentSelfRegistrationDomains in policies_test.go.
func (s *adminSession) currentBranding(t *testing.T) brandingFormValues {
	t.Helper()
	resp, err := s.http.Get(s.base + "/branding/")
	if err != nil {
		t.Fatalf("GET /branding/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /branding/: %v", err)
	}
	return brandingFormValues{
		ProductName:        string(mustSubmatch(t, brandingProductNameRe, body, "product_name")),
		LogoURL:            string(mustSubmatch(t, brandingLogoURLRe, body, "logo_url")),
		AccentColor:        string(mustSubmatch(t, brandingAccentColorRe, body, "accent_color")),
		BackgroundImageURL: string(mustSubmatch(t, brandingBackgroundImageURLRe, body, "background_image_url")),
	}
}

func mustSubmatch(t *testing.T, re *regexp.Regexp, body []byte, field string) []byte {
	t.Helper()
	m := re.FindSubmatch(body)
	if m == nil {
		t.Fatalf("%s field not found on /branding/", field)
	}
	return m[1]
}

// updateBranding POSTs to /branding/ and returns the raw response body, for
// tests that need to inspect both the persisted state and any validation
// error rendered back onto the form.
func (s *adminSession) updateBranding(t *testing.T, v brandingFormValues) (*http.Response, []byte) {
	t.Helper()
	token := s.csrfToken(t, "/branding/")
	resp, err := s.http.PostForm(s.base+"/branding/", url.Values{
		"csrf_token":           {token},
		"product_name":         {v.ProductName},
		"logo_url":             {v.LogoURL},
		"accent_color":         {v.AccentColor},
		"background_image_url": {v.BackgroundImageURL},
	})
	if err != nil {
		t.Fatalf("POST /branding/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /branding/ response: %v", err)
	}
	return resp, body
}

// TestBrandingShow_UpdatesLoginScreenBranding proves the singleton Branding
// settings' GET/POST round-trip actually persists to the database (rather
// than just echoing the submitted form back) and that the saved values show
// up on the real, unauthenticated /login/ page - restoring the original
// values afterward since Branding is shared, live-instance state.
func TestBrandingShow_UpdatesLoginScreenBranding(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	original := admin.currentBranding(t)
	t.Cleanup(func() {
		admin.updateBranding(t, original)
	})

	updated := brandingFormValues{
		ProductName:        fmt.Sprintf("E2E Branding %d", time.Now().UnixNano()),
		LogoURL:            "https://example.com/e2e-logo.png",
		AccentColor:        "#00aa55",
		BackgroundImageURL: "https://example.com/e2e-background.jpg",
	}
	resp, body := admin.updateBranding(t, updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /branding/ status = %d, want 200, body: %s", resp.StatusCode, body)
	}

	if got := admin.currentBranding(t); got != updated {
		t.Fatalf("branding after save = %+v, want %+v", got, updated)
	}

	// The accent color, product name, logo, and background image are only
	// ever rendered on the anonymous login screen (see layout/base.templ) -
	// confirm the new values actually reach it, not just the settings form.
	loginResp, err := http.Get(base + "/login/")
	if err != nil {
		t.Fatalf("GET /login/: %v", err)
	}
	defer loginResp.Body.Close()
	loginBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		t.Fatalf("read /login/: %v", err)
	}
	loginHTML := string(loginBody)

	accentMatch := loginAccentStyleRe.FindStringSubmatch(loginHTML)
	if accentMatch == nil {
		t.Fatalf("--accent style not found on /login/")
	}
	if accentMatch[1] != updated.AccentColor {
		t.Fatalf("/login/ --accent = %q, want %q", accentMatch[1], updated.AccentColor)
	}
	if !containsHTMLEscaped(loginHTML, updated.ProductName) {
		t.Fatalf("/login/ does not contain product name %q", updated.ProductName)
	}
	if !containsHTMLEscaped(loginHTML, updated.LogoURL) {
		t.Fatalf("/login/ does not contain logo URL %q", updated.LogoURL)
	}
	if !containsHTMLEscaped(loginHTML, updated.BackgroundImageURL) {
		t.Fatalf("/login/ does not contain background image URL %q", updated.BackgroundImageURL)
	}
}

func containsHTMLEscaped(haystack, needle string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(needle)).MatchString(haystack)
}

// TestBrandingShow_RejectsInvalidAccentColor proves AuthService.UpdateBranding's
// hex-color validation is actually wired up through the web handler, and that
// a rejected submission never reaches the database.
func TestBrandingShow_RejectsInvalidAccentColor(t *testing.T) {
	admin := newAdminSession(t)

	original := admin.currentBranding(t)
	t.Cleanup(func() {
		admin.updateBranding(t, original)
	})

	invalid := original
	invalid.AccentColor = "javascript:alert(1)"
	resp, body := admin.updateBranding(t, invalid)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered form with error)", resp.StatusCode)
	}
	if !regexp.MustCompile(`alert-danger`).Match(body) {
		t.Fatalf("expected a validation error rendered on the form, body: %s", body)
	}

	if got := admin.currentBranding(t); got.AccentColor != original.AccentColor {
		t.Fatalf("accent_color persisted as %q despite invalid submission, want unchanged %q", got.AccentColor, original.AccentColor)
	}
}

// TestBrandingShow_RejectsInvalidBackgroundImageURL mirrors
// TestBrandingShow_RejectsInvalidAccentColor for BackgroundImageURL's
// http(s)-only validation.
func TestBrandingShow_RejectsInvalidBackgroundImageURL(t *testing.T) {
	admin := newAdminSession(t)

	original := admin.currentBranding(t)
	t.Cleanup(func() {
		admin.updateBranding(t, original)
	})

	invalid := original
	invalid.BackgroundImageURL = "javascript:alert(1)"
	resp, body := admin.updateBranding(t, invalid)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered form with error)", resp.StatusCode)
	}
	if !regexp.MustCompile(`alert-danger`).Match(body) {
		t.Fatalf("expected a validation error rendered on the form, body: %s", body)
	}

	if got := admin.currentBranding(t); got.BackgroundImageURL != original.BackgroundImageURL {
		t.Fatalf("background_image_url persisted as %q despite invalid submission, want unchanged %q", got.BackgroundImageURL, original.BackgroundImageURL)
	}
}

// TestBrandingShow_RequiresLogin proves /branding/ sits behind LoginRequired.
func TestBrandingShow_RequiresLogin(t *testing.T) {
	base := e2eBaseURL(t)
	anon := newAnonymousClient(t)

	resp, err := anon.Get(base + "/branding/")
	if err != nil {
		t.Fatalf("GET /branding/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); len(loc) < 7 || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}

// TestBrandingShow_RequiresAdmin proves the ModuleRequired("branding") check,
// not just LoginRequired: a logged-in user whose groups don't grant the
// branding module (the default built-in User group doesn't) must be bounced
// to the dashboard rather than allowed through.
func TestBrandingShow_RequiresAdmin(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email := fmt.Sprintf("e2e-nonstaff-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, email, true)

	member := newAnonymousClient(t)
	token := csrfTokenFrom(t, member, base, "/login/")
	loginResp, err := member.PostForm(base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {"password12345"},
	})
	if err != nil {
		t.Fatalf("login as non-staff user: %v", err)
	}
	loginResp.Body.Close()

	resp, err := member.Get(base + "/branding/")
	if err != nil {
		t.Fatalf("GET /branding/ as non-staff: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", loc)
	}
}
