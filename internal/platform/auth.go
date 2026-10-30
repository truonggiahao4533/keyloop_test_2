package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role is a caller's role, carried in the JWT "role" claim.
type Role string

const (
	RoleViewer  Role = "viewer"
	RoleManager Role = "manager"
	RoleIngest  Role = "ingest"
)

// Permission is an operation a role may perform.
type Permission string

const (
	PermRead         Permission = "read"
	PermLogAction    Permission = "log_action"
	PermSetThreshold Permission = "set_threshold"
	PermIngest       Permission = "ingest"
)

var rolePerms = map[Role]map[Permission]bool{
	RoleViewer:  {PermRead: true},
	RoleManager: {PermRead: true, PermLogAction: true, PermSetThreshold: true},
	RoleIngest:  {PermIngest: true},
}

// ParseRole validates s.
func ParseRole(s string) (Role, error) {
	if _, ok := rolePerms[Role(s)]; !ok {
		return "", fmt.Errorf("unknown role %q", s)
	}
	return Role(s), nil
}

// Can reports whether the role grants p.
func (r Role) Can(p Permission) bool { return rolePerms[r][p] }

// Principal is the authenticated caller.
type Principal struct {
	Subject string
	Role    Role
}

type principalKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal stored in ctx, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// ErrInvalidToken is returned for any token that fails verification.
var ErrInvalidToken = errors.New("invalid token")

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Authenticator verifies and mints HS256 JWTs with claims {sub, role}.
type Authenticator struct {
	secret []byte
}

// NewAuthenticator returns an Authenticator using the shared secret.
func NewAuthenticator(secret string) *Authenticator {
	return &Authenticator{secret: []byte(secret)}
}

// Parse verifies the token and returns its principal.
func (a *Authenticator) Parse(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	role, err := ParseRole(c.Role)
	if err != nil || c.Subject == "" {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Subject: c.Subject, Role: role}, nil
}

// Mint issues a token for sub with the given role, valid for ttl.
func (a *Authenticator) Mint(sub string, role Role, ttl time.Duration) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}).SignedString(a.secret)
}
