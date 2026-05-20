package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/skorfmann/jot/internal/manifest"
	overlayassets "github.com/skorfmann/jot/web/overlay"
)

type overlayBootstrap struct {
	Slug      *string  `json:"slug"`
	DeployID  *string  `json:"deployId"`
	Title     *string  `json:"title"`
	URL       string   `json:"url"`
	CreatedBy *string  `json:"createdBy"`
	CreatedAt *string  `json:"createdAt"`
	Tags      []string `json:"tags"`
	APIBase   string   `json:"apiBase"`
}

type overlayBundle struct {
	Script string
	Styles []string
}

type viteManifestEntry struct {
	File    string   `json:"file"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
}

var (
	overlayBundleOnce sync.Once
	overlayBundleInfo overlayBundle
	overlayBundleErr  error
)

func (s *Server) handleOverlayAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	assetPath := strings.TrimPrefix(r.URL.Path, "/_jot/")
	if assetPath == "" || strings.Contains(assetPath, "..") || !strings.HasPrefix(assetPath, "assets/") {
		http.NotFound(w, r)
		return
	}
	body, err := overlayassets.Dist.ReadFile(path.Join("dist", assetPath))
	if err != nil {
		if errorsIsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", manifest.ContentTypeForPath(assetPath))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func loadOverlayBundle() (overlayBundle, error) {
	overlayBundleOnce.Do(func() {
		raw, err := overlayassets.Dist.ReadFile("dist/.vite/manifest.json")
		if err != nil {
			overlayBundleErr = err
			return
		}
		var manifestFile map[string]viteManifestEntry
		if err := json.Unmarshal(raw, &manifestFile); err != nil {
			overlayBundleErr = err
			return
		}
		for _, entry := range manifestFile {
			if !entry.IsEntry {
				continue
			}
			if entry.File == "" {
				overlayBundleErr = fmt.Errorf("overlay manifest entry is missing file")
				return
			}
			overlayBundleInfo = overlayBundle{Script: overlayAssetURL(entry.File), Styles: make([]string, 0, len(entry.CSS))}
			for _, css := range entry.CSS {
				overlayBundleInfo.Styles = append(overlayBundleInfo.Styles, overlayAssetURL(css))
			}
			return
		}
		overlayBundleErr = fmt.Errorf("overlay manifest has no entry script")
	})
	return overlayBundleInfo, overlayBundleErr
}

func overlayAssetURL(file string) string {
	return "/_jot/" + strings.TrimPrefix(file, "/")
}

func (s *Server) injectOverlayHTML(body []byte, payload overlayBootstrap) ([]byte, error) {
	bundle, err := loadOverlayBundle()
	if err != nil {
		return nil, err
	}
	bootstrap, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var headSnippet bytes.Buffer
	for _, style := range bundle.Styles {
		fmt.Fprintf(&headSnippet, `<link rel="preload" as="style" href="%s" data-jot-overlay-style>`+"\n", html.EscapeString(style))
	}

	var bodySnippet bytes.Buffer
	bodySnippet.WriteString(`<div id="jot-overlay-root"></div>` + "\n")
	fmt.Fprintf(&bodySnippet, `<script type="application/json" id="jot-overlay-bootstrap">%s</script>`+"\n", bootstrap)
	fmt.Fprintf(&bodySnippet, `<script type="module" crossorigin src="%s"></script>`+"\n", html.EscapeString(bundle.Script))

	out := insertBeforeHTMLMarker(body, []byte("</head>"), headSnippet.Bytes())
	out = insertBeforeHTMLMarker(out, []byte("</body>"), bodySnippet.Bytes())
	return out, nil
}

func insertBeforeHTMLMarker(body, marker, snippet []byte) []byte {
	if len(snippet) == 0 {
		return body
	}
	lower := bytes.ToLower(body)
	idx := bytes.LastIndex(lower, bytes.ToLower(marker))
	if idx < 0 {
		out := make([]byte, 0, len(body)+len(snippet)+1)
		out = append(out, body...)
		out = append(out, '\n')
		out = append(out, snippet...)
		return out
	}
	out := make([]byte, 0, len(body)+len(snippet))
	out = append(out, body[:idx]...)
	out = append(out, snippet...)
	out = append(out, body[idx:]...)
	return out
}

func (s *Server) overlayBootstrapForRequest(r *http.Request, m *manifest.Manifest) overlayBootstrap {
	payload := overlayBootstrap{
		URL:     s.absoluteRequestURL(r),
		Tags:    []string{},
		APIBase: "/_api",
	}
	if m == nil {
		return payload
	}
	payload.Slug = ptrIfNotEmpty(m.Slug)
	payload.DeployID = ptrIfNotEmpty(m.ID)
	payload.Title = ptrIfNotEmpty(m.Title)
	payload.CreatedBy = ptrIfNotEmpty(m.CreatedBy)
	if !m.CreatedAt.IsZero() {
		createdAt := m.CreatedAt.UTC().Format(time.RFC3339)
		payload.CreatedAt = &createdAt
	}
	if len(m.Tags) > 0 {
		payload.Tags = append([]string(nil), m.Tags...)
	}
	return payload
}

func ptrIfNotEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func (s *Server) absoluteRequestURL(r *http.Request) string {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	if base == "" {
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		base = scheme + "://" + r.Host
	}
	return base + r.URL.RequestURI()
}

func shouldInjectOverlay(method, contentType string) bool {
	return method == http.MethodGet && isHTMLContentType(contentType)
}

func isHTMLContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.EqualFold(mediaType, "text/html")
}

func headerValue(headers map[string]string, key string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func errorsIsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
