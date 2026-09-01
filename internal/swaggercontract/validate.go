package swaggercontract

import (
	"context"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

var expectedApplicationPaths = []string{
	"/api/v1/profile",
	"/api/v1/accounts/{account_id}/conversations",
	"/api/v1/accounts/{account_id}/conversations/filter",
	"/api/v1/accounts/{account_id}/conversations/{conversation_id}/messages",
}

// ValidateFile loads and validates an OpenAPI document, then verifies that it
// is the application contract expected by the Wavo CLI.
func ValidateFile(path string) error {
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromFile(path)
	if err != nil {
		return fmt.Errorf("load OpenAPI document: %w", err)
	}
	if document == nil || document.Paths == nil || document.Paths.Len() == 0 {
		return fmt.Errorf("OpenAPI document has no paths")
	}
	if err := document.Validate(context.Background(), openapi3.DisableExamplesValidation()); err != nil {
		return fmt.Errorf("validate OpenAPI document: %w", err)
	}
	for _, expectedPath := range expectedApplicationPaths {
		if document.Paths.Value(expectedPath) == nil {
			return fmt.Errorf("OpenAPI document is missing required application path %q", expectedPath)
		}
	}
	return nil
}
