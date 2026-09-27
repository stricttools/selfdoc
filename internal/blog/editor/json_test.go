package editor

import "testing"

// The bodies are the Python server's bodies, so the encoder's spelling is
// asserted directly: the separators Python's json.dumps writes, its
// ensure_ascii escaping, and the three characters Go's own encoder would
// rewrite and Python's does not.

func TestTheEncoderWritesThePythonsSpelling(t *testing.T) {
	t.Run("a struct keeps its declared member order", func(t *testing.T) {
		type payload struct {
			Repo  string `json:"repo"`
			Path  string `json:"path"`
			Saved bool   `json:"saved"`
			Bytes int    `json:"bytes"`
		}
		want := `{"repo": "proj", "path": "hello.md", "saved": true, "bytes": 12}`
		assertEncodes(t, payload{"proj", "hello.md", true, 12}, want)
	})

	t.Run("HTML is not escaped", func(t *testing.T) {
		// Go's encoding/json rewrites "<", ">" and "&" as their \u escapes,
		// which would rewrite every byte of a previewed page.
		type payload struct {
			HTML string `json:"html"`
		}
		assertEncodes(t, payload{`<a href="x">a & b</a>`},
			`{"html": "<a href=\"x\">a & b</a>"}`)
	})

	t.Run("a character outside ASCII is escaped", func(t *testing.T) {
		type payload struct {
			Word string `json:"word"`
		}
		assertEncodes(t, payload{"naïve"}, `{"word": "na\u00efve"}`)
	})

	t.Run("an empty list is a list", func(t *testing.T) {
		type payload struct {
			Tags []string `json:"tags"`
		}
		assertEncodes(t, payload{nil}, `{"tags": []}`)
	})

	t.Run("an absent line is null", func(t *testing.T) {
		assertEncodes(t, LintFinding{Code: "description-too-long-for-search-snippet", Severity: "warn", Message: "x"},
			`{"code": "description-too-long-for-search-snippet", "severity": "warn", "line": null, "message": "x"}`)
	})

	t.Run("a page target carries no section members", func(t *testing.T) {
		target := Target{
			Kind: "page", Repo: "alpha", Slug: "alpha", Project: "Alpha",
			Title: "Alpha Guide", Page: "guide.md",
			Address: "alpha/guide/", Href: "../../alpha/guide/",
		}
		assertEncodes(t, target,
			`{"kind": "page", "repo": "alpha", "slug": "alpha", `+
				`"project": "Alpha", "title": "Alpha Guide", "page": "guide.md", `+
				`"anchor": "", "address": "alpha/guide/", "href": "../../alpha/guide/"}`)
	})

	t.Run("a section target carries the page it sits on and its level", func(t *testing.T) {
		pageTitle := "Alpha Guide"
		level := 2
		target := Target{
			Kind: "section", Repo: "alpha", Slug: "alpha", Project: "Alpha",
			Title: "Getting started", Page: "guide.md",
			PageTitle: &pageTitle, Level: &level, Anchor: "getting-started",
			Address: "alpha/guide/#getting-started",
			Href:    "../../alpha/guide/#getting-started",
		}
		assertEncodes(t, target,
			`{"kind": "section", "repo": "alpha", "slug": "alpha", `+
				`"project": "Alpha", "title": "Getting started", "page": "guide.md", `+
				`"page_title": "Alpha Guide", "level": 2, "anchor": "getting-started", `+
				`"address": "alpha/guide/#getting-started", `+
				`"href": "../../alpha/guide/#getting-started"}`)
	})
}

// assertEncodes fails the test unless a payload encodes to want.
func assertEncodes(t *testing.T, payload any, want string) {
	t.Helper()
	body, err := encodeJSON(payload)
	if err != nil {
		t.Fatalf("encodeJSON: %v", err)
	}
	if string(body) != want {
		t.Errorf("encoded\n  %s\nwant\n  %s", body, want)
	}
}
