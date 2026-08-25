// Integration tests for service-client and activity-log web UI views,
// exercised through real HTTP requests (session auth) against the ephemeral
// test Postgres, mirroring web_ui/tests/test_client_views.py.
package webui_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"controlplane/internal/config"
)

const adminEmail = "admin@example.com"
const adminPassword = "password123"

func TestClientDelete_RequiresAdmin(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	client := newTestClient(t)
	csrf := fetchCSRFToken(t, client, baseURL, "/login/")
	resp := postForm(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/delete/", csrf, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	var count int64
	gdb.Model(&clientModel{}).Where("id = ?", creds.Client.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected client to survive a non-admin delete attempt, count = %d", count)
	}
}

func TestClientDelete_GetShowsConfirmationWithoutDeleting(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	createAdminUser(t, gdb, adminEmail, adminPassword)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	client := newTestClient(t)
	loginAs(t, client, baseURL, adminEmail, adminPassword)

	resp, err := client.Get(baseURL + "/clients/" + creds.Client.ID.String() + "/delete/")
	if err != nil {
		t.Fatalf("GET delete confirmation: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var count int64
	gdb.Model(&clientModel{}).Where("id = ?", creds.Client.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected GET not to delete, count = %d", count)
	}
}

func TestClientDelete_PostDeletesClient(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	createAdminUser(t, gdb, adminEmail, adminPassword)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	client := newTestClient(t)
	loginAs(t, client, baseURL, adminEmail, adminPassword)
	csrf := fetchCSRFToken(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/delete/")

	resp := postForm(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/delete/", csrf, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	var count int64
	gdb.Model(&clientModel{}).Where("id = ?", creds.Client.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected client to be deleted, count = %d", count)
	}
}

func TestClientRegenerateKey_RequiresAdmin(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	originalKey := creds.Client.EncryptionKey

	client := newTestClient(t)
	csrf := fetchCSRFToken(t, client, baseURL, "/login/")
	resp := postForm(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/regenerate-key/", csrf, nil)
	resp.Body.Close()

	var current clientModel
	gdb.First(&current, "id = ?", creds.Client.ID)
	if current.EncryptionKey != originalKey {
		t.Fatalf("expected encryption key to be unchanged for a non-admin request")
	}
}

func TestClientRegenerateKey_PostIssuesANewKey(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	createAdminUser(t, gdb, adminEmail, adminPassword)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	originalKey := creds.Client.EncryptionKey

	client := newTestClient(t)
	loginAs(t, client, baseURL, adminEmail, adminPassword)
	csrf := fetchCSRFToken(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/")

	resp := postForm(t, client, baseURL, "/clients/"+creds.Client.ID.String()+"/regenerate-key/", csrf, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	var current clientModel
	gdb.First(&current, "id = ?", creds.Client.ID)
	if current.EncryptionKey == originalKey {
		t.Fatal("expected a new encryption key")
	}
	if current.EncryptionKey == "" {
		t.Fatal("expected a non-empty encryption key")
	}
}

func TestClientRegenerateKey_GetDoesNotChangeTheKey(t *testing.T) {
	gdb, e, deps := setupWebUI(t)
	baseURL := newTestServer(t, e)
	createAdminUser(t, gdb, adminEmail, adminPassword)
	creds, err := deps.CreateServiceClient(context.Background(), "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	originalKey := creds.Client.EncryptionKey

	client := newTestClient(t)
	loginAs(t, client, baseURL, adminEmail, adminPassword)

	resp, err := client.Get(baseURL + "/clients/" + creds.Client.ID.String() + "/regenerate-key/")
	if err != nil {
		t.Fatalf("GET regenerate-key: %v", err)
	}
	resp.Body.Close()

	var current clientModel
	gdb.First(&current, "id = ?", creds.Client.ID)
	if current.EncryptionKey != originalKey {
		t.Fatal("expected GET not to change the encryption key")
	}
}

func TestActivityLogFilters_DeduplicatesResourceAndTypeOptions(t *testing.T) {
	gdb, e, _ := setupWebUI(t)
	baseURL := newTestServer(t, e)
	createAdminUser(t, gdb, adminEmail, adminPassword)

	for _, a := range []config.Activity{
		{Type: "create", Resource: "config", ResourceID: "1"},
		{Type: "update", Resource: "config", ResourceID: "1"},
		{Type: "delete", Resource: "user", ResourceID: "2"},
	} {
		if err := gdb.Create(&a).Error; err != nil {
			t.Fatalf("create activity: %v", err)
		}
	}

	client := newTestClient(t)
	loginAs(t, client, baseURL, adminEmail, adminPassword)

	resp, err := client.Get(baseURL + "/activity/")
	if err != nil {
		t.Fatalf("GET /activity/: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	if strings.Count(body, `<option value="config"`) != 1 {
		t.Fatalf("expected exactly one deduplicated \"config\" resource option, body: %s", body)
	}
	if !strings.Contains(body, `<option value="config"`) || !strings.Contains(body, `<option value="user"`) {
		t.Fatalf("expected both config and user resource options present, body: %s", body)
	}
	if strings.Count(body, `<option value="create"`) != 1 {
		t.Fatalf("expected exactly one deduplicated \"create\" action option, body: %s", body)
	}
}

// clientModel is a minimal local alias matching auth.ServiceClient's table so
// this file doesn't need to import the auth package just to re-read a row.
type clientModel struct {
	ID            string `gorm:"column:id;type:uuid;primaryKey"`
	EncryptionKey string `gorm:"column:encryption_key"`
}

func (clientModel) TableName() string { return "service_clients" }
