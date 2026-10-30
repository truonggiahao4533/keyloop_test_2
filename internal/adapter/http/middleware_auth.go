package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

// authMiddleware verifies the bearer token and stores the Principal in the
// request context.
func authMiddleware(a *platform.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			c.Header("WWW-Authenticate", "Bearer")
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
			return
		}
		p, err := a.Parse(token)
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired token")
			return
		}
		c.Request = c.Request.WithContext(platform.WithPrincipal(c.Request.Context(), p))
		// Also keep it on the gin context: outer middleware (otelgin) restores
		// c.Request on the way out, so the logger cannot rely on the request.
		c.Set(ctxPrincipal, p)
		c.Next()
	}
}

// require allows the request only if the caller's role grants perm.
func require(perm platform.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := platform.PrincipalFrom(c.Request.Context())
		if !ok || !p.Role.Can(perm) {
			writeError(c, http.StatusForbidden, "FORBIDDEN", "role is not allowed to perform this operation")
			return
		}
		c.Next()
	}
}

const ctxPrincipal = "principal"

func principal(c *gin.Context) platform.Principal {
	p, _ := c.Get(ctxPrincipal)
	pr, _ := p.(platform.Principal)
	return pr
}
