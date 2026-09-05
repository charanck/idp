package web

// Handlers aggregates every page/resource handler so router.go stays
// readable. Each field is itself built from narrow interfaces (see the
// individual *_handler.go files) - this is not a reintroduction of the old
// fat Deps bag of concrete dependencies.
type Handlers struct {
	Dashboard            *DashboardHandler
	Activity             *ActivityHandler
	Application          *ApplicationHandler
	Environment          *EnvironmentHandler
	Config               *ConfigHandler
	Flag                 *FlagHandler
	Client               *ClientHandler
	User                 *UserHandler
	Group                *GroupHandler
	Policy               *PolicyHandler
	Auth                 *AuthHandler
	OAuthLogin           *OAuthLoginHandler
	OAuthProvider        *OAuthProviderHandler
	OIDC                 *OIDCHandler
	NotificationSettings *NotificationSettingsHandler
	Notification         *NotificationHandler
}
