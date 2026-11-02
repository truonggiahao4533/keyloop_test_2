package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/api"
)

// docsPage is Swagger UI served from the API's own origin, so "Try it out"
// needs no CORS. Click "Authorize" and paste a token from `api token`.
const docsPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Inventory API Docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#swagger-ui", persistAuthorization: true });
  </script>
</body>
</html>`

func serveSpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml", api.Spec)
}

func serveDocs(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docsPage))
}
