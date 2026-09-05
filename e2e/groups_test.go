package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"controlplane/internal/auth"
)

var groupEditIDRe = regexp.MustCompile(`/groups/([0-9a-fA-F-]{36})/edit/`)

// createGroup creates a uniquely-named custom group via the web UI, granting
// the given module permissions (auth.Module* keys) and Application allow-list
// (empty applicationIDs = unrestricted), returning its name and ID.
func (s *adminSession) createGroup(t *testing.T, modules []string, applicationIDs []string) (name, id string) {
	t.Helper()
	name = fmt.Sprintf("e2e-group-%d", time.Now().UnixNano())
	token := s.csrfToken(t, "/groups/create/")
	form := url.Values{"csrf_token": {token}, "name": {name}}
	for _, m := range modules {
		form.Set("module_"+m, "on")
	}
	for _, appID := range applicationIDs {
		form.Add("application_ids", appID)
	}
	resp, err := s.http.PostForm(s.base+"/groups/create/", form)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	resp.Body.Close()

	listResp, err := s.http.Get(s.base + "/groups/?q=" + url.QueryEscape(name))
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read groups list: %v", err)
	}
	m := groupEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no group ID found for %q", name)
	}
	return name, string(m[1])
}

// createUserWithGroups creates a uniquely-named, active user via the web UI
// assigned to exactly groupIDs - the create form's default pre-checked "User"
// group checkbox is only a GET-time UI hint, so a real POST omitting
// group_ids assigns no groups at all - returning the user's ID, email, and
// fixed password.
func (s *adminSession) createUserWithGroups(t *testing.T, groupIDs []string) (id, email, password string) {
	t.Helper()
	email = fmt.Sprintf("e2e-group-user-%d@example.com", time.Now().UnixNano())
	password = "password12345"
	token := s.csrfToken(t, "/users/create/")
	form := url.Values{
		"csrf_token": {token},
		"email":      {email},
		"username":   {fmt.Sprintf("e2e-group-user-%d", time.Now().UnixNano())},
		"password1":  {password},
		"password2":  {password},
		"is_active":  {"on"},
	}
	for _, gid := range groupIDs {
		form.Add("group_ids", gid)
	}
	resp, err := s.http.PostForm(s.base+"/users/create/", form)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	resp.Body.Close()

	listResp, err := s.http.Get(s.base + "/users/?q=" + url.QueryEscape(email))
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read users list: %v", err)
	}
	m := userEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no user ID found for %q", email)
	}
	return string(m[1]), email, password
}

// builtinGroupID scrapes the ID of the built-in group named name (e.g.
// "User" or "Admin") off the /users/create/ form's group checkboxes - the
// groups list page never renders a built-in group's ID since it has no
// edit/delete link of its own.
func (s *adminSession) builtinGroupID(t *testing.T, name string) string {
	t.Helper()
	resp, err := s.http.Get(s.base + "/users/create/")
	if err != nil {
		t.Fatalf("GET /users/create/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /users/create/: %v", err)
	}
	re := regexp.MustCompile(`(?s)name="group_ids" value="([0-9a-fA-F-]{36})"(?: checked)?>\s*` + regexp.QuoteMeta(name) + `\s*</label>`)
	m := re.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no built-in group %q found on /users/create/", name)
	}
	return string(m[1])
}

// loginAs drives the real /login/ form as email/password on client (from
// newAnonymousClient, so the resulting session cookie is captured without
// silently following any redirects), for tests that need to act as a
// specific non-admin directory user.
func loginAs(t *testing.T, client *http.Client, base, email, password string) {
	t.Helper()
	token := csrfTokenFrom(t, client, base, "/login/")
	resp, err := client.PostForm(base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {password},
	})
	if err != nil {
		t.Fatalf("login as %s: %v", email, err)
	}
	resp.Body.Close()
}

func TestGroupsCRUD_CreateEditDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	name, id := admin.createGroup(t, []string{auth.ModuleConfigs, auth.ModuleFlags}, nil)

	editResp, err := admin.http.Get(base + "/groups/" + id + "/edit/")
	if err != nil {
		t.Fatalf("GET edit form: %v", err)
	}
	editResp.Body.Close()
	if editResp.StatusCode != http.StatusOK {
		t.Fatalf("edit form status = %d, want 200", editResp.StatusCode)
	}

	renamed := name + "-renamed"
	token := admin.csrfToken(t, "/groups/"+id+"/edit/")
	updateResp, err := admin.http.PostForm(base+"/groups/"+id+"/edit/", url.Values{
		"csrf_token":     {token},
		"name":           {renamed},
		"module_configs": {"on"},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	updateResp.Body.Close()

	listResp, err := admin.http.Get(base + "/groups/?q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET groups list: %v", err)
	}
	body, err := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if err != nil {
		t.Fatalf("read groups list: %v", err)
	}
	if !groupEditIDRe.Match(body) {
		t.Fatalf("renamed group %q not found in list", renamed)
	}

	deleteToken := admin.csrfToken(t, "/groups/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/groups/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDeleteResp, err := admin.http.Get(base + "/groups/?q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET groups list after delete: %v", err)
	}
	afterBody, err := io.ReadAll(afterDeleteResp.Body)
	afterDeleteResp.Body.Close()
	if err != nil {
		t.Fatalf("read groups list after delete: %v", err)
	}
	if groupEditIDRe.Match(afterBody) {
		t.Fatalf("deleted group %q still present in list", renamed)
	}
}

