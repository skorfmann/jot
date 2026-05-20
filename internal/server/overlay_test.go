package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skorfmann/jot/internal/auth"
	"github.com/skorfmann/jot/internal/manifest"
	"github.com/skorfmann/jot/internal/storage"
)

func TestOverlayInjectsEligibleHTMLResponse(t *testing.T) {
	html := []byte(`<!doctype html><html><head><title>Report</title></head><body><main>Hello</main></body></html>`)
	js := []byte(`console.log("ok");`)
	m := overlayTestManifest(html, js)
	s := newOverlayTestServer(t, &overlayTestStore{
		currentID: m.ID,
		manifest:  m,
		blobs: map[string][]byte{
			m.Files["/index.html"].SHA256: html,
			m.Files["/app.js"].SHA256:     js,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/report/", nil)
	req.Header.Set("Authorization", "Bearer dev")
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\n%s", res.Code, http.StatusOK, body)
	}
	for _, want := range []string{
		`<div id="jot-overlay-root"></div>`,
		`id="jot-overlay-bootstrap"`,
		`"slug":"report"`,
		`"deployId":"01HX0000000000000000000001"`,
		`"title":"Quarterly report"`,
		`"apiBase":"/_api"`,
		`data-jot-overlay-style`,
		`href="/_jot/assets/`,
		`src="/_jot/assets/`,
		`<main>Hello</main>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("injected HTML missing %q:\n%s", want, body)
		}
	}
	if got := res.Header().Get("ETag"); got == `"`+m.Files["/index.html"].SHA256+`"` {
		t.Fatalf("ETag used original blob hash after injection: %s", got)
	}
}

func TestOverlayLeavesNonHTMLUnchanged(t *testing.T) {
	html := []byte(`<html><body>Hello</body></html>`)
	js := []byte(`console.log("ok");`)
	m := overlayTestManifest(html, js)
	s := newOverlayTestServer(t, &overlayTestStore{
		currentID: m.ID,
		manifest:  m,
		blobs: map[string][]byte{
			m.Files["/index.html"].SHA256: html,
			m.Files["/app.js"].SHA256:     js,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/report/app.js", nil)
	req.Header.Set("Authorization", "Bearer dev")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Body.String(); got != string(js) {
		t.Fatalf("body changed:\n%s", got)
	}
	if strings.Contains(res.Body.String(), "jot-overlay-root") {
		t.Fatalf("non-HTML response was injected:\n%s", res.Body.String())
	}
}

func TestOverlayServesImmutableDeployIDContent(t *testing.T) {
	oldHTML := []byte(`<!doctype html><html><body>Old deploy</body></html>`)
	currentHTML := []byte(`<!doctype html><html><body>Current deploy</body></html>`)
	js := []byte(`console.log("ok");`)
	oldDeploy := overlayTestManifest(oldHTML, js)
	oldDeploy.ID = "01HX0000000000000000000000"
	currentDeploy := overlayTestManifest(currentHTML, js)
	currentDeploy.ID = "01HX0000000000000000000001"
	s := newOverlayTestServer(t, &overlayTestStore{
		currentID: currentDeploy.ID,
		manifests: []*manifest.Manifest{
			currentDeploy,
			oldDeploy,
		},
		blobs: map[string][]byte{
			oldDeploy.Files["/index.html"].SHA256:     oldHTML,
			currentDeploy.Files["/index.html"].SHA256: currentHTML,
			currentDeploy.Files["/app.js"].SHA256:     js,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/01HX0000000000000000000000/", nil)
	req.Header.Set("Authorization", "Bearer dev")
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\n%s", res.Code, http.StatusOK, body)
	}
	if !strings.Contains(body, "Old deploy") || strings.Contains(body, "Current deploy") {
		t.Fatalf("immutable deploy route served wrong body:\n%s", body)
	}
	if !strings.Contains(body, `"deployId":"01HX0000000000000000000000"`) {
		t.Fatalf("bootstrap missing historical deploy id:\n%s", body)
	}
}

func TestOverlayDoesNotInjectHeadHTMLResponse(t *testing.T) {
	html := []byte(`<!doctype html><html><body>Hello</body></html>`)
	js := []byte(`console.log("ok");`)
	m := overlayTestManifest(html, js)
	s := newOverlayTestServer(t, &overlayTestStore{
		currentID: m.ID,
		manifest:  m,
		blobs: map[string][]byte{
			m.Files["/index.html"].SHA256: html,
			m.Files["/app.js"].SHA256:     js,
		},
	})
	req := httptest.NewRequest(http.MethodHead, "/report/", nil)
	req.Header.Set("Authorization", "Bearer dev")
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if res.Body.Len() != 0 {
		t.Fatalf("HEAD response wrote a body:\n%s", res.Body.String())
	}
	if got := res.Header().Get("ETag"); got != `"`+m.Files["/index.html"].SHA256+`"` {
		t.Fatalf("ETag = %q, want original blob hash", got)
	}
}

func TestOverlayDoesNotInjectSystemEndpoints(t *testing.T) {
	s := newOverlayTestServer(t, &overlayTestStore{})
	for _, target := range []string{"/_health", "/_api/version", "/_auth/login"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		res := httptest.NewRecorder()

		s.Handler().ServeHTTP(res, req)

		if res.Code < 200 || res.Code >= 400 {
			t.Fatalf("%s status = %d, want 2xx/3xx", target, res.Code)
		}
		if strings.Contains(res.Body.String(), "jot-overlay-root") {
			t.Fatalf("%s system response was injected:\n%s", target, res.Body.String())
		}
	}
}

func TestOverlayServesImmutableHashedAssets(t *testing.T) {
	s := newOverlayTestServer(t, &overlayTestStore{})
	bundle, err := loadOverlayBundle()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, bundle.Script, nil)
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if res.Body.Len() == 0 {
		t.Fatal("asset body is empty")
	}
}

func TestOverlayInjectsRootIndexWithNullDeployContext(t *testing.T) {
	s := newOverlayTestServer(t, &overlayTestStore{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer dev")
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\n%s", res.Code, http.StatusOK, body)
	}
	for _, want := range []string{`id="jot-overlay-bootstrap"`, `"slug":null`, `"deployId":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("root overlay missing %q:\n%s", want, body)
		}
	}
}

func newOverlayTestServer(t *testing.T, store storage.Store) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Server.BaseURL = "https://jot.example.com"
	cfg.Server.InsecureHTTP = true
	cfg.Auth.Mode = "dev"
	cfg.Auth.CookieSecret = "0000000000000000000000000000000000000000000000000000000000000000"
	authenticator, err := auth.New(context.Background(), cfg.Auth, cfg.Server.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, store, authenticator, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func overlayTestManifest(html, js []byte) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            "01HX0000000000000000000001",
		Slug:          "report",
		CreatedAt:     time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
		CreatedBy:     "dev@local",
		Title:         "Quarterly report",
		Tags:          []string{"finance"},
		Files: map[string]manifest.File{
			"/index.html": {
				SHA256:      manifest.Hash(html),
				Size:        int64(len(html)),
				ContentType: "text/html; charset=utf-8",
			},
			"/app.js": {
				SHA256:      manifest.Hash(js),
				Size:        int64(len(js)),
				ContentType: "application/javascript",
			},
		},
	}
}

