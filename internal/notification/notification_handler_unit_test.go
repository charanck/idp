package notification_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/datatypes"

	"controlplane/internal/notification"
)

var errFakeNotificationService = errors.New("fake notification service error")

func newNotificationRequest(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestCreate_UnknownChannelReturns400(t *testing.T) {
	h := notification.NewNotificationHandler(&fakeNotificationCreator{}, &fakeNotificationLister{}, &fakeNotificationGetter{}, notification.NewChannelRegistry())

	c, rec := newNotificationRequest(http.MethodPost, "/notifications", `{"channel":"carrier-pigeon","recipient":{},"content":{}}`)
	err := h.Create(c)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", httpErr.Code)
	}
	_ = rec
}

func TestCreate_ServiceErrorReturns500(t *testing.T) {
	creator := &fakeNotificationCreator{err: errFakeNotificationService}
	h := notification.NewNotificationHandler(creator, &fakeNotificationLister{}, &fakeNotificationGetter{}, notification.NewChannelRegistry())

	c, _ := newNotificationRequest(http.MethodPost, "/notifications", `{"channel":"email","recipient":{"email":"a@example.com"},"content":{"subject":"hi"}}`)
	err := h.Create(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", httpErr.Code)
	}
}

func TestCreate_ReturnsCreatedNotificationAsJSON(t *testing.T) {
	n := &notification.Notification{
		ID:        uuid.New(),
		Channel:   "email",
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
		Status:    notification.StatusQueued,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	creator := &fakeNotificationCreator{notification: n}
	h := notification.NewNotificationHandler(creator, &fakeNotificationLister{}, &fakeNotificationGetter{}, notification.NewChannelRegistry())

	c, rec := newNotificationRequest(http.MethodPost, "/notifications", `{"channel":"email","recipient":{"email":"a@example.com"},"content":{"subject":"hi"}}`)
	if err := h.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var resp struct {
		Channel string `json:"channel"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Channel != "email" || resp.Status != notification.StatusQueued {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestList_ReturnsNotificationsAsJSON(t *testing.T) {
	lister := &fakeNotificationLister{notifications: []notification.Notification{
		{ID: uuid.New(), Channel: "sms", Status: notification.StatusQueued, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`)},
	}}
	h := notification.NewNotificationHandler(&fakeNotificationCreator{}, lister, &fakeNotificationGetter{}, notification.NewChannelRegistry())

	c, rec := newNotificationRequest(http.MethodGet, "/notifications?channel=sms", "")
	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
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
	if len(body) != 1 || body[0].Channel != "sms" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestGet_NotFoundReturns404(t *testing.T) {
	getter := &fakeNotificationGetter{notification: nil}
	h := notification.NewNotificationHandler(&fakeNotificationCreator{}, &fakeNotificationLister{}, getter, notification.NewChannelRegistry())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/notifications/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(uuid.New().String())

	err := h.Get(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", httpErr.Code)
	}
}

func TestGet_InvalidIDReturns400(t *testing.T) {
	h := notification.NewNotificationHandler(&fakeNotificationCreator{}, &fakeNotificationLister{}, &fakeNotificationGetter{}, notification.NewChannelRegistry())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/notifications/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("not-a-uuid")

	err := h.Get(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", httpErr.Code)
	}
}
