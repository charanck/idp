package auth

import "context"

// ServiceClientAuthenticator adapts AuthService's service-client API-key
// verification to the shape api/http/notification_middleware.go's
// NotificationAuthenticator expects
// (Authenticate(ctx, apiKey) (subject string, err error)). It's defined here,
// not in api/http, so that internal/notification never has to import
// internal/auth to consume it - Go interfaces are satisfied structurally, so
// this type works as a NotificationAuthenticator without either package
// importing the other.
type ServiceClientAuthenticator struct {
	Service *AuthService
}

func (a ServiceClientAuthenticator) Authenticate(ctx context.Context, apiKey string) (string, error) {
	client, err := a.Service.AuthenticateServiceAPIKey(ctx, apiKey)
	if err != nil || client == nil {
		return "", err
	}
	return client.Name, nil
}
