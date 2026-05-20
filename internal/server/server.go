package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/skorfmann/jot/internal/auth"
	"github.com/skorfmann/jot/internal/manifest"
	"github.com/skorfmann/jot/internal/storage"
)

type Server struct {
	cfg   Config
	store storage.Store
	auth  *auth.Authenticator
	mux   *http.ServeMux
	log   *slog.Logger
}

type checkRequest struct {
	Files []checkFile `json:"files"`
}

type checkFile struct {
	Path        string `json:"path,omitempty"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type checkResponse struct {
	Missing []storage.UploadRef `json:"missing"`
}

func New(cfg Config, store storage.Store, authenticator *auth.Authenticator, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{cfg: cfg, store: store, auth: authenticator, mux: http.NewServeMux(), log: logger}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.logRequests(s.mux)
}

func (s *Server) StartBackground(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := s.runGC(ctx); err != nil {
					s.log.Error("gc failed", "event_type", "error", "error", err)
				}
				timer.Reset(24 * time.Hour)
			}
		}
	}()
}

func (s *Server) routes() {
	s.mux.HandleFunc("/_health", s.handleHealth)
	s.mux.HandleFunc("/_api/version", s.handleVersion)
	s.mux.HandleFunc("/_api/auth/config", s.handleAuthConfig)
	s.mux.HandleFunc("/_auth/login", s.auth.LoginHandler(s.cfg.Server.InsecureHTTP))
	s.mux.HandleFunc("/_auth/callback", s.auth.CallbackHandler(s.cfg.Server.InsecureHTTP))
	s.mux.Handle("/_api/", s.auth.WithIdentity(http.HandlerFunc(s.handleAPI)))
	s.mux.HandleFunc("/", s.handleContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	if err := s.store.Health(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"server": Version, "manifest_schema": manifest.SchemaVersion, "min_cli": MinCLI})
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.auth.ConfigResponse())
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/_api/deploys:check":
		s.handleDeployCheck(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_api/deploys/"):
		s.handleDeployPut(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/_api/deploys":
		s.handleDeployList(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_api/deploys/"):
		s.handleDeployInspect(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_api/slugs/") && strings.HasSuffix(r.URL.Path, "/history"):
		s.handleHistory(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/_api/slugs/") && strings.HasSuffix(r.URL.Path, "/rollback"):
		s.handleRollback(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/_api/slugs/"):
		s.handleDeleteSlug(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/_api/whoami":
		id, _ := auth.IdentityFromContext(r.Context())
		writeJSON(w, http.StatusOK, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleDeployCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}
	var missing []storage.UploadRef
	for _, f := range req.Files {
		if len(f.SHA256) != 64 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid sha256: " + f.SHA256})
			return
		}
		exists, err := s.store.BlobExists(r.Context(), f.SHA256)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		if !exists {
			url, err := s.store.PresignPutBlob(r.Context(), f.SHA256, f.ContentType, 15*time.Minute)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
				return
			}
			missing = append(missing, storage.UploadRef{SHA256: f.SHA256, URL: url})
		}
	}
	writeJSON(w, http.StatusOK, checkResponse{Missing: missing})
}

func (s *Server) handleDeployPut(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/_api/deploys/")
	if _, err := ulid.ParseStrict(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deploy id must be a ULID"})
		return
	}
	var m manifest.Manifest
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}
	identity, _ := auth.IdentityFromContext(r.Context())
	m.SchemaVersion = manifest.SchemaVersion
	m.ID = id
	m.CreatedAt = time.Now().UTC()
	m.CreatedBy = identity.Email
	if m.Headers != nil && len(m.Headers) == 0 {
		m.Headers = nil
	}
	if err := s.validatePushManifest(r.Context(), &m); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	current, err := s.store.GetCurrent(r.Context(), m.Slug)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if err := s.store.PutManifest(r.Context(), &m); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var ifMatch *string
	if current.Found {
		ifMatch = &current.ETag
	}
	err = s.store.PutCurrent(r.Context(), m.Slug, manifest.CurrentPointer{ManifestID: m.ID}, ifMatch, !current.Found)
	if err != nil {
		if storage.IsPreconditionFailed(err) {
			conflict := s.describeCurrent(r.Context(), m.Slug)
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": "slug changed during push", "current": conflict})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if s.cfg.Server.HistorySize > 0 {
		_ = s.store.PruneHistory(context.WithoutCancel(r.Context()), m.Slug, s.cfg.Server.HistorySize)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"manifest": m, "url": s.deployURL(m.Slug)})
}

func (s *Server) validatePushManifest(ctx context.Context, m *manifest.Manifest) error {
	if err := manifest.Validate(m); err != nil {
		return err
	}
	if len(m.Files) > s.cfg.Limits.FilesPerPush {
		return fmt.Errorf("push contains %d files, limit is %d", len(m.Files), s.cfg.Limits.FilesPerPush)
	}
	var total int64
	for p, f := range m.Files {
		if f.Size > s.cfg.Limits.BytesPerFile {
			return fmt.Errorf("%s is %s, limit per file is %s", p, storage.FormatLimit(f.Size), storage.FormatLimit(s.cfg.Limits.BytesPerFile))
		}
		total += f.Size
		if total > s.cfg.Limits.BytesPerPush {
			return fmt.Errorf("push total is %s, limit is %s", storage.FormatLimit(total), storage.FormatLimit(s.cfg.Limits.BytesPerPush))
		}
		ok, err := s.store.BlobExists(ctx, f.SHA256)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("blob %s for %s is missing; run deploys:check and upload it first", f.SHA256, p)
		}
	}
	return nil
}

func (s *Server) handleDeployList(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAllManifests(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	id, _ := auth.IdentityFromContext(r.Context())
	q := strings.ToLower(r.URL.Query().Get("search"))
	tagFilters := r.URL.Query()["tag"]
	mine := r.URL.Query().Get("mine") == "1" || r.URL.Query().Get("mine") == "true"
	limit := intQuery(r, "limit", 50)
	filtered := make([]*manifest.Manifest, 0, len(items))
	for _, item := range items {
		if mine && item.CreatedBy != id.Email {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Summary), q) {
			continue
		}
		if len(tagFilters) > 0 && !hasAllTags(item.Tags, tagFilters) {
			continue
		}
		filtered = append(filtered, item)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deploys": filtered})
}

func (s *Server) handleDeployInspect(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimPrefix(r.URL.Path, "/_api/deploys/")
	m, err := s.findDeploy(r.Context(), ref)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "deploy not found"})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": m, "url": s.deployURL(m.Slug)})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_api/slugs/"), "/history")
	items, err := s.store.ListManifests(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deploys": items})
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_api/slugs/"), "/rollback")
	var req struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	history, err := s.store.ListManifests(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if len(history) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "slug not found"})
		return
	}
	targetID := req.ID
	if targetID == "" {
		if len(history) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no previous deploy available"})
			return
		}
		targetID = history[1].ID
	}
	var target *manifest.Manifest
	for _, item := range history {
		if item.ID == targetID {
			target = item
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "target deploy not found"})
		return
	}
	cur, err := s.store.GetCurrent(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if !cur.Found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "slug not found"})
		return
	}
	if err := s.store.PutCurrent(r.Context(), slug, manifest.CurrentPointer{ManifestID: target.ID}, &cur.ETag, false); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": target, "url": s.deployURL(slug)})
}

func (s *Server) handleDeleteSlug(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/_api/slugs/")
	if err := manifest.ValidateSlug(slug); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.store.DeleteSlug(r.Context(), slug); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/_") {
		http.NotFound(w, r)
		return
	}
	id, err := s.auth.Authenticate(r)
	if err != nil {
		if wantsHTML(r) {
			returnTo := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/_auth/login?return_to="+returnTo, http.StatusFound)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
		return
	}
	if r.URL.Path == "/" {
		s.handleRootIndex(w, r, id)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	if err := manifest.ValidateSlug(slug); err != nil {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 {
		http.Redirect(w, r, "/"+slug+"/", http.StatusMovedPermanently)
		return
	}
	rest := "/" + parts[1]
	ref, err := s.store.GetCurrent(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if !ref.Found {
		http.NotFound(w, r)
		return
	}
	m, err := s.store.GetManifest(r.Context(), slug, ref.Pointer.ManifestID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	resolved, ok := manifest.Resolve(m, rest, wantsHTML(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if resolved.StatusCode == http.StatusMovedPermanently {
		http.Redirect(w, r, "/"+slug+resolved.RedirectTo, http.StatusMovedPermanently)
		return
	}
	etag := `"` + resolved.File.SHA256 + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, _, err := s.store.GetBlob(r.Context(), resolved.File.SHA256)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer body.Close()
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", resolved.File.ContentType)
	w.Header().Set("Cache-Control", defaultCacheControl(resolved.File.ContentType))
	for k, v := range manifest.HeadersForPath(m, resolved.Path) {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resolved.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, body)
	}
}

