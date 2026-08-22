package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/jwtutil"
	"controlplane/internal/ratelimit"
)

// requestContext builds the activity.Context for the current request,
// mirroring log_activity's request.user.email fallback.
func requestContext(c echo.Context) *activity.Context {
	ctx := &activity.Context{
		IPAddress: ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP()),
	}
	if user := UserFromContext(c); user != nil {
		ctx.UserEmail = user.Email
	}
	return ctx
}

// RegisterAuthRoutes mounts /register, /token, /me, /s2s/clients, /s2s/ping
// under the given group, mirroring authentication/api.py.
func RegisterAuthRoutes(g *echo.Group, deps *Deps) {
	g.POST("/register", registerUserHandler(deps), RateLimit(deps, "auth-register"), JWTAdminAuth(deps))
	g.POST("/token", createUserTokenHandler(deps), RateLimit(deps, "auth-token"))
	g.GET("/me", getCurrentUserHandler(deps), JWTAuth(deps))
	g.POST("/s2s/clients", createServiceClientHandler(deps), JWTAdminAuth(deps))
	g.GET("/s2s/ping", s2sPingHandler(deps), APIKeyAuth(deps))
}

type registerUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
}

func toUserResponse(u *auth.User) userResponse {
	return userResponse{ID: u.ID.String(), Email: u.Email, Username: u.Username, IsActive: u.IsActive}
}

func registerUserHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req registerUserRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if len(req.Password) < 8 {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "password must be at least 8 characters")
		}

		user, err := deps.AuthService.RegisterUser(req.Email, req.Password, "")
		if err != nil {
			if errors.Is(err, auth.ErrAlreadyExists) {
				return echo.NewHTTPError(http.StatusConflict, err.Error())
			}
			slog.Error("unexpected error registering user", "email", req.Email, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to register user")
		}

		deps.Activity.LogCreate("user", user.ID.String(), user.Email, requestContext(c), map[string]any{"created_by_api": true})
		return c.JSON(http.StatusOK, toUserResponse(user))
	}
}

type tokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func createUserTokenHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req tokenRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}

		user, err := deps.AuthService.AuthenticateUser(req.Username, req.Password)
		if err != nil {
			return err
		}
		if user == nil {
			deps.Activity.LogLoginFailed(req.Username, requestContext(c), "Invalid API token request")
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid email or password")
		}

		token, err := jwtutil.CreateAccessToken(deps.JWTSecretKey, user.ID.String(), "user", deps.JWTExpireMinutes)
		if err != nil {
			return err
		}
		deps.Activity.LogLogin(user.Email, requestContext(c), "API token issued")
		return c.JSON(http.StatusOK, tokenResponse{AccessToken: token, TokenType: "bearer"})
	}
}

func getCurrentUserHandler(_ *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		user := UserFromContext(c)
		return c.JSON(http.StatusOK, toUserResponse(user))
	}
}

type createServiceClientRequest struct {
	Name string `json:"name"`
}

type serviceClientResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ClientID      string `json:"client_id"`
	APIKeyID      string `json:"api_key_id"`
	APIKey        string `json:"api_key"`
	EncryptionKey string `json:"encryption_key"`
}

func createServiceClientHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req createServiceClientRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if len(req.Name) < 3 || len(req.Name) > 120 {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "name must be between 3 and 120 characters")
		}

		creds, err := deps.AuthService.CreateServiceClient(req.Name)
		if err != nil {
			if errors.Is(err, auth.ErrAlreadyExists) {
				return echo.NewHTTPError(http.StatusConflict, err.Error())
			}
			slog.Error("unexpected error creating service client", "name", req.Name, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create service client")
		}

		apiKeyID := ""
		if creds.Client.APIKeyID != nil {
			apiKeyID = *creds.Client.APIKeyID
		}
		deps.Activity.LogCreate("client", creds.Client.ID.String(), creds.Client.Name, requestContext(c), map[string]any{
			"created_by_api": true,
			"api_key_id":     apiKeyID,
		})
		return c.JSON(http.StatusOK, serviceClientResponse{
			ID:            creds.Client.ID.String(),
			Name:          creds.Client.Name,
			ClientID:      creds.Client.ID.String(),
			APIKeyID:      apiKeyID,
			APIKey:        creds.APIKey,
			EncryptionKey: creds.Client.EncryptionKey,
		})
	}
}

func s2sPingHandler(_ *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		client := ServiceClientFromContext(c)
		slog.Debug("S2S ping", "client", client.Name)
		return c.JSON(http.StatusOK, map[string]any{
			"ok": true,
			"identity": map[string]any{
				"method":    "api_key",
				"client_id": client.ID.String(),
				"name":      client.Name,
			},
		})
	}
}
