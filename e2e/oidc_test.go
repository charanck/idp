package e2e

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// enableAuthApplication configures clientDBID as an OIDC auth application:
// a single redirectURI, optional consent requirement, and an allowed-groups
// list (empty = any directory user may log in).
func (s *adminSession) enableAuthApplication(t *testing.T, clientDBID, redirectURI string, requireConsent bool, allowedGroupIDs []string) {
	t.Helper()
	token := s.csrfToken(t, "/clients/"+clientDBID+"/edit/")
	form := url.Values{
		"csrf_token":          {token},
		"is_auth_application": {"on"},
		"redirect_uris":       {redirectURI},
	}
	if requireConsent {
		form.Set("require_consent", "on")
	}
	for _, gid := range allowedGroupIDs {
		form.Add("allowed_group_ids", gid)
	}
	resp, err := s.http.PostForm(s.base+"/clients/"+clientDBID+"/edit/", form)
	if err != nil {
		t.Fatalf("enable auth application: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("enable auth application status = %d: %s", resp.StatusCode, body)
	}
}

// fetchJWKSPublicKey fetches /.well-known/jwks.json and manually reconstructs
// the first key's RSA public key - no JWK-parsing library is vendored here.
func fetchJWKSPublicKey(t *testing.T, base string) *rsa.PublicKey {
	t.Helper()
	resp, err := http.Get(base + "/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("GET jwks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("jwks status = %d: %s", resp.StatusCode, body)
	}
	var doc struct {
		Keys []struct {
			N string `json:"n"`
			E string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(doc.Keys) == 0 {
		t.Fatalf("jwks contains no keys")
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].N)
	if err != nil {
		t.Fatalf("decode jwks n: %v", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].E)
	if err != nil {
		t.Fatalf("decode jwks e: %v", err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}
}

// verifyRS256 parses and signature-verifies tokenString against pubKey,
// returning its claims.
func verifyRS256(t *testing.T, tokenString string, pubKey *rsa.PublicKey) jwt.MapClaims {
	t.Helper()
	token, err := jwt.Parse(tokenString, func(tok *jwt.Token) (any, error) {
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		t.Fatalf("invalid token claims")
	}
	return claims
}

// driveOIDCAuthorize drives GET /oauth2/authorize on an already-logged-in
// client, returning the final redirect Location URL (either back to
// redirectURI with a code, an error redirect, or nil if a 200 page was
// rendered instead - callers check statusCode/body themselves in that case).
func driveOIDCAuthorize(t *testing.T, client *http.Client, base, clientID, redirectURI, scope, state, nonce string) (*http.Response, string) {
	t.Helper()
	authorizeURL := fmt.Sprintf("%s/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&nonce=%s",
		base,
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scope),
		url.QueryEscape(state),
		url.QueryEscape(nonce),
	)
	resp, err := client.Get(authorizeURL)
	if err != nil {
		t.Fatalf("GET /oauth2/authorize: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /oauth2/authorize response: %v", err)
	}
	return resp, string(body)
}

// exchangeOIDCCode calls POST /oauth2/token with client_secret_post
// authentication, returning the decoded ID/access tokens and status code.
func exchangeOIDCCode(t *testing.T, base, clientID, clientSecret, code, redirectURI string) (idToken, accessToken string, statusCode int) {
	t.Helper()
	resp, err := http.PostForm(base+"/oauth2/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		t.Fatalf("POST /oauth2/token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", resp.StatusCode
	}
	var body struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return body.IDToken, body.AccessToken, resp.StatusCode
}

func TestOIDCDiscovery_ServesMetadataAndJWKS(t *testing.T) {
	base := e2eBaseURL(t)

	discResp, err := http.Get(base + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("GET discovery: %v", err)
	}
	defer discResp.Body.Close()
	if discResp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status = %d", discResp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(discResp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	for _, field := range []string{"issuer", "authorization_endpoint", "token_endpoint", "userinfo_endpoint", "jwks_uri"} {
		if doc[field] == nil || doc[field] == "" {
			t.Fatalf("discovery missing field %q: %+v", field, doc)
		}
	}

	pubKey := fetchJWKSPublicKey(t, base)
	if pubKey.N.Sign() == 0 {
		t.Fatalf("jwks public key modulus is zero")
	}
}

func TestOIDCFlow_FullAuthorizationCodeFlow(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, clientSecret, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://relying-party.example.com/callback"
	admin.enableAuthApplication(t, clientDBID, redirectURI, false, nil)

	_, email, password := admin.createUserWithGroups(t, []string{admin.builtinGroupID(t, "User")})
	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	resp, _ := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid email groups", "state-123", "nonce-abc")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if location.Query().Get("state") != "state-123" {
		t.Fatalf("redirect state = %q, want state-123", location.Query().Get("state"))
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect location: %s", location.String())
	}

	idToken, accessToken, status := exchangeOIDCCode(t, base, clientID, clientSecret, code, redirectURI)
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d", status)
	}
	if idToken == "" || accessToken == "" {
		t.Fatalf("empty tokens returned")
	}

	pubKey := fetchJWKSPublicKey(t, base)
	idClaims := verifyRS256(t, idToken, pubKey)
	if idClaims["email"] != email {
		t.Fatalf("id_token email = %v, want %q", idClaims["email"], email)
	}
	if idClaims["nonce"] != "nonce-abc" {
		t.Fatalf("id_token nonce = %v, want nonce-abc", idClaims["nonce"])
	}
	groupsClaim, ok := idClaims["groups"].([]any)
	if !ok || len(groupsClaim) == 0 {
		t.Fatalf("id_token groups claim missing or empty: %v", idClaims["groups"])
	}

	userinfoReq, err := http.NewRequest(http.MethodGet, base+"/oauth2/userinfo", nil)
	if err != nil {
		t.Fatalf("build userinfo request: %v", err)
	}
	userinfoReq.Header.Set("Authorization", "Bearer "+accessToken)
	userinfoResp, err := http.DefaultClient.Do(userinfoReq)
	if err != nil {
		t.Fatalf("GET /oauth2/userinfo: %v", err)
	}
	defer userinfoResp.Body.Close()
	if userinfoResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(userinfoResp.Body)
		t.Fatalf("userinfo status = %d: %s", userinfoResp.StatusCode, body)
	}
	var userinfo map[string]any
	if err := json.NewDecoder(userinfoResp.Body).Decode(&userinfo); err != nil {
		t.Fatalf("decode userinfo: %v", err)
	}
	if userinfo["sub"] != idClaims["sub"] {
		t.Fatalf("userinfo sub = %v, want %v", userinfo["sub"], idClaims["sub"])
	}
}

