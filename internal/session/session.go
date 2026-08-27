// Package session implements Redis-backed server-side sessions for the web
// UI: a signed session-ID cookie points at a JSON blob in Redis, and flash
// messages/CSRF tokens ride inside that same blob rather than needing
// separate storage.
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

const (
	// CookieName is the session-ID cookie.
	CookieName = "cp_session"
	contextKey = "webui_session"
	defaultTTL = 14 * 24 * time.Hour

	flashesKey = "_flashes"
	csrfKey    = "_csrf"
	userIDKey  = "_user_id"
)

// Store manages loading/persisting sessions against Redis.
type Store struct {
	rdb    *redis.Client
	secret []byte
	ttl    time.Duration
}

func NewStore(rdb *redis.Client, secret string, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Store{rdb: rdb, secret: []byte(secret), ttl: ttl}
}

// Flash is a one-time message queued for the next page render
// (tags: success/info/warning/error/danger).
type Flash struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

// Session is a single request's view of the session data. It is not
// goroutine-safe and must not be shared across requests.
type Session struct {
	id          string
	oldID       string
	data        map[string]any
	dirty       bool
	destroyed   bool
	regenerated bool
}

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the OS entropy source is broken; there is
		// nothing sane to do except produce a value that will simply never
		// match, so the caller fails closed rather than panicking mid-request.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func redisKey(id string) string { return "webui-session:" + id }

func (st *Store) sign(id string) string {
	mac := hmac.New(sha256.New, st.secret)
	mac.Write([]byte(id))
	return id + "." + hex.EncodeToString(mac.Sum(nil))
}

func (st *Store) verify(cookieValue string) (string, bool) {
	idx := strings.LastIndex(cookieValue, ".")
	if idx < 0 {
		return "", false
	}
	id, sig := cookieValue[:idx], cookieValue[idx+1:]
	if id == "" || sig == "" {
		return "", false
	}
	mac := hmac.New(sha256.New, st.secret)
	mac.Write([]byte(id))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}
	return id, true
}

func (st *Store) load(c echo.Context) *Session {
	cookie, err := c.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return &Session{data: map[string]any{}}
	}

	id, ok := st.verify(cookie.Value)
	if !ok {
		return &Session{data: map[string]any{}}
	}

	val, err := st.rdb.Get(c.Request().Context(), redisKey(id)).Result()
	if err != nil {
		return &Session{data: map[string]any{}}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(val), &data); err != nil || data == nil {
		return &Session{data: map[string]any{}}
	}
	return &Session{id: id, data: data}
}

func (st *Store) persist(c echo.Context, sess *Session) error {
	ctx := c.Request().Context()

	if sess.regenerated && sess.oldID != "" {
		st.rdb.Del(ctx, redisKey(sess.oldID))
	}

	if sess.destroyed {
		if sess.id != "" {
			if err := st.rdb.Del(ctx, redisKey(sess.id)).Err(); err != nil {
				return err
			}
		}
		c.SetCookie(&http.Cookie{
			Name: CookieName, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		return nil
	}

	if !sess.dirty {
		return nil
	}

	if sess.id == "" {
		sess.id = randomToken(32)
	}

	b, err := json.Marshal(sess.data)
	if err != nil {
		return err
	}
	if err := st.rdb.Set(ctx, redisKey(sess.id), string(b), st.ttl).Err(); err != nil {
		return err
	}

	secure := c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https")
	c.SetCookie(&http.Cookie{
		Name:     CookieName,
		Value:    st.sign(sess.id),
		Path:     "/",
		MaxAge:   int(st.ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Middleware loads the session (if any) before the handler runs and persists
// any changes (new session, modified data, or destroy) before the response is
// sent. Persisting is done via a Response.Before hook rather than simply
// after next(c) returns, because handlers commonly write the response body
// themselves mid-handler (redirects, rendered pages) - which flushes headers
// immediately - so setting the session cookie only after next(c) returns
// would silently drop it on every such request.
func (st *Store) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sess := st.load(c)
			c.Set(contextKey, sess)

			var persisted bool
			var persistErr error
			c.Response().Before(func() {
				if persisted {
					return
				}
				persisted = true
				persistErr = st.persist(c, sess)
			})

			err := next(c)

			if !persisted {
				persistErr = st.persist(c, sess)
			}
			if persistErr != nil && err == nil {
				err = persistErr
			}
			return err
		}
	}
}

// FromContext returns the current request's session, set up by Middleware.
func FromContext(c echo.Context) *Session {
	sess, _ := c.Get(contextKey).(*Session)
	return sess
}

func (s *Session) Get(key string) (any, bool) {
	v, ok := s.data[key]
	return v, ok
}

func (s *Session) GetString(key string) string {
	v, _ := s.data[key].(string)
	return v
}

func (s *Session) Set(key string, value any) {
	s.data[key] = value
	s.dirty = true
}

func (s *Session) Delete(key string) {
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		s.dirty = true
	}
}

// Pop reads and removes a key in one step, e.g. for OAuth state validation.
func (s *Session) Pop(key string) (any, bool) {
	v, ok := s.data[key]
	if ok {
		delete(s.data, key)
		s.dirty = true
	}
	return v, ok
}

func (s *Session) PopString(key string) (string, bool) {
	v, ok := s.Pop(key)
	if !ok {
		return "", false
	}
	str, ok := v.(string)
	return str, ok
}

// Regenerate rotates the session ID (deleting the old Redis entry on
// persist) as session-fixation protection: call this when a session
// transitions from anonymous to authenticated.
func (s *Session) Regenerate() {
	if s.id != "" {
		s.oldID = s.id
		s.regenerated = true
	}
	s.id = ""
	s.dirty = true
}

// Destroy deletes the session from Redis and clears the cookie, for logout.
func (s *Session) Destroy() {
	s.destroyed = true
}

// UserID returns the logged-in user's ID string, if any.
func (s *Session) UserID() (string, bool) {
	v := s.GetString(userIDKey)
	return v, v != ""
}

func (s *Session) SetUserID(id string) {
	s.Set(userIDKey, id)
}

func (s *Session) ClearUserID() {
	s.Delete(userIDKey)
}

// AddFlash queues a one-time message for the next page render.
func (s *Session) AddFlash(tag, text string) {
	flashes := s.peekFlashes()
	flashes = append(flashes, Flash{Tag: tag, Text: text})
	b, err := json.Marshal(flashes)
	if err != nil {
		return
	}
	s.data[flashesKey] = string(b)
	s.dirty = true
}

func (s *Session) peekFlashes() []Flash {
	raw, ok := s.data[flashesKey].(string)
	if !ok || raw == "" {
		return nil
	}
	var flashes []Flash
	if err := json.Unmarshal([]byte(raw), &flashes); err != nil {
		return nil
	}
	return flashes
}

// PopFlashes returns and clears all queued flash messages, for rendering
// once at the top of the next page.
func (s *Session) PopFlashes() []Flash {
	flashes := s.peekFlashes()
	if _, ok := s.data[flashesKey]; ok {
		delete(s.data, flashesKey)
		s.dirty = true
	}
	return flashes
}

// CSRFToken returns this session's CSRF token, generating and storing one on
// first use, mirroring {% csrf_token %}.
func (s *Session) CSRFToken() string {
	if tok, ok := s.data[csrfKey].(string); ok && tok != "" {
		return tok
	}
	tok := randomToken(32)
	s.data[csrfKey] = tok
	s.dirty = true
	return tok
}
