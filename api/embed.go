// Package api embeds the OpenAPI contract so the binary can serve it.
package api

import _ "embed"

// Spec is api/openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
