package manifest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/oklog/ulid/v2"
)

const (
	SchemaVersion = 1
	MaxSlugLength = 64
)

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

type Manifest struct {
	SchemaVersion int                          `json:"schema_version"`
	ID            string                       `json:"id"`
	Slug          string                       `json:"slug"`
	CreatedAt     time.Time                    `json:"created_at"`
	CreatedBy     string                       `json:"created_by"`
	Actor         string                       `json:"actor,omitempty"`
	Title         string                       `json:"title,omitempty"`
	Summary       string                       `json:"summary,omitempty"`
	Tags          []string                     `json:"tags,omitempty"`
	SPAFallback   string                       `json:"spa_fallback,omitempty"`
	Headers       map[string]map[string]string `json:"headers,omitempty"`
	Files         map[string]File              `json:"files"`
}

type File struct {
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type CurrentPointer struct {
	ManifestID string `json:"manifest_id"`
}

type ResolvedFile struct {
	Path       string
	File       File
	StatusCode int
	RedirectTo string
}

func NewID(now time.Time) (string, error) {
	entropy := ulid.Monotonic(rand.Reader, 0)
	return ulid.MustNew(ulid.Timestamp(now), entropy).String(), nil
}

func RandomSlug() (string, error) {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return strings.ToLower(enc[:8]), nil
}

func ValidateSlug(slug string) error {
	if slug == "" {
		return errors.New("slug is required")
	}
	if strings.HasPrefix(slug, "_") {
		return fmt.Errorf("slug %q is reserved", slug)
	}
	if len(slug) > MaxSlugLength || !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug %q must match ^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$", slug)
	}
	return nil
}

func NormalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	clean := path.Clean(p)
	if clean == "." || clean == "/" {
		return "/index.html", nil
	}
	if strings.Contains(clean, "\x00") {
		return "", errors.New("path contains NUL byte")
	}
	return clean, nil
}

func Validate(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is required")
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema_version %d", m.SchemaVersion)
	}
	if _, err := ulid.ParseStrict(m.ID); err != nil {
		return fmt.Errorf("id must be a ULID: %w", err)
	}
	if err := ValidateSlug(m.Slug); err != nil {
		return err
	}
	if m.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if m.CreatedBy == "" {
		return errors.New("created_by is required")
	}
	if len(m.Files) == 0 {
		return errors.New("manifest must contain at least one file")
	}
	seen := make(map[string]struct{}, len(m.Files))
	for p, f := range m.Files {
		np, err := NormalizePath(p)
		if err != nil {
			return fmt.Errorf("invalid file path %q: %w", p, err)
		}
		if np != p {
			return fmt.Errorf("file path %q must be normalized as %q", p, np)
		}
		if _, exists := seen[p]; exists {
			return fmt.Errorf("duplicate file path %q", p)
		}
		seen[p] = struct{}{}
		if len(f.SHA256) != 64 {
			return fmt.Errorf("file %s has invalid sha256 length", p)
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return fmt.Errorf("file %s has invalid sha256: %w", p, err)
		}
		if f.Size < 0 {
			return fmt.Errorf("file %s has negative size", p)
		}
		if f.ContentType == "" {
			return fmt.Errorf("file %s is missing content_type", p)
		}
	}
	if m.SPAFallback != "" {
		fallback, err := NormalizePath(m.SPAFallback)
		if err != nil {
			return fmt.Errorf("invalid spa_fallback: %w", err)
		}
		if fallback != m.SPAFallback {
			return fmt.Errorf("spa_fallback %q must be normalized as %q", m.SPAFallback, fallback)
		}
		if _, ok := m.Files[m.SPAFallback]; !ok {
			return fmt.Errorf("spa_fallback %q is not present in files", m.SPAFallback)
		}
	}
	return nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ContentTypeForPath(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if ext == ".js" {
		return "application/javascript"
	}
	if ext == ".mjs" {
		return "application/javascript"
	}
	if ext == ".css" {
		return "text/css; charset=utf-8"
	}
	if ext == ".html" || ext == ".htm" {
		return "text/html; charset=utf-8"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func Resolve(m *Manifest, requestPath string, acceptHTML bool) (ResolvedFile, bool) {
	hadTrailingSlash := strings.HasSuffix(requestPath, "/") && requestPath != "/"
	rest, err := NormalizePath(requestPath)
	if err != nil {
		return ResolvedFile{}, false
	}
	if f, ok := m.Files[rest]; ok {
		return ResolvedFile{Path: rest, File: f, StatusCode: 200}, true
	}
	if hadTrailingSlash {
		indexPath := path.Join(rest, "index.html")
		if !strings.HasPrefix(indexPath, "/") {
			indexPath = "/" + indexPath
		}
		if f, ok := m.Files[indexPath]; ok {
			return ResolvedFile{Path: indexPath, File: f, StatusCode: 200}, true
		}
	}
	if path.Ext(rest) == "" {
		htmlPath := rest + ".html"
		if f, ok := m.Files[htmlPath]; ok {
			return ResolvedFile{Path: htmlPath, File: f, StatusCode: 200}, true
		}
		indexPath := path.Join(rest, "index.html")
		if !strings.HasPrefix(indexPath, "/") {
			indexPath = "/" + indexPath
		}
		if f, ok := m.Files[indexPath]; ok {
			redirectTo := rest
			if !strings.HasSuffix(redirectTo, "/") {
				redirectTo += "/"
			}
			return ResolvedFile{Path: indexPath, File: f, StatusCode: 301, RedirectTo: redirectTo}, true
		}
	}
	if acceptHTML && m.SPAFallback != "" {
		if f, ok := m.Files[m.SPAFallback]; ok {
			return ResolvedFile{Path: m.SPAFallback, File: f, StatusCode: 200}, true
		}
	}
	if f, ok := m.Files["/404.html"]; ok {
		return ResolvedFile{Path: "/404.html", File: f, StatusCode: 404}, true
	}
	return ResolvedFile{}, false
}

func HeadersForPath(m *Manifest, p string) map[string]string {
	if len(m.Headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Headers))
	for glob := range m.Headers {
		keys = append(keys, glob)
	}
	sort.Strings(keys)
	for _, glob := range keys {
		ok, err := doublestar.PathMatch(strings.TrimPrefix(glob, "/"), strings.TrimPrefix(p, "/"))
		if err == nil && ok {
			return m.Headers[glob]
		}
		ok, err = doublestar.PathMatch(glob, p)
		if err == nil && ok {
			return m.Headers[glob]
		}
	}
	return nil
}

func MarshalCanonical(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