func TestOIDCFlow_RequireConsent_AllowAndDeny(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, _, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://consent-rp.example.com/callback"
	admin.enableAuthApplication(t, clientDBID, redirectURI, true, nil)

	_, email, password := admin.createUserWithGroups(t, []string{admin.builtinGroupID(t, "User")})
	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	resp, body := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid", "state-consent", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize (consent screen) status = %d, want 200", resp.StatusCode)
	}
	csrfM := csrfTokenRe.FindStringSubmatch(body)
	if csrfM == nil {
		t.Fatalf("no CSRF token found on consent page")
	}

	denyResp, err := member.PostForm(base+"/oauth2/authorize", url.Values{
		"csrf_token":    {csrfM[1]},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid"},
		"state":         {"state-consent"},
		"decision":      {"deny"},
	})
	if err != nil {
		t.Fatalf("POST deny: %v", err)
	}
	denyResp.Body.Close()
	if denyResp.StatusCode != http.StatusFound {
		t.Fatalf("deny status = %d, want 302", denyResp.StatusCode)
	}
	denyLocation, err := url.Parse(denyResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse deny redirect: %v", err)
	}
	if denyLocation.Query().Get("error") != "access_denied" {
		t.Fatalf("deny redirect error = %q, want access_denied", denyLocation.Query().Get("error"))
	}

	resp2, body2 := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid", "state-consent-2", "")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("authorize (consent screen 2) status = %d, want 200", resp2.StatusCode)
	}
	csrfM2 := csrfTokenRe.FindStringSubmatch(body2)
	if csrfM2 == nil {
		t.Fatalf("no CSRF token found on consent page (2)")
	}
	allowResp, err := member.PostForm(base+"/oauth2/authorize", url.Values{
		"csrf_token":    {csrfM2[1]},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid"},
		"state":         {"state-consent-2"},
		"decision":      {"allow"},
	})
	if err != nil {
		t.Fatalf("POST allow: %v", err)
	}
	allowResp.Body.Close()
	if allowResp.StatusCode != http.StatusFound {
		t.Fatalf("allow status = %d, want 302", allowResp.StatusCode)
	}
	allowLocation, err := url.Parse(allowResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse allow redirect: %v", err)
	}
	if allowLocation.Query().Get("code") == "" {
		t.Fatalf("allow redirect missing code: %s", allowLocation.String())
	}
}

