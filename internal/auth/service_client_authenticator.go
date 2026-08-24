package auth

import "context"

// ServiceClientAuthenticator adapts AuthService's service-client API-key
// verification to the shape notification.Authenticator expects
// (Authenticate(ctx, apiKey) (subject string, err error)). It's defined here,
// not in notification, so that internal/notification never has to import
// internal/auth to consume it - Go interfaces are satisfied structurally, so
// this type works as a notification.Authenticator without either package
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
