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

func TestLoadConfigSupportsGoCDKStorageURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jot.yaml")
	err := os.WriteFile(path, []byte(`
server:
  base_url: http://localhost:8080
storage:
  url: gs://jot-test?access_id=jot-server@example.iam.gserviceaccount.com
  google_access_id: jot-server@example.iam.gserviceaccount.com
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
	if cfg.Storage.URL != "gs://jot-test?access_id=jot-server@example.iam.gserviceaccount.com" {
		t.Fatalf("storage.url = %q", cfg.Storage.URL)
	}
	if cfg.Storage.GoogleAccessID != "jot-server@example.iam.gserviceaccount.com" {
		t.Fatalf("storage.google_access_id = %q", cfg.Storage.GoogleAccessID)
	}
}
