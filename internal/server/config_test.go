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
