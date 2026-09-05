package auth

import (
	"context"
	"strings"

	model "controlplane/internal/model/auth"
)

// GetPolicy returns the singleton policy settings row.
func (s *AuthService) GetPolicy(ctx context.Context) (*model.Policy, error) {
	return s.policies.Get(ctx)
}

// UpdatePolicy updates the singleton policy settings row.
func (s *AuthService) UpdatePolicy(ctx context.Context, selfRegistrationAllowedDomains string) (*model.Policy, error) {
	policy, err := s.policies.Get(ctx)
	if err != nil {
		return nil, err
	}
	policy.SelfRegistrationAllowedDomains = selfRegistrationAllowedDomains
	if err := s.policies.Update(ctx, policy); err != nil {
		return nil, err
	}
	return policy, nil
}

// domainAllowed checks email's domain against the policy's comma-separated
// self-registration allow-list (empty list = unrestricted).
func domainAllowed(allowedDomains, email string) bool {
	allowedDomains = strings.TrimSpace(allowedDomains)
	if allowedDomains == "" {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	domain = strings.ToLower(domain)
	for _, allowed := range strings.Split(allowedDomains, ",") {
		if strings.ToLower(strings.TrimSpace(allowed)) == domain {
			return true
		}
	}
	return false
}
