package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"

	model "controlplane/internal/model/auth"
)

// ErrWeakPassword is returned by ValidatePasswordAgainstPolicy when a
// password fails one or more of the policy's complexity requirements.
var ErrWeakPassword = errors.New("password does not meet policy requirements")

// policyCacheKey is a fixed key (no filter/query dimensions) since Policy is
// a singleton row.
const policyCacheKey = "authcache:policy"

// GetPolicy returns the singleton policy settings row. Read-through cached,
// invalidated by UpdatePolicy. Called on every authenticated request via
// web.AuthMiddleware, so caching it has outsized impact.
func (s *AuthService) GetPolicy(ctx context.Context) (*model.Policy, error) {
	version, err := s.cache.GetVersion(ctx, policyCacheVersionKey)
	if err != nil {
		return s.policies.Get(ctx)
	}
	cacheKey := fmt.Sprintf("%s:v%d", policyCacheKey, version)
	if cached, found := cacheGet[*model.Policy](ctx, s.cache, cacheKey); found {
		return cached, nil
	}

	policy, err := s.policies.Get(ctx)
	if err != nil {
		return nil, err
	}
	cacheSet(ctx, s.cache, cacheKey, s.cacheTimeout, policy)
	return policy, nil
}

// UpdatePolicyInput bundles every editable Policy field, grouped the same
// way the Policies admin page groups them into cards.
type UpdatePolicyInput struct {
	SelfRegistrationAllowedDomains string

	PasswordMinLength     int
	PasswordRequireUpper  bool
	PasswordRequireLower  bool
	PasswordRequireDigit  bool
	PasswordRequireSymbol bool
	PasswordMaxAgeDays    int

	MaxFailedLoginAttempts int
	LockoutDurationMinutes int

	SessionIdleTimeoutMinutes int

	SSOOnly           bool
	LoginIPAllowlist  string
}

// UpdatePolicy replaces the singleton policy settings row.
func (s *AuthService) UpdatePolicy(ctx context.Context, in UpdatePolicyInput) (*model.Policy, error) {
	policy, err := s.policies.Get(ctx)
	if err != nil {
		return nil, err
	}
	policy.SelfRegistrationAllowedDomains = in.SelfRegistrationAllowedDomains
	policy.PasswordMinLength = in.PasswordMinLength
	policy.PasswordRequireUpper = in.PasswordRequireUpper
	policy.PasswordRequireLower = in.PasswordRequireLower
	policy.PasswordRequireDigit = in.PasswordRequireDigit
	policy.PasswordRequireSymbol = in.PasswordRequireSymbol
	policy.PasswordMaxAgeDays = in.PasswordMaxAgeDays
	policy.MaxFailedLoginAttempts = in.MaxFailedLoginAttempts
	policy.LockoutDurationMinutes = in.LockoutDurationMinutes
	policy.SessionIdleTimeoutMinutes = in.SessionIdleTimeoutMinutes
	policy.SSOOnly = in.SSOOnly
	policy.LoginIPAllowlist = in.LoginIPAllowlist
	if err := s.policies.Update(ctx, policy); err != nil {
		return nil, err
	}
	s.invalidatePolicyCache(ctx)
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
	for allowed := range strings.SplitSeq(allowedDomains, ",") {
		if strings.ToLower(strings.TrimSpace(allowed)) == domain {
			return true
		}
	}
	return false
}

// ValidatePasswordAgainstPolicy checks a candidate password against the
// policy's minimum length and character-class requirements, returning
// ErrWeakPassword joined with one message per failed rule.
func ValidatePasswordAgainstPolicy(policy *model.Policy, password string) error {
	var problems []string
	if policy.PasswordMinLength > 0 && len(password) < policy.PasswordMinLength {
		problems = append(problems, "be at least "+strconv.Itoa(policy.PasswordMinLength)+" characters")
	}
	if policy.PasswordRequireUpper && !containsFunc(password, unicode.IsUpper) {
		problems = append(problems, "include an uppercase letter")
	}
	if policy.PasswordRequireLower && !containsFunc(password, unicode.IsLower) {
		problems = append(problems, "include a lowercase letter")
	}
	if policy.PasswordRequireDigit && !containsFunc(password, unicode.IsDigit) {
		problems = append(problems, "include a digit")
	}
	if policy.PasswordRequireSymbol && !containsFunc(password, isSymbol) {
		problems = append(problems, "include a symbol")
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.Join(ErrWeakPassword, errors.New("password must "+strings.Join(problems, ", ")))
}

// IsPasswordExpired reports whether a password set at passwordChangedAt has
// exceeded the policy's max age (0 = passwords never expire). A nil
// passwordChangedAt - a user who predates this feature or was created by an
// admin - is treated as not expired.
func IsPasswordExpired(policy *model.Policy, passwordChangedAt *time.Time, now time.Time) bool {
	if policy.PasswordMaxAgeDays <= 0 || passwordChangedAt == nil {
		return false
	}
	return now.After(passwordChangedAt.Add(time.Duration(policy.PasswordMaxAgeDays) * 24 * time.Hour))
}

func containsFunc(s string, f func(rune) bool) bool {
	for _, r := range s {
		if f(r) {
			return true
		}
	}
	return false
}

func isSymbol(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// IPAllowed checks clientIP against the policy's comma-separated login IP
// allow-list (plain IPs or CIDRs, empty = unrestricted). An unparseable
// clientIP or allow-list entry is skipped rather than treated as a match.
func IPAllowed(allowlist, clientIP string) bool {
	allowlist = strings.TrimSpace(allowlist)
	if allowlist == "" {
		return true
	}
	ip, err := netip.ParseAddr(clientIP)
	if err != nil {
		return false
	}
	for entry := range strings.SplitSeq(allowlist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil && addr == ip {
			return true
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}