func TestOIDCAuthorize_RejectsUserOutsideAllowedGroups(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, _, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://restricted-rp.example.com/callback"
	_, restrictedGroupID := admin.createGroup(t, nil, nil)
	admin.enableAuthApplication(t, clientDBID, redirectURI, false, []string{restrictedGroupID})

	_, email, password := admin.createUserWithGroups(t, []string{admin.builtinGroupID(t, "User")})
	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	resp, body := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid", "state-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize status = %d, want 200 (error page)", resp.StatusCode)
	}
	if !strings.Contains(body, "authorized to log into this application") {
		t.Fatalf("expected 'not authorized' error page, got: %s", body)
	}
}

func TestOIDCAuthorize_RequiresLogin(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, _, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://anon-rp.example.com/callback"
	admin.enableAuthApplication(t, clientDBID, redirectURI, false, nil)

	anon := newAnonymousClient(t)
	resp, _ := driveOIDCAuthorize(t, anon, base, clientID, redirectURI, "openid", "state-1", "")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/login/?next=") {
		t.Fatalf("redirect location = %q, want /login/?next=...", location)
	}
}

func TestOIDCAuthorize_RejectsUnregisteredRedirectURI(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, _, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	admin.enableAuthApplication(t, clientDBID, "https://registered-rp.example.com/callback", false, nil)

	resp, body := driveOIDCAuthorize(t, admin.http, base, clientID, "https://unregistered.example.com/callback", "openid", "state-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize status = %d, want 200 (error page)", resp.StatusCode)
	}
	if !strings.Contains(body, "registered to log in through this identity provider") {
		t.Fatalf("expected 'isn't registered' error page, got: %s", body)
	}
}

func TestOIDCToken_RejectsReplayedAuthorizationCode(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, clientSecret, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://replay-rp.example.com/callback"
	admin.enableAuthApplication(t, clientDBID, redirectURI, false, nil)

	_, email, password := admin.createUserWithGroups(t, []string{admin.builtinGroupID(t, "User")})
	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	resp, _ := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid", "state-replay", "")
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect location: %s", location.String())
	}

	_, _, firstStatus := exchangeOIDCCode(t, base, clientID, clientSecret, code, redirectURI)
	if firstStatus != http.StatusOK {
		t.Fatalf("first exchange status = %d, want 200", firstStatus)
	}

	_, _, secondStatus := exchangeOIDCCode(t, base, clientID, clientSecret, code, redirectURI)
	if secondStatus != http.StatusBadRequest {
		t.Fatalf("replayed exchange status = %d, want 400", secondStatus)
	}
}

func TestOIDCToken_RejectsWrongClientSecret(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	clientID, _, ok := strings.Cut(apiKey, ".")
	if !ok {
		t.Fatalf("unexpected API key format: %q", apiKey)
	}
	const redirectURI = "https://wrongsecret-rp.example.com/callback"
	admin.enableAuthApplication(t, clientDBID, redirectURI, false, nil)

	_, email, password := admin.createUserWithGroups(t, []string{admin.builtinGroupID(t, "User")})
	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	resp, _ := driveOIDCAuthorize(t, member, base, clientID, redirectURI, "openid", "state-1", "")
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect location: %s", location.String())
	}

	_, _, status := exchangeOIDCCode(t, base, clientID, "wrong-secret", code, redirectURI)
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-secret exchange status = %d, want 401", status)
	}
}
