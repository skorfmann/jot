package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skorfmann/jot/internal/manifest"
)

func TestListHumanOutputIncludesFullURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	createdAt := time.Date(2026, 5, 20, 6, 52, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_api/deploys" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dev" {
			t.Fatalf("authorization = %q, want Bearer dev", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deploys": []manifest.Manifest{{
				Slug:      "ayxv4nrp",
				CreatedAt: createdAt,
				CreatedBy: "thies@example.com",
				Title:     "Example",
			}},
		})
	}))
	defer server.Close()

	if err := saveCredentialFile(server.URL, Credential{Mode: "dev"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := NewRoot(&buf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--server", server.URL, "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, server.URL+"/ayxv4nrp/") {
		t.Fatalf("ls output missing full URL:\n%s", out)
	}
	if !strings.Contains(out, "thies@example.com") || !strings.Contains(out, "Example") {
		t.Fatalf("ls output missing deploy details:\n%s", out)
	}
}

func TestListJSONIncludesFullURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deploys": []manifest.Manifest{{
				Slug:      "bgcdzk76",
				CreatedAt: time.Date(2026, 5, 20, 6, 47, 0, 0, time.UTC),
				CreatedBy: "skorfmann@example.com",
			}},
		})
	}))
	defer server.Close()

	if err := saveCredentialFile(server.URL, Credential{Mode: "dev"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := NewRoot(&buf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--server", server.URL, "ls", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var got []struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("deploy count = %d, want 1", len(got))
	}
	if got[0].Slug != "bgcdzk76" || got[0].URL != server.URL+"/bgcdzk76/" {
		t.Fatalf("deploy = %#v", got[0])
	}
}

func TestListLocalHumanOutputIncludesFullURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".jot"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`[
  {
    "slug": "lcpbh7op",
    "url": "https://jot.example.com/lcpbh7op/",
    "title": "Jot Example HTML",
    "pushed_at": "2026-05-20T06:10:00Z",
    "pushed_by": "skorfmann@example.com"
  }
]`)
	if err := os.WriteFile(filepath.Join(dir, ".jot", "pushes.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	var buf bytes.Buffer
	cmd := NewRoot(&buf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"ls", "--local"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "https://jot.example.com/lcpbh7op/") {
		t.Fatalf("local ls output missing full URL:\n%s", out)
	}
	if !strings.Contains(out, "Jot Example HTML") {
		t.Fatalf("local ls output missing title:\n%s", out)
	}
}
