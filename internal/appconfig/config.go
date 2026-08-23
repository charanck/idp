// Package appconfig loads runtime configuration from environment variables,
// using the same variable names as the Django settings (control_plane_project/settings.py)
// so existing deployment configs (.env, docker-compose) don't need to change.
package appconfig

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Debug bool

	MasterEncryptionKey string

	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBSSLMode  string

	RedisURL            string
	CacheKeyPrefix      string
	CacheTimeoutSeconds int

	AuthRateLimit              int
	AuthRateLimitWindowSeconds int
	S2SAuthRateLimit           int

	AllowedHosts       []string
	CSRFTrustedOrigins []string

	AdminEmail    string
	AdminPassword string

	SessionSecret string

	Port string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads configuration from the environment. It intentionally does not
// generate a random dev-only MASTER_ENCRYPTION_KEY the way the Python
// settings.py does - that behavior is dev-convenience, and a Go binary
// running in production must always be given real secrets explicitly.
func Load() (*Config, error) {
	// .env is a local-dev convenience only; production supplies env vars
	// directly, so a missing file here is expected and not an error.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("Error loading .env file: %v", err)
	}
	cfg := &Config{
		Debug: getenvBool("DEBUG", false),

		MasterEncryptionKey: os.Getenv("MASTER_ENCRYPTION_KEY"),

		DBHost:     getenv("DB_HOST", "localhost"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBName:     getenv("DB_NAME", "idp"),
		DBUser:     getenv("DB_USER", "idp"),
		DBPassword: getenv("DB_PASSWORD", "idp"),
		DBSSLMode:  getenv("DB_SSLMODE", "disable"),

		RedisURL:            getenv("REDIS_URL", "redis://127.0.0.1:6379/1"),
		CacheKeyPrefix:      getenv("CACHE_KEY_PREFIX", "control_plane"),
		CacheTimeoutSeconds: getenvInt("CACHE_TIMEOUT", 300),

		AuthRateLimit:              getenvInt("AUTH_RATE_LIMIT", 10),
		AuthRateLimitWindowSeconds: getenvInt("AUTH_RATE_LIMIT_WINDOW_SECONDS", 60),
		S2SAuthRateLimit:           getenvInt("S2S_AUTH_RATE_LIMIT", 20),

		AllowedHosts:       splitCSV(getenv("ALLOWED_HOSTS", "localhost,127.0.0.1")),
		CSRFTrustedOrigins: splitCSV(os.Getenv("CSRF_TRUSTED_ORIGINS")),

		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),

		SessionSecret: getenv("SESSION_SECRET", getenv("DJANGO_SECRET_KEY", "dev-insecure-session-secret")),

		Port: getenv("PORT", "8000"),
	}

	if cfg.MasterEncryptionKey == "" {
		return nil, fmt.Errorf("MASTER_ENCRYPTION_KEY is required")
	}

	return cfg, nil
}

// DSN builds a Postgres connection string for GORM/pgx.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBPassword, c.DBSSLMode,
	)
}
