package content

import (
	"path/filepath"
	"testing"
)

// cvDocument is a complete CV declaration: every section is required and
// non-empty, so there is no shorter valid document.
const cvDocument = `
format_version = 1

[identity]
name = "Ada Lovelace"
headline = "Analyst"
location = "London, England"
email = "ada@example.org"
summary = "I write notes about engines."

  [[identity.profile]]
  label = "example.org/ada"
  url = "https://example.org/ada"

[[skills]]
category = "Languages"
items = ["Analytical Engine notation"]

[[projects]]
name = "Note G"
technologies = ["Analytical Engine"]

[[interests]]
title = "Poetical science"
body = "Imagination is the discovering faculty."

[[education]]
degree = "Private tuition in mathematics"
years = "1833 - 1840"
institute = "University of London"
location = "London, England"

[[experience]]
role = "Translator and analyst"
period = "1842 - 1843"
company = "Scientific Memoirs"
location = "London, England"

[[languages]]
name = "English"
level = "Native"

[contact]
body = "Write to ada@example.org."
`

func cvConfig() map[string]any {
	return map[string]any{"author": map[string]any{
		"name": "Ada Lovelace",
		"url":  "https://example.org/ada",
	}}
}

func TestResolveCV(t *testing.T) {
	isolate(t)

	t.Run("renders the declared document", func(t *testing.T) {
		base := t.TempDir()
		write(t, filepath.Join(base, "stricttools", "docs", "cv.toml"), cvDocument)
		rendered, ok, err := ResolveContent("cv",
			map[string]string{"path": "stricttools/docs/cv.toml"}, nil, base,
			cvConfig())
		if err != nil || !ok {
			t.Fatalf("ResolveContent: ok=%v err=%v", ok, err)
		}
		wants(t, rendered, "Ada Lovelace", "Analytical Engine notation")
	})

	t.Run("a missing path attribute is a hard error", func(t *testing.T) {
		// A CV page that rendered a placeholder would publish a person's
		// record with holes in it.
		_, err := ResolveCV(nil, cvConfig(), t.TempDir())
		if err == nil {
			t.Fatal("a cv directive with no path must be refused")
		}
		wants(t, err.Error(), `directive 'cv' requires path="<file>"`, "docs/cv.toml")
	})

	t.Run("a path that is not a file is a hard error", func(t *testing.T) {
		_, err := ResolveCV(map[string]string{"path": "stricttools/docs/cv.toml"},
			cvConfig(), t.TempDir())
		if err == nil {
			t.Fatal("a cv directive naming no document must be refused")
		}
		wants(t, err.Error(), "directive 'cv': 'stricttools/docs/cv.toml' is not a file")
	})

	t.Run("a malformed document is the loader's error", func(t *testing.T) {
		base := t.TempDir()
		write(t, filepath.Join(base, "cv.toml"), "format_version = 1\n")
		_, err := ResolveCV(map[string]string{"path": "cv.toml"}, cvConfig(), base)
		if err == nil {
			t.Fatal("a document missing every section must be refused")
		}
	})
}
