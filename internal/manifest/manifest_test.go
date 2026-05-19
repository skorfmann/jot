package manifest

import (
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	valid := []string{"a", "a1", "a-b", "abc123", "a2345678"}
	for _, slug := range valid {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("expected %q to be valid: %v", slug, err)
		}
	}
	invalid := []string{"", "_meta", "-a", "a-", "A", "a_b"}
	for _, slug := range invalid {
		if err := ValidateSlug(slug); err == nil {
			t.Fatalf("expected %q to be invalid", slug)
		}
	}
}

func TestResolve(t *testing.T) {
	m := &Manifest{
		SchemaVersion: SchemaVersion,
		ID:            "01HXABCDEFGHJKMNPQRSTVWXYZ",
		Slug:          "abc123",
		CreatedAt:     time.Now(),
		CreatedBy:     "dev@local",
		SPAFallback:   "/index.html",
		Files: map[string]File{
			"/index.html":      {SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1, ContentType: "text/html"},
			"/about.html":      {SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Size: 1, ContentType: "text/html"},
			"/docs/index.html": {SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Size: 1, ContentType: "text/html"},
			"/404.html":        {SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Size: 1, ContentType: "text/html"},
		},
	}
	got, ok := Resolve(m, "/about", true)
	if !ok || got.Path != "/about.html" || got.StatusCode != 200 {
		t.Fatalf("about resolution = %#v, %v", got, ok)
	}
	got, ok = Resolve(m, "/docs", true)
	if !ok || got.Path != "/docs/index.html" || got.StatusCode != 301 || got.RedirectTo != "/docs/" {
		t.Fatalf("docs resolution = %#v, %v", got, ok)
	}
	got, ok = Resolve(m, "/docs/", true)
	if !ok || got.Path != "/docs/index.html" || got.StatusCode != 200 {
		t.Fatalf("docs slash resolution = %#v, %v", got, ok)
	}
	got, ok = Resolve(m, "/missing-route", true)
	if !ok || got.Path != "/index.html" || got.StatusCode != 200 {
		t.Fatalf("spa resolution = %#v, %v", got, ok)
	}
	got, ok = Resolve(m, "/missing.png", false)
	if !ok || got.Path != "/404.html" || got.StatusCode != 404 {
		t.Fatalf("404 resolution = %#v, %v", got, ok)
	}
}