func (s *Server) findDeploy(ctx context.Context, ref string) (*manifest.Manifest, error) {
	if _, err := ulid.ParseStrict(ref); err == nil {
		all, err := s.store.ListAllManifests(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range all {
			if item.ID == ref {
				return item, nil
			}
		}
		return nil, storage.ErrNotFound
	}
	if err := manifest.ValidateSlug(ref); err != nil {
		return nil, err
	}
	cur, err := s.store.GetCurrent(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !cur.Found {
		return nil, storage.ErrNotFound
	}
	return s.store.GetManifest(ctx, ref, cur.Pointer.ManifestID)
}

func (s *Server) describeCurrent(ctx context.Context, slug string) map[string]any {
	cur, err := s.store.GetCurrent(ctx, slug)
	if err != nil || !cur.Found {
		return nil
	}
	m, err := s.store.GetManifest(ctx, slug, cur.Pointer.ManifestID)
	if err != nil {
		return map[string]any{"manifest_id": cur.Pointer.ManifestID}
	}
	return map[string]any{"manifest_id": m.ID, "created_by": m.CreatedBy, "created_at": m.CreatedAt}
}

func (s *Server) deployURL(slug string) string {
	u := strings.TrimRight(s.cfg.Server.BaseURL, "/") + "/" + slug + "/"
	return u
}

func hasAllTags(tags, filters []string) bool {
	have := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		have[tag] = struct{}{}
	}
	for _, filter := range filters {
		if _, ok := have[filter]; !ok {
			return false
		}
	}
	return true
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") || r.Header.Get("Accept") == ""
}

func defaultCacheControl(contentType string) string {
	if strings.HasPrefix(contentType, "text/html") {
		return "public, max-age=0, must-revalidate"
	}
	return "public, max-age=3600"
}

func intQuery(r *http.Request, key string, fallback int) int {
	if v := r.URL.Query().Get(key); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		eventType := "access"
		if strings.HasPrefix(r.URL.Path, "/_api/") {
			eventType = "api"
		}
		if rw.status >= 400 {
			eventType = "error"
		}
		s.log.Info("request", "event_type", eventType, "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func sortManifests(items []*manifest.Manifest) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}
