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

// TestApplicationsList_RequiresAdmin proves the AdminRequired staff check,
// not just LoginRequired: a logged-in but non-staff user must be bounced to
// the dashboard rather than allowed through.
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
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", loc)
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
