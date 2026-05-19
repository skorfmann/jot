package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/skorfmann/jot/internal/manifest"
	"github.com/skorfmann/jot/internal/server"
	"github.com/spf13/cobra"
)

func (r *Root) listCmd() *cobra.Command {
	var mine, local, jsonOut bool
	var tags []string
	var search string
	var limit int
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List recent deploys",
		RunE: func(cmd *cobra.Command, args []string) error {
			if local {
				return r.listLocal(jsonOut)
			}
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			q := url.Values{}
			if mine {
				q.Set("mine", "1")
			}
			if search != "" {
				q.Set("search", search)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			for _, tag := range tags {
				q.Add("tag", tag)
			}
			var res struct {
				Deploys []manifest.Manifest `json:"deploys"`
			}
			if err := client.request(cmd.Context(), http.MethodGet, "/_api/deploys?"+q.Encode(), nil, &res); err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(r.out).Encode(res.Deploys)
			}
			for _, d := range res.Deploys {
				r.printf("%s  %-12s  %-24s  %s\n", d.CreatedAt.Format("2006-01-02 15:04"), d.Slug, d.CreatedBy, firstNonEmpty(d.Title, d.Summary))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&mine, "mine", false, "Only show deploys pushed by the current identity.")
	cmd.Flags().BoolVar(&local, "local", false, "Only show deploys tracked in ./.jot/pushes.json.")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Repeatable tag filter.")
	cmd.Flags().StringVar(&search, "search", "", "Substring search over title and summary.")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum deploys to return.")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON.")
	return cmd
}

func (r *Root) inspectCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "inspect <slug|id>",
		Short: "Show full metadata and file list for a deploy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			var res struct {
				Manifest manifest.Manifest `json:"manifest"`
				URL      string            `json:"url"`
			}
			if err := client.request(cmd.Context(), http.MethodGet, "/_api/deploys/"+url.PathEscape(args[0]), nil, &res); err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(r.out).Encode(res)
			}
			r.printf("URL:        %s\n", res.URL)
			r.printf("ID:         %s\n", res.Manifest.ID)
			r.printf("Slug:       %s\n", res.Manifest.Slug)
			r.printf("Created:    %s\n", res.Manifest.CreatedAt)
			r.printf("Created by: %s\n", res.Manifest.CreatedBy)
			if res.Manifest.Title != "" {
				r.printf("Title:      %s\n", res.Manifest.Title)
			}
			if res.Manifest.Summary != "" {
				r.printf("Summary:    %s\n", res.Manifest.Summary)
			}
			r.printf("Files:\n")
			for p, f := range res.Manifest.Files {
				r.printf("  %-40s %8d %s\n", p, f.Size, f.ContentType)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON.")
	return cmd
}

func (r *Root) historyCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "history <slug>",
		Short: "Show bounded deploy history for a slug",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			var res struct {
				Deploys []manifest.Manifest `json:"deploys"`
			}
			if err := client.request(cmd.Context(), http.MethodGet, "/_api/slugs/"+url.PathEscape(args[0])+"/history", nil, &res); err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(r.out).Encode(res.Deploys)
			}
			for _, d := range res.Deploys {
				r.printf("%s  %s  %s\n", d.CreatedAt.Format("2006-01-02 15:04"), d.ID, firstNonEmpty(d.Title, d.Summary))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON.")
	return cmd
}

func (r *Root) rollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <slug> [id]",
		Short: "Restore the previous or a specific manifest for a slug",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			req := map[string]string{}
			if len(args) == 2 {
				req["id"] = args[1]
			}
			var res struct {
				Manifest manifest.Manifest `json:"manifest"`
				URL      string            `json:"url"`
			}
			if err := client.request(cmd.Context(), http.MethodPost, "/_api/slugs/"+url.PathEscape(args[0])+"/rollback", req, &res); err != nil {
				return err
			}
			r.printf("Rolled back %s -> %s\n", args[0], res.Manifest.ID)
			return nil
		},
	}
}

func (r *Root) rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <slug>",
		Short: "Hard-delete a slug and its manifests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			if err := client.request(cmd.Context(), http.MethodDelete, "/_api/slugs/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			r.printf("Deleted %s\n", args[0])
			return nil
		},
	}
}

func (r *Root) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the authenticated identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(r.serverURL)
			if err != nil {
				return err
			}
			var res map[string]any
			if err := client.request(cmd.Context(), http.MethodGet, "/_api/whoami", nil, &res); err != nil {
				return err
			}
			return json.NewEncoder(r.out).Encode(res)
		},
	}
}

func (r *Root) initCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "init", Short: "Scaffold jot configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "server",
		Short: "Print a documented server config scaffold",
		Run: func(cmd *cobra.Command, args []string) {
			r.printf("%s", server.ServerConfigScaffold())
		},
	})
	return cmd
}

func (r *Root) listLocal(jsonOut bool) error {
	body, err := os.ReadFile(filepath.Join(".jot", "pushes.json"))
	if err != nil {
		return err
	}
	if jsonOut {
		_, err = r.out.Write(body)
		if err == nil && !strings.HasSuffix(string(body), "\n") {
			r.printf("\n")
		}
		return err
	}
	var pushes []localPush
	if err := json.Unmarshal(body, &pushes); err != nil {
		return err
	}
	for _, p := range pushes {
		r.printf("%s  %-12s  %-24s  %s\n", p.PushedAt.Format("2006-01-02 15:04"), p.Slug, p.PushedBy, firstNonEmpty(p.Title, p.Summary, p.URL))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
