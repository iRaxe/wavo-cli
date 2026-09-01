package swaggercontract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFile(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "valid application contract",
			body: `{
  "openapi": "3.0.3",
  "info": {"title": "Wavo fixture", "version": "1"},
  "paths": {
    "/api/v1/profile": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v1/accounts/{account_id}/conversations": {
      "parameters": [{"in":"path","name":"account_id","required":true,"schema":{"type":"integer"}}],
      "get": {"responses": {"200": {"description": "ok"}}}
    },
    "/api/v1/accounts/{account_id}/conversations/filter": {
      "parameters": [{"in":"path","name":"account_id","required":true,"schema":{"type":"integer"}}],
      "post": {"responses": {"200": {"description": "ok"}}}
    },
    "/api/v1/accounts/{account_id}/conversations/{conversation_id}/messages": {
      "parameters": [
        {"in":"path","name":"account_id","required":true,"schema":{"type":"integer"}},
        {"in":"path","name":"conversation_id","required":true,"schema":{"type":"integer"}}
      ],
      "get": {"responses": {"200": {"description": "ok"}}}
    }
  }
}`,
		},
		{name: "invalid JSON", body: `{not-json`, wantErr: true},
		{
			name:    "empty paths",
			body:    `{"openapi":"3.0.3","info":{"title":"empty","version":"1"},"paths":{}}`,
			wantErr: true,
		},
		{
			name:    "missing application paths",
			body:    `{"openapi":"3.0.3","info":{"title":"wrong","version":"1"},"paths":{"/health":{"get":{"responses":{"200":{"description":"ok"}}}}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application_swagger.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := ValidateFile(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFileAcceptsBundledContract(t *testing.T) {
	path := filepath.Join("..", "sdk", "testdata", "application_swagger.json")
	if err := ValidateFile(path); err != nil {
		t.Fatalf("ValidateFile(bundled contract) error = %v", err)
	}
}