type overlayTestStore struct {
	currentID string
	manifest  *manifest.Manifest
	manifests []*manifest.Manifest
	blobs     map[string][]byte
}

func (s *overlayTestStore) Health(context.Context) error { return nil }

func (s *overlayTestStore) BlobExists(context.Context, string) (bool, error) { return true, nil }

func (s *overlayTestStore) PresignPutBlob(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

func (s *overlayTestStore) GetBlob(_ context.Context, hash string) (io.ReadCloser, storage.BlobMeta, error) {
	body, ok := s.blobs[hash]
	if !ok {
		return nil, storage.BlobMeta{}, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), storage.BlobMeta{SHA256: hash, Size: int64(len(body))}, nil
}

func (s *overlayTestStore) PutManifest(context.Context, *manifest.Manifest) error { return nil }

func (s *overlayTestStore) GetManifest(_ context.Context, slug string, id string) (*manifest.Manifest, error) {
	for _, item := range s.allManifests() {
		if item.Slug == slug && item.ID == id {
			return item, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (s *overlayTestStore) ListManifests(_ context.Context, slug string) ([]*manifest.Manifest, error) {
	items := make([]*manifest.Manifest, 0, len(s.allManifests()))
	for _, item := range s.allManifests() {
		if item.Slug == slug {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *overlayTestStore) ListAllManifests(context.Context) ([]*manifest.Manifest, error) {
	return s.allManifests(), nil
}

func (s *overlayTestStore) GetCurrent(context.Context, string) (storage.CurrentRef, error) {
	if s.currentID == "" {
		return storage.CurrentRef{}, nil
	}
	return storage.CurrentRef{
		Pointer: manifest.CurrentPointer{ManifestID: s.currentID},
		ETag:    s.currentID,
		Found:   true,
	}, nil
}

func (s *overlayTestStore) PutCurrent(context.Context, string, manifest.CurrentPointer, *string, bool) error {
	return nil
}

func (s *overlayTestStore) DeleteSlug(context.Context, string) error { return nil }

func (s *overlayTestStore) PruneHistory(context.Context, string, int) error { return nil }

func (s *overlayTestStore) ListBlobHashes(context.Context) (map[string]struct{}, error) {
	return nil, nil
}

func (s *overlayTestStore) MoveBlobToTrash(context.Context, string) error { return nil }

func (s *overlayTestStore) DeleteExpiredTrash(context.Context, time.Duration) error { return nil }

func (s *overlayTestStore) allManifests() []*manifest.Manifest {
	if len(s.manifests) > 0 {
		return s.manifests
	}
	if s.manifest != nil {
		return []*manifest.Manifest{s.manifest}
	}
	return nil
}
