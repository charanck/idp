package e2e

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var configEditIDRe = regexp.MustCompile(`/configs/([0-9a-fA-F-]{36})/edit/`)

// findConfigID scrapes the configs list, filtered by application and key,
// for the entry's ID off its "Edit" link - there's no other way to learn a
// config's ID from the web UI, which never round-trips it in a form value on
// the list page. Scoping by application_id (not just the key substring) is
// required: the e2e suite reuses fixed key names like "LIFECYCLE_KEY" across
// runs, and past runs' rows are never cleaned up, so an unscoped search can
// match a leftover row from a previous run instead of the one just created.
func (s *adminSession) findConfigID(t *testing.T, appID, key string) string {
	t.Helper()
	resp, err := s.http.Get(s.base + "/configs/?application_id=" + url.QueryEscape(appID) + "&q=" + url.QueryEscape(key))
	if err != nil {
		t.Fatalf("GET configs list: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read configs list: %v", err)
	}
	m := configEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no config ID found for key %q", key)
	}
	return string(m[1])
}

// TestConfigsLifecycle_CreateEditHistoryRollbackCloneDelete exercises every
// config web-UI action in one pass, since each step depends on state left by
// the previous one (an edit only produces a rollback-able version, etc).
func TestConfigsLifecycle_CreateEditHistoryRollbackCloneDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	_, appID := admin.createApplication(t)
	_, envID := admin.createEnvironment(t, appID)

	admin.createConfig(t, appID, envID, "LIFECYCLE_KEY", "v1")
	id := admin.findConfigID(t, appID, "LIFECYCLE_KEY")

	editToken := admin.csrfToken(t, "/configs/"+id+"/edit/")
	editResp, err := admin.http.PostForm(base+"/configs/"+id+"/edit/", url.Values{
		"csrf_token":     {editToken},
		"application_id": {appID},
		"environment_id": {envID},
		"key":            {"LIFECYCLE_KEY"},
		"value":          {"v2"},
		"type":           {"string"},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	editResp.Body.Close()

	historyResp, err := admin.http.Get(base + "/configs/" + id + "/history/")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	historyBody, err := io.ReadAll(historyResp.Body)
	historyResp.Body.Close()
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if historyResp.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d, want 200", historyResp.StatusCode)
	}
	rollbackFormRe := regexp.MustCompile(`/configs/` + id + `/rollback/(\d+)/`)
	versions := rollbackFormRe.FindAllSubmatch(historyBody, -1)
	if len(versions) < 2 {
		t.Fatalf("expected at least 2 versions (create + edit) in history, found %d: %s", len(versions), historyBody)
	}

	rollbackToVersion := string(versions[len(versions)-1][1]) // oldest listed = version 1, the original create
	rollbackToken := admin.csrfToken(t, "/configs/"+id+"/history/")
	rollbackResp, err := admin.http.PostForm(base+"/configs/"+id+"/rollback/"+rollbackToVersion+"/", url.Values{
		"csrf_token": {rollbackToken},
	})
	if err != nil {
		t.Fatalf("POST rollback: %v", err)
	}
	rollbackResp.Body.Close()

	afterRollbackHistory, err := admin.http.Get(base + "/configs/" + id + "/history/")
	if err != nil {
		t.Fatalf("GET history after rollback: %v", err)
	}
	defer afterRollbackHistory.Body.Close()
	afterRollbackBody, err := io.ReadAll(afterRollbackHistory.Body)
	if err != nil {
		t.Fatalf("read history after rollback: %v", err)
	}
	if versionsAfter := rollbackFormRe.FindAllSubmatch(afterRollbackBody, -1); len(versionsAfter) != len(versions)+1 {
		t.Fatalf("expected rollback to add a new version (%d -> %d): %s", len(versions), len(versionsAfter), afterRollbackBody)
	}

	cloneToken := admin.csrfToken(t, "/configs/"+id+"/clone/")
	cloneResp, err := admin.http.PostForm(base+"/configs/"+id+"/clone/", url.Values{
		"csrf_token":     {cloneToken},
		"application_id": {appID},
		"environment_id": {envID},
		"key":            {"LIFECYCLE_KEY_CLONE"},
		"value":          {"cloned"},
		"type":           {"string"},
		"submit_action":  {"create_single"},
	})
	if err != nil {
		t.Fatalf("POST clone: %v", err)
	}
	cloneResp.Body.Close()
	cloneID := admin.findConfigID(t, appID, "LIFECYCLE_KEY_CLONE")
	if cloneID == id {
		t.Fatal("cloned config has the same ID as the original")
	}

	deleteToken := admin.csrfToken(t, "/configs/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/configs/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDelete, err := admin.http.Get(base + "/configs/?application_id=" + url.QueryEscape(appID))
	if err != nil {
		t.Fatalf("GET configs list after delete: %v", err)
	}
	defer afterDelete.Body.Close()
	afterDeleteBody, err := io.ReadAll(afterDelete.Body)
	if err != nil {
		t.Fatalf("read configs list after delete: %v", err)
	}
	// q=LIFECYCLE_KEY would also substring-match the surviving clone's key
	// (LIFECYCLE_KEY_CLONE), so check for the deleted config's own ID instead
	// of re-deriving a query that happens to exclude it.
	if strings.Contains(string(afterDeleteBody), "/configs/"+id+"/edit/") {
		t.Fatalf("deleted config still present in list: %s", afterDeleteBody)
	}
}

func TestConfigsList_RequiresLogin(t *testing.T) {
	base := e2eBaseURL(t)
	anon := newAnonymousClient(t)

	resp, err := anon.Get(base + "/configs/")
	if err != nil {
		t.Fatalf("GET /configs/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}

var flagToggleFormRe = regexp.MustCompile(`/flags/([0-9a-fA-F-]{36})/toggle/`)

func TestFlagsLifecycle_CreateToggleDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	appName, appID := admin.createApplication(t)
	admin.createEnvironment(t, appID) // CreateFlag fans out across every environment of the app

	flagName := "e2e-lifecycle-flag"
	token := admin.csrfToken(t, "/flags/create/")
	createResp, err := admin.http.PostForm(base+"/flags/create/", url.Values{
		"csrf_token":     {token},
		"application_id": {appID},
		"name":           {flagName},
		"description":    {"created by e2e"},
	})
	if err != nil {
		t.Fatalf("POST create flag: %v", err)
	}
	createResp.Body.Close()

	listResp, err := admin.http.Get(base + "/flags/?application_id=" + url.QueryEscape(appID))
	if err != nil {
		t.Fatalf("GET flags list: %v", err)
	}
	listBody, err := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if err != nil {
		t.Fatalf("read flags list: %v", err)
	}
	m := flagToggleFormRe.FindSubmatch(listBody)
	if m == nil {
		t.Fatalf("no flag found for application %q after create: %s", appName, listBody)
	}
	flagID := string(m[1])

	toggleToken := admin.csrfToken(t, "/flags/?application_id="+url.QueryEscape(appID))
	toggleResp, err := admin.http.PostForm(base+"/flags/"+flagID+"/toggle/", url.Values{
		"csrf_token": {toggleToken},
	})
	if err != nil {
		t.Fatalf("POST toggle flag: %v", err)
	}
	toggleResp.Body.Close()
	if toggleResp.StatusCode != http.StatusOK && toggleResp.StatusCode != http.StatusFound {
		t.Fatalf("toggle status = %d, want 200 or 302", toggleResp.StatusCode)
	}

	deleteToken := admin.csrfToken(t, "/flags/"+flagID+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/flags/"+flagID+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete flag: %v", err)
	}
	deleteResp.Body.Close()

	afterDelete, err := admin.http.Get(base + "/flags/?application_id=" + url.QueryEscape(appID))
	if err != nil {
		t.Fatalf("GET flags list after delete: %v", err)
	}
	defer afterDelete.Body.Close()
	afterDeleteBody, err := io.ReadAll(afterDelete.Body)
	if err != nil {
		t.Fatalf("read flags list after delete: %v", err)
	}
	if flagToggleFormRe.Match(afterDeleteBody) {
		t.Fatalf("deleted flag still present in list: %s", afterDeleteBody)
	}
}
