// Package openapi embeds the API's OpenAPI specification, the contract between this API and its clients.
package openapi

import _ "embed"

// Spec is the OpenAPI specification in YAML.
//
//go:embed api.yaml
var Spec []byte
