package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/skorfmann/jot/internal/manifest"
	"github.com/skorfmann/jot/internal/storage"
	"github.com/spf13/cobra"
)

type pushOptions struct {
	slug       string
	title      string
	summary    string
	tags       []string
	index      string
	spa        string
	headers    []string
	actor      string
	jsonOutput bool
}

type localFile struct {
	SourcePath   string
	ManifestPath string
	File         manifest.File
}

func (r *Root) pushCmd() *cobra.Command {
	var opts pushOptions
	cmd := &cobra.Command{
		Use:   "push <path>",
		Short: "Push a file or directory and print its private URL",
		Long: `Push a built static artifact to jot.

Use this for finished HTML artifacts, reports, or built directories. Metadata is
stored on the deploy so future ` + "`jot ls --search`" + ` queries have useful context.`,
		Example: `  jot push ./report.html --title "Q2 Sales" --summary "Q2 revenue by region" --tag report
  jot push ./dist --as dashboard --spa /index.html --title "Dashboard"
  jot push ./dist --header "/assets/**=cache-control: public, max-age=31536000, immutable"
  jot push ./report.html --json --actor claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.runPush(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.slug, "as", "", "Use or create this slug. Default: generated 8-character slug.")
	cmd.Flags().StringVar(&opts.title, "title", "", "Short human-readable label.\nExample: \"Q2 Sales Report\".")
	cmd.Flags().StringVar(&opts.summary, "summary", "", "A 1-2 sentence description used by jot ls --search.\nWrite it as a note for your future self or another agent.\nExample: \"Q2 2026 revenue breakdown by region, generated from BI export on May 19.\"")
	cmd.Flags().StringArrayVar(&opts.tags, "tag", nil, "Repeatable tag for categorization.\nExample: --tag report --tag q2 --tag finance")
	cmd.Flags().StringVar(&opts.index, "index", "", "Directory file to expose as /index.html when no index.html exists.\nExample: --index report.html")
	cmd.Flags().StringVar(&opts.spa, "spa", "", "Manifest path to serve for unknown HTML routes.\nExample: --spa /index.html")
	cmd.Flags().StringArrayVar(&opts.headers, "header", nil, "Repeatable per-path response header override.\nFormat: \"<glob>=<key>: <value>\"\nExample: --header \"/assets/**=cache-control: public, max-age=31536000, immutable\"")
	cmd.Flags().StringVar(&opts.actor, "actor", os.Getenv("JOT_ACTOR"), "Free-form actor recorded for audit only.\nExample: claude-code")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit newline-delimited JSON upload events and a final result.")
	return cmd
}

func (r *Root) runPush(ctx context.Context, src string, opts pushOptions) error {
	client, err := newAPIClient(r.serverURL)
	if err != nil {
		return err
	}
	files, err := collectFiles(src, opts.index)
	if err != nil {
		return err
	}
	if opts.slug == "" {
		opts.slug, err = manifest.RandomSlug()
		if err != nil {
			return err
		}
	}
	if err := manifest.ValidateSlug(opts.slug); err != nil {
		return err
	}
	headerMap, err := parseHeaders(opts.headers)
	if err != nil {
		return err
	}
	deployID := ulid.Make().String()
	m := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            deployID,
		Slug:          opts.slug,
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     "pending",
		Actor:         opts.actor,
		Title:         opts.title,
		Summary:       opts.summary,
		Tags:          opts.tags,
		SPAFallback:   opts.spa,
		Headers:       headerMap,
		Files:         map[string]manifest.File{},
	}
	if m.SPAFallback != "" {
		np, err := manifest.NormalizePath(m.SPAFallback)
		if err != nil {
			return err
		}
		m.SPAFallback = np
	}
	req := checkRequest{Files: make([]checkFile, 0, len(files))}
	for _, f := range files {
		m.Files[f.ManifestPath] = f.File
		req.Files = append(req.Files, checkFile{Path: f.ManifestPath, SHA256: f.File.SHA256, Size: f.File.Size, ContentType: f.File.ContentType})
	}
	var check checkResponse
	if err := client.request(ctx, http.MethodPost, "/_api/deploys:check", req, &check); err != nil {
		return err
	}
	missingByHash := map[string]string{}
	for _, ref := range check.Missing {
		missingByHash[ref.SHA256] = ref.URL
	}
	done := 0
	for _, f := range files {
		uploadURL, ok := missingByHash[f.File.SHA256]
		if !ok {
			continue
		}
		done++
		if opts.jsonOutput {
			_ = json.NewEncoder(r.out).Encode(map[string]any{"type": "upload", "file": f.ManifestPath, "done": done, "total": len(check.Missing)})
		} else {
			r.printf("Uploading %d/%d %s\n", done, len(check.Missing), f.ManifestPath)
		}
		if err := putFile(ctx, uploadURL, f); err != nil {
			return err
		}
	}
	var res struct {
		Manifest manifest.Manifest `json:"manifest"`
		URL      string            `json:"url"`
	}
	if err := client.request(ctx, http.MethodPut, "/_api/deploys/"+deployID, m, &res); err != nil {
		return err
	}
	if err := recordLocalPush(src, res.Manifest, res.URL); err != nil && !opts.jsonOutput {
		r.printf("Warning: could not update .jot local context: %v\n", err)
	}
	if opts.jsonOutput {
		return json.NewEncoder(r.out).Encode(map[string]any{"type": "result", "url": res.URL, "manifest": res.Manifest})
	}
	r.printf("Pushed -> %s\n", res.URL)
	if res.Manifest.Title != "" {
		r.printf("  Title:   %s\n", res.Manifest.Title)
	}
	if res.Manifest.Summary != "" {
		r.printf("  Summary: %s\n", res.Manifest.Summary)
	}
	if len(res.Manifest.Tags) > 0 {
		r.printf("  Tags:    %s\n", strings.Join(res.Manifest.Tags, ", "))
	}
	return nil
}

func collectFiles(src string, index string) ([]localFile, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	var files []localFile
	if !info.IsDir() {
		lf, err := hashFile(src, "/index.html")
		if err != nil {
			return nil, err
		}
		return []localFile{lf}, nil
	}
	root := src
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".jot" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		mp, err := manifest.NormalizePath(rel)
		if err != nil {
			return err
		}
		lf, err := hashFile(p, mp)
		if err != nil {
			return err
		}
		files = append(files, lf)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("source directory contains no regular files")
	}
	hasIndex := false
	for _, f := range files {
		if f.ManifestPath == "/index.html" {
			hasIndex = true
			break
		}
	}
	if !hasIndex && index != "" {
		indexPath := filepath.Join(root, index)
		for i := range files {
			if samePath(files[i].SourcePath, indexPath) {
				files[i].ManifestPath = "/index.html"
				hasIndex = true
				break
			}
		}
	}
	if !hasIndex {
		return nil, errors.New("directory pushes need index.html or --index <file>")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ManifestPath < files[j].ManifestPath })
	return files, nil
}

func hashFile(src string, manifestPath string) (localFile, error) {
	f, err := os.Open(src)
	if err != nil {
		return localFile{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return localFile{}, err
	}
	return localFile{
		SourcePath:   src,
		ManifestPath: manifestPath,
		File: manifest.File{
			SHA256:      hex.EncodeToString(h.Sum(nil)),
			Size:        n,
			ContentType: manifest.ContentTypeForPath(manifestPath),
		},
	}, nil
}

func putFile(ctx context.Context, uploadURL string, lf localFile) error {
	body, err := os.ReadFile(lf.SourcePath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", lf.File.ContentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("upload %s failed: %s: %s", lf.ManifestPath, res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func parseHeaders(values []string) (map[string]map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := map[string]map[string]string{}
	for _, raw := range values {
		glob, rest, ok := strings.Cut(raw, "=")
		if !ok || glob == "" {
			return nil, fmt.Errorf("header %q must look like <glob>=<key>: <value>", raw)
		}
		key, value, ok := strings.Cut(rest, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("header %q must include a header key and value", raw)
		}
		if out[glob] == nil {
			out[glob] = map[string]string{}
		}
		out[glob][strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
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

type localPush struct {
	Slug     string    `json:"slug"`
	URL      string    `json:"url"`
	Title    string    `json:"title,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	PushedAt time.Time `json:"pushed_at"`
	PushedBy string    `json:"pushed_by"`
}

func recordLocalPush(src string, m manifest.Manifest, deployURL string) error {
	dir := src
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		dir = filepath.Dir(src)
	}
	jotDir := filepath.Join(dir, ".jot")
	if err := os.MkdirAll(jotDir, 0o700); err != nil {
		return err
	}
	pushesPath := filepath.Join(jotDir, "pushes.json")
	var pushes []localPush
	if body, err := os.ReadFile(pushesPath); err == nil {
		_ = json.Unmarshal(body, &pushes)
	}
	pushes = append(pushes, localPush{
		Slug:     m.Slug,
		URL:      deployURL,
		Title:    m.Title,
		Summary:  m.Summary,
		Tags:     m.Tags,
		PushedAt: m.CreatedAt,
		PushedBy: m.CreatedBy,
	})
	body, err := json.MarshalIndent(pushes, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(pushesPath, body, 0o600); err != nil {
		return err
	}
	return appendGitignore(dir)
}

func appendGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if strings.Contains(string(body), ".jot/") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n# Local jot push context\n.jot/\n")
	return err
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}
