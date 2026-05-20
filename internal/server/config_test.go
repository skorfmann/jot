package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigParsesDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jot.yaml")
	err := os.WriteFile(path, []byte(`
server:
  base_url: http://localhost:8080
storage:
  bucket: jot
auth:
  mode: dev
  cookie_secret: 0000000000000000000000000000000000000000000000000000000000000000
  session_ttl: 1h
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.SessionTTL != time.Hour {
		t.Fatalf("session_ttl = %s, want 1h", cfg.Auth.SessionTTL)
	}
}

func TestLoadConfigSupportsCLIClientIDEnv(t *testing.T) {
	t.Setenv("JOT_AUTH_CLI_CLIENT_ID", "cli-client")
	t.Setenv("JOT_AUTH_CLI_CLIENT_SECRET", "cli-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "jot.yaml")
	err := os.WriteFile(path, []byte(`
server:
  base_url: http://localhost:8080
storage:
  bucket: jot
auth:
  mode: dev
  cookie_secret: 0000000000000000000000000000000000000000000000000000000000000000
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.CLIClientID != "cli-client" {
		t.Fatalf("cli_client_id = %q, want cli-client", cfg.Auth.CLIClientID)
	}
	if cfg.Auth.CLIClientSecret != "cli-secret" {
		t.Fatalf("cli_client_secret = %q, want cli-secret", cfg.Auth.CLIClientSecret)
	}
}
