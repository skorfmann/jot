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

func TestRootRedirectsUnauthenticatedHTMLRequests(t *testing.T) {
	s := newRootTestServer(t, &rootTestStore{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusFound)
	}
	if got := res.Header().Get("Location"); got != "/_auth/login?return_to=%2F" {
		t.Fatalf("location = %q", got)
	}
}

func TestRootRendersCurrentDeployList(t *testing.T) {
	old := rootTestManifest("01HX0000000000000000000000", "report", "Old Report", "alice@example.com", time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), 100)
	current := rootTestManifest("01HX0000000000000000000001", "report", "Current Report", "alice@example.com", time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), 200)
	other := rootTestManifest("01HX0000000000000000000002", "deck", "Board Deck", "dev@local", time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC), 300)
	store := &rootTestStore{
		all: []*manifest.Manifest{old, current, other},
		current: map[string]string{
			"report": current.ID,
			"deck":   other.ID,
		},
		manifests: map[string]*manifest.Manifest{
			"report/" + old.ID:     old,
			"report/" + current.ID: current,
			"deck/" + other.ID:     other,
		},
	}
	s := newRootTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer dev")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\n%s", res.Code, http.StatusOK, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	body := res.Body.String()
	for _, want := range []string{
		"Jot Deploys",
		"Current Report",
		"Board Deck",
		"https://jot.example.com/report/",
		"https://jot.example.com/deck/",
		"alice@example.com",
		"dev@local",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("root HTML missing %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"Old Report", "blobs/sha256", current.Files["/index.html"].SHA256} {
		if strings.Contains(body, bad) {
			t.Fatalf("root HTML contains %q:\n%s", bad, body)
		}
	}
}

func TestRootMineAndSearchFilters(t *testing.T) {
	mine := rootTestManifest("01HX0000000000000000000003", "mine", "Revenue Table", "dev@local", time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), 100)
	other := rootTestManifest("01HX0000000000000000000004", "other", "Revenue Draft", "other@example.com", time.Date(2026, 5, 20, 13, 0, 0, 0, time.UTC), 100)
	store := &rootTestStore{
		all: []*manifest.Manifest{mine, other},
		current: map[string]string{
			"mine":  mine.ID,
			"other": other.ID,
		},
		manifests: map[string]*manifest.Manifest{
			"mine/" + mine.ID:   mine,
			"other/" + other.ID: other,
		},
	}
	s := newRootTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/?q=revenue&mine=1", nil)
	req.Header.Set("Authorization", "Bearer dev")
	res := httptest.NewRecorder()

	s.Handler().ServeHTTP(res, req)

	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\n%s", res.Code, http.StatusOK, body)
	}
	if !strings.Contains(body, "Revenue Table") {
		t.Fatalf("mine deploy missing:\n%s", body)
	}
	if strings.Contains(body, "Revenue Draft") {
		t.Fatalf("non-mine deploy rendered:\n%s", body)
	}
	if !strings.Contains(body, `name="mine" value="1" checked`) {
		t.Fatalf("mine checkbox should stay checked:\n%s", body)
	}
}

func newRootTestServer(t *testing.T, store storage.Store) *Server {
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

func rootTestManifest(id, slug, title, createdBy string, createdAt time.Time, size int64) *manifest.Manifest {
	hash := strings.Repeat("a", 64)
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            id,
		Slug:          slug,
		CreatedAt:     createdAt,
		CreatedBy:     createdBy,
		Title:         title,
		Summary:       "A private published page",
		Tags:          []string{"report"},
		Files: map[string]manifest.File{
			"/index.html": {
				SHA256:      hash,
				Size:        size,
				ContentType: "text/html; charset=utf-8",
			},
		},
	}
}

type rootTestStore struct {
	all       []*manifest.Manifest
	current   map[string]string
	manifests map[string]*manifest.Manifest
}

func (s *rootTestStore) Health(context.Context) error { return nil }

func (s *rootTestStore) BlobExists(context.Context, string) (bool, error) { return true, nil }

func (s *rootTestStore) PresignPutBlob(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

func (s *rootTestStore) GetBlob(context.Context, string) (io.ReadCloser, storage.BlobMeta, error) {
	return io.NopCloser(bytes.NewReader(nil)), storage.BlobMeta{}, nil
}

func (s *rootTestStore) PutManifest(context.Context, *manifest.Manifest) error { return nil }

func (s *rootTestStore) GetManifest(_ context.Context, slug string, id string) (*manifest.Manifest, error) {
	m, ok := s.manifests[slug+"/"+id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return m, nil
}

func (s *rootTestStore) ListManifests(context.Context, string) ([]*manifest.Manifest, error) {
	return nil, nil
}

func (s *rootTestStore) ListAllManifests(context.Context) ([]*manifest.Manifest, error) {
	return s.all, nil
}

func (s *rootTestStore) GetCurrent(_ context.Context, slug string) (storage.CurrentRef, error) {
	id, ok := s.current[slug]
	if !ok {
		return storage.CurrentRef{}, nil
	}
	return storage.CurrentRef{
		Pointer: manifest.CurrentPointer{ManifestID: id},
		ETag:    id,
		Found:   true,
	}, nil
}

func (s *rootTestStore) PutCurrent(context.Context, string, manifest.CurrentPointer, *string, bool) error {
	return nil
}

func (s *rootTestStore) DeleteSlug(context.Context, string) error { return nil }

func (s *rootTestStore) PruneHistory(context.Context, string, int) error { return nil }

func (s *rootTestStore) ListBlobHashes(context.Context) (map[string]struct{}, error) {
	return nil, nil
}

func (s *rootTestStore) MoveBlobToTrash(context.Context, string) error { return nil }

func (s *rootTestStore) DeleteExpiredTrash(context.Context, time.Duration) error { return nil }
