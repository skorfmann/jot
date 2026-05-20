package server

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/skorfmann/jot/internal/auth"
	"github.com/skorfmann/jot/internal/manifest"
	"github.com/skorfmann/jot/internal/storage"
)

type rootDeploy struct {
	ID        string
	Slug      string
	URL       string
	CreatedAt time.Time
	CreatedBy string
	Title     string
	Summary   string
	Tags      []string
	FileCount int
	TotalSize int64
}

type rootIndexData struct {
	Identity auth.Identity
	Deploys  []rootDeploy
	Query    string
	Mine     bool
	Count    int
}

func (s *Server) handleRootIndex(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	deploys, err := s.currentDeploys(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	mine := r.URL.Query().Get("mine") == "1" || r.URL.Query().Get("mine") == "true"
	deploys = filterRootDeploys(deploys, id, query, mine)

	data := rootIndexData{
		Identity: id,
		Deploys:  deploys,
		Query:    query,
		Mine:     mine,
		Count:    len(deploys),
	}
	var body bytes.Buffer
	if err := rootIndexTemplate.Execute(&body, data); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func (s *Server) currentDeploys(ctx context.Context) ([]rootDeploy, error) {
	all, err := s.store.ListAllManifests(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(all))
	deploys := make([]rootDeploy, 0, len(all))
	for _, item := range all {
		if item == nil {
			continue
		}
		if _, ok := seen[item.Slug]; ok {
			continue
		}
		seen[item.Slug] = struct{}{}
		current, err := s.store.GetCurrent(ctx, item.Slug)
		if err != nil {
			return nil, err
		}
		if !current.Found {
			continue
		}
		m, err := s.store.GetManifest(ctx, item.Slug, current.Pointer.ManifestID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				continue
			}
			return nil, err
		}
		deploys = append(deploys, s.rootDeployFromManifest(m))
	}
	sort.Slice(deploys, func(i, j int) bool {
		return deploys[i].CreatedAt.After(deploys[j].CreatedAt)
	})
	return deploys, nil
}

func (s *Server) rootDeployFromManifest(m *manifest.Manifest) rootDeploy {
	var total int64
	for _, file := range m.Files {
		total += file.Size
	}
	return rootDeploy{
		ID:        m.ID,
		Slug:      m.Slug,
		URL:       s.deployURL(m.Slug),
		CreatedAt: m.CreatedAt,
		CreatedBy: m.CreatedBy,
		Title:     m.Title,
		Summary:   m.Summary,
		Tags:      m.Tags,
		FileCount: len(m.Files),
		TotalSize: total,
	}
}

func filterRootDeploys(deploys []rootDeploy, id auth.Identity, query string, mine bool) []rootDeploy {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := deploys[:0]
	for _, deploy := range deploys {
		if mine && deploy.CreatedBy != id.Email {
			continue
		}
		if query != "" && !rootDeployMatches(deploy, query) {
			continue
		}
		filtered = append(filtered, deploy)
	}
	return filtered
}

func rootDeployMatches(deploy rootDeploy, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		deploy.Slug,
		deploy.ID,
		deploy.CreatedBy,
		deploy.Title,
		deploy.Summary,
		strings.Join(deploy.Tags, " "),
	}, " "))
	return strings.Contains(haystack, query)
}

