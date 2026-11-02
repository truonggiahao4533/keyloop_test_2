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

	Location *time.Location // the dealership's zone, which decides "today"

	DBMaxConns int32
	DBTimeout  time.Duration

	RateLimitRPS   float64 // per client IP; 0 disables
	RateLimitBurst int
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

		RateLimitRPS:   50,
		RateLimitBurst: 100,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 16 {
		return c, fmt.Errorf("JWT_SECRET is required (min 16 chars)")
	}
	var err error
	if c.Location, err = DealershipLocation(); err != nil {
		return c, err
	}
	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return c, fmt.Errorf("DB_MAX_CONNS must be a positive integer")
		}
		c.DBMaxConns = int32(n)
	}
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return c, fmt.Errorf("RATE_LIMIT_RPS must be a non-negative number")
		}
		c.RateLimitRPS = f
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("RATE_LIMIT_BURST must be a positive integer")
		}
		c.RateLimitBurst = n
	}
	return c, nil
}

// DealershipLocation reads DEALERSHIP_TIMEZONE, an IANA zone name that defaults
// to UTC. "Local" is rejected because it would tie "today" to the host's zone.
func DealershipLocation() (*time.Location, error) {
	name := getenv("DEALERSHIP_TIMEZONE", "UTC")
	loc, err := time.LoadLocation(name)
	if err != nil || name == "Local" {
		return nil, fmt.Errorf("DEALERSHIP_TIMEZONE must be an IANA time zone name, such as Asia/Ho_Chi_Minh")
	}
	return loc, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
