package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestApplicationsCRUD_CreateEditDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	name, id := admin.createApplication(t)

	editResp, err := admin.http.Get(base + "/applications/" + id + "/edit/")
	if err != nil {
		t.Fatalf("GET edit form: %v", err)
	}
	defer editResp.Body.Close()
	if editResp.StatusCode != http.StatusOK {
		t.Fatalf("edit form status = %d, want 200", editResp.StatusCode)
	}

	renamed := name + "-renamed"
	token := admin.csrfToken(t, "/applications/"+id+"/edit/")
	updateResp, err := admin.http.PostForm(base+"/applications/"+id+"/edit/", url.Values{
		"csrf_token": {token},
		"name":       {renamed},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	updateResp.Body.Close()

	listResp, err := admin.http.Get(base + "/applications/?q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET applications list: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read applications list: %v", err)
	}
	if !applicationEditIDRe.Match(body) {
		t.Fatalf("renamed application %q not found in list", renamed)
	}

	deleteToken := admin.csrfToken(t, "/applications/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/applications/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDelete, err := admin.http.Get(base + "/applications/?q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET applications list after delete: %v", err)
	}
	defer afterDelete.Body.Close()
	afterBody, err := io.ReadAll(afterDelete.Body)
	if err != nil {
		t.Fatalf("read applications list after delete: %v", err)
	}
	if applicationEditIDRe.Match(afterBody) {
		t.Fatalf("deleted application %q still present in list", renamed)
	}
}

func TestApplicationsList_RequiresLogin(t *testing.T) {
	base := e2eBaseURL(t)
	anon := newAnonymousClient(t)

	resp, err := anon.Get(base + "/applications/")
	if err != nil {
		t.Fatalf("GET /applications/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}

// TestApplicationsList_RequiresAdmin proves the ModuleRequired("applications")
// check, not just LoginRequired: a logged-in user whose groups don't grant the
// applications module (the default built-in User group doesn't) must be
// bounced to /profile/ rather than allowed through — the default User group
// also lacks the dashboard module, so that's where the denial redirect lands.
func TestApplicationsList_RequiresAdmin(t *testing.T) {
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

	resp, err := member.Get(base + "/applications/")
	if err != nil {
		t.Fatalf("GET /applications/ as non-staff: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/profile/" {
		t.Fatalf("Location = %q, want /profile/", loc)
	}
}

func TestEnvironmentsCRUD_CreateEditDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	_, appID := admin.createApplication(t)

	name, id := admin.createEnvironment(t, appID)

	renamed := name + "-renamed"
	token := admin.csrfToken(t, "/environments/"+id+"/edit/")
	updateResp, err := admin.http.PostForm(base+"/environments/"+id+"/edit/", url.Values{
		"csrf_token":     {token},
		"application_id": {appID},
		"name":           {renamed},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	updateResp.Body.Close()

	listResp, err := admin.http.Get(base + "/environments/?application_id=" + url.QueryEscape(appID) + "&q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET environments list: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read environments list: %v", err)
	}
	if !environmentEditIDRe.Match(body) {
		t.Fatalf("renamed environment %q not found in list", renamed)
	}

	deleteToken := admin.csrfToken(t, "/environments/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/environments/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDelete, err := admin.http.Get(base + "/environments/?application_id=" + url.QueryEscape(appID) + "&q=" + url.QueryEscape(renamed))
	if err != nil {
		t.Fatalf("GET environments list after delete: %v", err)
	}
	defer afterDelete.Body.Close()
	afterBody, err := io.ReadAll(afterDelete.Body)
	if err != nil {
		t.Fatalf("read environments list after delete: %v", err)
	}
	if environmentEditIDRe.Match(afterBody) {
		t.Fatalf("deleted environment %q still present in list", renamed)
	}
}

// TestDeveloperGroup_CannotCreateApplicationsOrEnvironments_ButCanWriteConfigsAndFlags
// pins down the built-in Developer group's scope: its seeded permissions are
// dashboard/configs/flags (migration 00012_developer_group.sql), not
// applications/environments, so application/environment creation stays
// admin-only while configs and flags remain writable for Developer.
func TestDeveloperGroup_CannotCreateApplicationsOrEnvironments_ButCanWriteConfigsAndFlags(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	devGroupID := admin.builtinGroupID(t, "Developer")
	_, email, password := admin.createUserWithGroups(t, []string{devGroupID})

	member := newAnonymousClient(t)
	loginAs(t, member, base, email, password)

	createAppResp, err := member.Get(base + "/applications/create/")
	if err != nil {
		t.Fatalf("GET /applications/create/: %v", err)
	}
	createAppResp.Body.Close()
	if createAppResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /applications/create/ status = %d, want 302 (module not granted)", createAppResp.StatusCode)
	}
	if loc := createAppResp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("GET /applications/create/ Location = %q, want /dashboard/", loc)
	}

	createEnvResp, err := member.Get(base + "/environments/create/")
	if err != nil {
		t.Fatalf("GET /environments/create/: %v", err)
	}
	createEnvResp.Body.Close()
	if createEnvResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /environments/create/ status = %d, want 302 (module not granted)", createEnvResp.StatusCode)
	}
	if loc := createEnvResp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("GET /environments/create/ Location = %q, want /dashboard/", loc)
	}

	_, appID := admin.createApplication(t)
	_, envID := admin.createEnvironment(t, appID)

	configToken := csrfTokenFrom(t, member, base, "/configs/create/")
	configResp, err := member.PostForm(base+"/configs/create/", url.Values{
		"csrf_token":     {configToken},
		"application_id": {appID},
		"environment_id": {envID},
		"key":            {fmt.Sprintf("e2e-dev-key-%d", time.Now().UnixNano())},
		"value":          {"v"},
		"type":           {"string"},
	})
	if err != nil {
		t.Fatalf("POST /configs/create/: %v", err)
	}
	configResp.Body.Close()
	if configResp.StatusCode != http.StatusFound {
		t.Fatalf("POST /configs/create/ status = %d, want 302 (Developer has configs)", configResp.StatusCode)
	}
	if loc := configResp.Header.Get("Location"); loc != "/configs/" {
		t.Fatalf("POST /configs/create/ Location = %q, want /configs/ (create succeeded)", loc)
	}

	flagToken := csrfTokenFrom(t, member, base, "/flags/create/")
	flagResp, err := member.PostForm(base+"/flags/create/", url.Values{
		"csrf_token":     {flagToken},
		"application_id": {appID},
		"name":           {fmt.Sprintf("e2e-dev-flag-%d", time.Now().UnixNano())},
	})
	if err != nil {
		t.Fatalf("POST /flags/create/: %v", err)
	}
	flagResp.Body.Close()
	if flagResp.StatusCode != http.StatusFound {
		t.Fatalf("POST /flags/create/ status = %d, want 302 (Developer has flags)", flagResp.StatusCode)
	}
	if loc := flagResp.Header.Get("Location"); loc != "/flags/" {
		t.Fatalf("POST /flags/create/ Location = %q, want /flags/ (create succeeded)", loc)
	}
}