var rootIndexTemplate = template.Must(template.New("root-index").Funcs(template.FuncMap{
	"displayTitle": func(deploy rootDeploy) string {
		if strings.TrimSpace(deploy.Title) != "" {
			return deploy.Title
		}
		return "Untitled deploy"
	},
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Local().Format("2006-01-02 15:04")
	},
	"formatBytes": storage.FormatLimit,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jot Deploys</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f7f9fa;
      --panel: #ffffff;
      --ink: #1e252b;
      --muted: #63717a;
      --line: #dce3e8;
      --accent: #126c5f;
      --accent-soft: #e6f3f0;
      --warn-soft: #fff4df;
      --warn: #86510c;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      background: var(--panel);
      border-bottom: 1px solid var(--line);
    }
    main, .top {
      width: min(1120px, calc(100vw - 32px));
      margin: 0 auto;
    }
    .top {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 24px;
      padding: 28px 0 20px;
      align-items: end;
    }
    h1, h2, p { margin: 0; }
    h1 {
      font-size: 30px;
      line-height: 1.1;
      letter-spacing: 0;
    }
    .sub {
      margin-top: 7px;
      color: var(--muted);
      font-size: 15px;
    }
    .identity {
      color: var(--muted);
      font-size: 13px;
      text-align: right;
    }
    main { padding: 22px 0 44px; }
    .toolbar {
      display: grid;
      grid-template-columns: 1fr auto auto;
      gap: 10px;
      align-items: center;
      margin-bottom: 16px;
    }
    input[type="search"] {
      width: 100%;
      height: 38px;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 0 11px;
      font: inherit;
      background: var(--panel);
      color: var(--ink);
    }
    .check {
      display: inline-flex;
      gap: 7px;
      align-items: center;
      height: 38px;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 0 10px;
      background: var(--panel);
      color: var(--ink);
      white-space: nowrap;
    }
    button {
      height: 38px;
      border: 0;
      border-radius: 6px;
      padding: 0 14px;
      background: var(--accent);
      color: #fff;
      font: inherit;
      font-weight: 650;
      cursor: pointer;
    }
    .count {
      color: var(--muted);
      margin-bottom: 10px;
      font-size: 13px;
    }
    .list {
      display: grid;
      gap: 10px;
    }
    .deploy {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 14px;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 15px;
    }
    .title-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      margin-bottom: 6px;
    }
    h2 {
      font-size: 17px;
      line-height: 1.25;
      letter-spacing: 0;
    }
    .slug {
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent);
      padding: 2px 7px;
      font: 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
    .summary {
      color: var(--muted);
      max-width: 760px;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 10px 14px;
      margin-top: 10px;
      color: var(--muted);
      font-size: 13px;
    }
    .tags {
      display: flex;
      flex-wrap: wrap;
      gap: 5px;
      margin-top: 10px;
    }
    .tag {
      border-radius: 999px;
      background: var(--warn-soft);
      color: var(--warn);
      padding: 2px 7px;
      font-size: 12px;
      font-weight: 650;
    }
    .actions {
      display: grid;
      gap: 8px;
      justify-items: end;
      align-content: start;
      min-width: 260px;
    }
    a.url {
      color: var(--accent);
      font: 13px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      overflow-wrap: anywhere;
      text-align: right;
      text-decoration: none;
    }
    a.url:hover { text-decoration: underline; }
    .empty {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 28px;
      text-align: center;
      color: var(--muted);
    }
    @media (max-width: 760px) {
      .top, .toolbar, .deploy {
        grid-template-columns: 1fr;
      }
      .identity, .actions, a.url {
        text-align: left;
        justify-items: start;
      }
    }
  </style>
</head>
<body>
  <header>
    <div class="top">
      <div>
        <h1>Jot Deploys</h1>
        <p class="sub">Current private pages published to this server.</p>
      </div>
      <div class="identity">Signed in as<br><strong>{{.Identity.Email}}</strong></div>
    </div>
  </header>
  <main>
    <form class="toolbar" method="get" action="/">
      <input type="search" name="q" value="{{.Query}}" placeholder="Search title, summary, slug, tag, or owner" aria-label="Search deploys">
      <label class="check"><input type="checkbox" name="mine" value="1" {{if .Mine}}checked{{end}}> Mine</label>
      <button type="submit">Filter</button>
    </form>
    <p class="count">{{.Count}} current deploy{{if ne .Count 1}}s{{end}}</p>
    {{if .Deploys}}
      <div class="list">
      {{range .Deploys}}
        <article class="deploy">
          <div>
            <div class="title-row">
              <h2>{{displayTitle .}}</h2>
              <span class="slug">{{.Slug}}</span>
            </div>
            {{if .Summary}}<p class="summary">{{.Summary}}</p>{{end}}
            <div class="meta">
              <span>{{formatTime .CreatedAt}}</span>
              <span>{{.CreatedBy}}</span>
              <span>{{.FileCount}} file{{if ne .FileCount 1}}s{{end}}</span>
              <span>{{formatBytes .TotalSize}}</span>
            </div>
            {{if .Tags}}
              <div class="tags">{{range .Tags}}<span class="tag">{{.}}</span>{{end}}</div>
            {{end}}
          </div>
          <div class="actions">
            <a class="url" href="/{{.Slug}}/">{{.URL}}</a>
          </div>
        </article>
      {{end}}
      </div>
    {{else}}
      <div class="empty">No current deploys match this view.</div>
    {{end}}
  </main>
</body>
</html>`))