// TestGroupEditDelete_BuiltInGroupForbidden proves the IsSystem guard in
// GroupHandler.Edit/Delete, not just that the list page hides the links.
func TestGroupEditDelete_BuiltInGroupForbidden(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	userGroupID := admin.builtinGroupID(t, "User")

	editResp, err := admin.http.Get(base + "/groups/" + userGroupID + "/edit/")
	if err != nil {
		t.Fatalf("GET edit form for built-in group: %v", err)
	}
	editResp.Body.Close()
	if editResp.StatusCode != http.StatusForbidden {
		t.Fatalf("edit status = %d, want 403", editResp.StatusCode)
	}

	token := admin.csrfToken(t, "/groups/")
	deleteResp, err := admin.http.PostForm(base+"/groups/"+userGroupID+"/delete/", url.Values{
		"csrf_token": {token},
	})
	if err != nil {
		t.Fatalf("POST delete for built-in group: %v", err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusForbidden {
		t.Fatalf("delete status = %d, want 403", deleteResp.StatusCode)
	}
}

// TestGroupModulePermissions_RestrictsAccessToGrantedModulesOnly proves the
// real ModuleRequired(module) middleware, not just that CreateGroup/SetUserGroups
// round-trip: a user assigned only to a group granting "configs" can reach
// /configs/ but is bounced from /flags/ and /users/.
func TestGroupModulePermissions_RestrictsAccessToGrantedModulesOnly(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	_, groupID := admin.createGroup(t, []string{auth.ModuleConfigs}, nil)
	_, email, password := admin.createUserWithGroups(t, []string{groupID})

	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	configsResp, err := member.Get(base + "/configs/")
	if err != nil {
		t.Fatalf("GET /configs/: %v", err)
	}
	configsResp.Body.Close()
	if configsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /configs/ status = %d, want 200 (module granted)", configsResp.StatusCode)
	}

	flagsResp, err := member.Get(base + "/flags/")
	if err != nil {
		t.Fatalf("GET /flags/: %v", err)
	}
	flagsResp.Body.Close()
	if flagsResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /flags/ status = %d, want 302 (module not granted)", flagsResp.StatusCode)
	}
	if loc := flagsResp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("GET /flags/ Location = %q, want /dashboard/", loc)
	}

	usersResp, err := member.Get(base + "/users/")
	if err != nil {
		t.Fatalf("GET /users/: %v", err)
	}
	usersResp.Body.Close()
	if usersResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /users/ status = %d, want 302 (module not granted)", usersResp.StatusCode)
	}
}

// TestGroupApplicationScope_RestrictsConfigVisibilityToScopedApplication
// proves a group's Application allow-list (from Part A) actually filters
// what a member sees on /configs/, not just which modules they can reach.
func TestGroupApplicationScope_RestrictsConfigVisibilityToScopedApplication(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	_, allowedAppID := admin.createApplication(t)
	_, allowedEnvID := admin.createEnvironment(t, allowedAppID)
	admin.createConfig(t, allowedAppID, allowedEnvID, "ALLOWED_KEY", "allowed-value")

	_, otherAppID := admin.createApplication(t)
	_, otherEnvID := admin.createEnvironment(t, otherAppID)
	admin.createConfig(t, otherAppID, otherEnvID, "OTHER_KEY", "other-value")

	_, groupID := admin.createGroup(t, []string{auth.ModuleConfigs}, []string{allowedAppID})
	_, email, password := admin.createUserWithGroups(t, []string{groupID})

	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	listResp, err := member.Get(base + "/configs/")
	if err != nil {
		t.Fatalf("GET /configs/: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /configs/ status = %d, want 200", listResp.StatusCode)
	}
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read /configs/: %v", err)
	}
	if !strings.Contains(string(body), "ALLOWED_KEY") {
		t.Fatalf("expected ALLOWED_KEY (in-scope application) visible: %s", body)
	}
	if strings.Contains(string(body), "OTHER_KEY") {
		t.Fatalf("expected OTHER_KEY (out-of-scope application) hidden: %s", body)
	}
}
