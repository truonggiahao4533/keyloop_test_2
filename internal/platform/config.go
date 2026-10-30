// Package platform holds cross-cutting infrastructure: configuration,
// logging, metrics, tracing and authentication.
package platform

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the service configuration, read from the environment.
type Config struct {
	Port         string
	DatabaseURL  string
	JWTSecret    string
	OTLPEndpoint string // empty disables trace export

	DBMaxConns int32
	DBTimeout  time.Duration
}

// LoadConfig reads the configuration from environment variables.
func LoadConfig() (Config, error) {
	c := Config{
		Port:         getenv("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		OTLPEndpoint: os.Getenv("OTLP_ENDPOINT"),
		DBMaxConns:   20,
		DBTimeout:    3 * time.Second,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 16 {
		return c, fmt.Errorf("JWT_SECRET is required (min 16 chars)")
	}
	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("DB_MAX_CONNS must be a positive integer")
		}
		c.DBMaxConns = int32(n)
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
