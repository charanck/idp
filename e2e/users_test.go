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

var userEditIDRe = regexp.MustCompile(`/users/([0-9a-fA-F-]{36})/edit/`)

func TestUsersCRUD_CreateEditDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	email := fmt.Sprintf("e2e-user-crud-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, email, true)

	listResp, err := admin.http.Get(base + "/users/?q=" + url.QueryEscape(email))
	if err != nil {
		t.Fatalf("GET users list: %v", err)
	}
	body, err := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if err != nil {
		t.Fatalf("read users list: %v", err)
	}
	m := userEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no user ID found for %q", email)
	}
	id := string(m[1])

	renamedEmail := fmt.Sprintf("e2e-user-crud-renamed-%d@example.com", time.Now().UnixNano())
	token := admin.csrfToken(t, "/users/"+id+"/edit/")
	editResp, err := admin.http.PostForm(base+"/users/"+id+"/edit/", url.Values{
		"csrf_token": {token},
		"email":      {renamedEmail},
		"username":   {fmt.Sprintf("e2e-user-crud-renamed-%d", time.Now().UnixNano())},
		"is_active":  {"on"},
		"is_staff":   {"on"},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	editResp.Body.Close()

	afterEditResp, err := admin.http.Get(base + "/users/?q=" + url.QueryEscape(renamedEmail))
	if err != nil {
		t.Fatalf("GET users list after edit: %v", err)
	}
	afterEditBody, err := io.ReadAll(afterEditResp.Body)
	afterEditResp.Body.Close()
	if err != nil {
		t.Fatalf("read users list after edit: %v", err)
	}
	if !userEditIDRe.Match(afterEditBody) {
		t.Fatalf("renamed user %q not found in list", renamedEmail)
	}

	deleteToken := admin.csrfToken(t, "/users/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/users/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDeleteResp, err := admin.http.Get(base + "/users/?q=" + url.QueryEscape(renamedEmail))
	if err != nil {
		t.Fatalf("GET users list after delete: %v", err)
	}
	afterDeleteBody, err := io.ReadAll(afterDeleteResp.Body)
	afterDeleteResp.Body.Close()
	if err != nil {
		t.Fatalf("read users list after delete: %v", err)
	}
	if userEditIDRe.Match(afterDeleteBody) {
		t.Fatalf("deleted user %q still present in list", renamedEmail)
	}
}

// TestUsersDelete_CannotDeleteOwnAccount mirrors user_handler.go's explicit
// self-delete guard, which is enforced in the handler rather than by any
// database constraint.
func TestUsersDelete_CannotDeleteOwnAccount(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	whoamiResp, err := admin.http.Get(base + "/users/?q=" + url.QueryEscape(mustAdminEmail(t)))
	if err != nil {
		t.Fatalf("GET users list: %v", err)
	}
	body, err := io.ReadAll(whoamiResp.Body)
	whoamiResp.Body.Close()
	if err != nil {
		t.Fatalf("read users list: %v", err)
	}
	m := userEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Skip("admin user not found via search, skipping self-delete guard test")
	}
	id := string(m[1])

	token := admin.csrfToken(t, "/users/"+id+"/delete/")
	resp, err := admin.http.PostForm(base+"/users/"+id+"/delete/", url.Values{
		"csrf_token": {token},
	})
	if err != nil {
		t.Fatalf("POST delete own account: %v", err)
	}
	resp.Body.Close()

	dashResp, err := admin.http.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer dashResp.Body.Close()
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the admin session to survive a self-delete attempt, status = %d", dashResp.StatusCode)
	}
}
