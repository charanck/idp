package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/datatypes"

	apihttp "controlplane/api/http"
	notificationmodel "controlplane/internal/model/notification"
	"controlplane/internal/notification"
)

func newInAppRequest(bearer string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/notifications/inapp/unread", nil)
	if bearer != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestUnread_MissingBearerTokenReturns401(t *testing.T) {
	h := apihttp.NewInAppHandler(&fakeSessionValidator{}, &fakeUnreadConsumer{})

	c, _ := newInAppRequest("")
	err := h.ConsumeUnread(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestUnread_InvalidTokenReturns401(t *testing.T) {
	h := apihttp.NewInAppHandler(&fakeSessionValidator{err: errFakeNotificationService}, &fakeUnreadConsumer{})

	c, _ := newInAppRequest("bad-token")
	err := h.ConsumeUnread(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestUnread_ReturnsConsumedNotificationsAsJSON(t *testing.T) {
	consumer := &fakeUnreadConsumer{notifications: []notificationmodel.Notification{
		{ID: uuid.New(), Channel: "inapp", Status: notificationmodel.StatusSent, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`)},
	}}
	h := apihttp.NewInAppHandler(&fakeSessionValidator{claims: notification.SessionClaims{UserID: "user-1", ApplicationID: uuid.New()}}, consumer)

	c, rec := newInAppRequest("good-token")
	if err := h.ConsumeUnread(c); err != nil {
		t.Fatalf("ConsumeUnread: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body []struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body) != 1 || body[0].Channel != "inapp" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
