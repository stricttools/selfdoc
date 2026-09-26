package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// requiredKeys are the declared facts every config carries, filled in by the
// writer below when a case does not name them. A case about one field should
// not have to restate the required ones -- and a case about a required field
// states it itself, which overrides these.
func requiredKeys() map[string]any {
	return map[string]any{
		"search_engine": "pagefind",
		"author": map[string]any{
			"name": "Test Author",
			"url":  "https://author.example",
		},
	}
}

// baseKeys are the valid source/base_url/version trio most cases build on.
func baseKeys(extra map[string]any) map[string]any {
	data := map[string]any{
		"source":   []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url": "https://example.com",
		"version":  "1.0.0",
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// writeConfig writes data as selfdoc.json inside a fresh temp directory,
// filling the required keys any case left unnamed, and returns the
// directory.
func writeConfig(t *testing.T, data map[string]any) string {
	t.Helper()
	payload := requiredKeys()
	for k, v := range data {
		payload[k] = v
	}
	return writeRawConfig(t, payload)
}

// writeRawConfig writes data verbatim, filling nothing in.
func writeRawConfig(t *testing.T, data map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshalling the case's document: %v", err)
	}
	return writeConfigBytes(t, string(encoded))
}

func writeConfigBytes(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "selfdoc.json"), []byte(text), 0o644); err != nil {
		t.Fatalf("writing selfdoc.json: %v", err)
	}
	return dir
}

// configCase is one document and the verdict it must produce. An empty
// wantErr means the document loads; a non-empty one is a substring the
// returned ConfigError must contain.
type configCase struct {
	name    string
	data    map[string]any
	raw     map[string]any
	wantErr string
	check   func(t *testing.T, cfg Config)
}

func (c configCase) run(t *testing.T) {
	t.Helper()
	var dir string
	if c.raw != nil {
		dir = writeRawConfig(t, c.raw)
	} else {
		dir = writeConfig(t, c.data)
	}
	cfg, err := Load(dir)
	if c.wantErr != "" {
		if err == nil {
			t.Fatalf("expected a ConfigError containing %q, got a loaded config", c.wantErr)
		}
		var configErr *ConfigError
		if !asConfigError(err, &configErr) {
			t.Fatalf("expected a *ConfigError, got %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Fatalf("error %q does not contain %q", err.Error(), c.wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("expected the document to load, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected a config, got nil")
	}
	if c.check != nil {
		c.check(t, cfg)
	}
}

func asConfigError(err error, target **ConfigError) bool {
	if typed, ok := err.(*ConfigError); ok {
		*target = typed
		return true
	}
	return false
}

func runCases(t *testing.T, cases []configCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.run(t) })
	}
}

// wantEqual asserts on one top-level key of a loaded config.
func wantEqual(key string, want any) func(*testing.T, Config) {
	return func(t *testing.T, cfg Config) {
		t.Helper()
		if !reflect.DeepEqual(cfg[key], want) {
			t.Fatalf("cfg[%q] = %#v, want %#v", key, cfg[key], want)
		}
	}
}

// -- happy path, defaults, missing file ---------------------------------------

func TestLoadHappyPath(t *testing.T) {
	runCases(t, []configCase{
		{
			name: "a complete document loads",
			data: map[string]any{
				"source":     []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url":   "https://example.com",
				"version":    "1.0.0",
				"docs":       "stricttools/docs/",
				"output":     "stricttools/.docs-cache/build/",
				"deploy":     map[string]any{"provider": "github-pages"},
				"directives": map[string]any{},
			},
			check: func(t *testing.T, cfg Config) {
				want := []any{map[string]any{"path": "src/", "language": "python"}}
				if !reflect.DeepEqual(cfg["source"], want) {
					t.Fatalf("source = %#v", cfg["source"])
				}
				deploy := cfg["deploy"].(map[string]any)
				if deploy["provider"] != "github-pages" {
					t.Fatalf("deploy.provider = %#v", deploy["provider"])
				}
			},
		},
		{
			name: "optional fields get their defaults when omitted",
			data: baseKeys(map[string]any{
				"source": []any{map[string]any{"path": "pkg/", "language": "go"}},
			}),
			check: func(t *testing.T, cfg Config) {
				if cfg["docs"] != "stricttools/docs/" || cfg["output"] != "stricttools/.docs-cache/build/" {
					t.Fatalf("docs = %#v, output = %#v", cfg["docs"], cfg["output"])
				}
				if cfg["deploy"] != nil {
					t.Fatalf("deploy = %#v, want nil", cfg["deploy"])
				}
				if !reflect.DeepEqual(cfg["directives"], map[string]any{}) {
					t.Fatalf("directives = %#v", cfg["directives"])
				}
			},
		},
	})
}

func TestLoadMissingFileIsNotAProject(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("expected no error for a directory with no selfdoc.json, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected a nil config, got %#v", cfg)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := writeConfigBytes(t, "{bad json")
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("expected a 'not valid JSON' refusal, got %v", err)
	}
}

func TestValidateConfigRejectsANonObjectDocument(t *testing.T) {
	if _, err := ValidateConfig([]any{"source"}); err == nil ||
		!strings.Contains(err.Error(), "selfdoc.json must be a JSON object") {
		t.Fatalf("expected the non-object refusal, got %v", err)
	}
}

// -- required fields and migration refusals -----------------------------------

func TestRequiredFieldsAndMigrations(t *testing.T) {
	runCases(t, []configCase{
		{
			name:  "a document with no source is a codeless project",
			data:  map[string]any{"base_url": "https://example.com"},
			check: wantEqual("source", []any{}),
		},
		{
			name:    "base_url is required",
			data:    map[string]any{"source": []any{map[string]any{"path": "src/", "language": "python"}}},
			wantErr: "missing required field 'base_url'",
		},
		{
			name: "base_url may not be empty",
			data: map[string]any{
				"source":   []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url": "",
			},
			wantErr: "'base_url' must be a non-empty string",
		},
		{
			name:    "source must be a list",
			data:    map[string]any{"source": "src/"},
			wantErr: "'source' must be a list",
		},
		{
			name:  "an explicitly empty source says what omitting it says",
			data:  map[string]any{"source": []any{}, "base_url": "https://example.com"},
			check: wantEqual("source", []any{}),
		},
		{
			name:    "a plain-string source entry names the migration",
			data:    map[string]any{"source": []any{"src/"}, "base_url": "https://example.com"},
			wantErr: "source[0] is a plain string ('src/')",
		},
		{
			name: "a top-level language key names the migration",
			data: map[string]any{
				"language": "python",
				"source":   []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url": "https://example.com",
			},
			wantErr: "Top-level 'language' field is no longer supported",
		},
		{
			name: "a source entry must declare a language",
			data: map[string]any{
				"source":   []any{map[string]any{"path": "src/"}},
				"base_url": "https://example.com",
			},
			wantErr: "'source[0].language' is required",
		},
		{
			name: "a source entry must declare a path",
			data: map[string]any{
				"source":   []any{map[string]any{"language": "python"}},
				"base_url": "https://example.com",
			},
			wantErr: "'source[0].path' is required",
		},
		{
			name: "an unsupported language is the extractor's business, not the loader's",
			data: baseKeys(map[string]any{
				"source": []any{map[string]any{"path": "lib/", "language": "ruby"}},
			}),
			check: func(t *testing.T, cfg Config) {
				entry := cfg["source"].([]any)[0].(map[string]any)
				if entry["language"] != "ruby" {
					t.Fatalf("language = %#v", entry["language"])
				}
			},
		},
		{
			name: "several source entries load in order",
			data: baseKeys(map[string]any{
				"source": []any{
					map[string]any{"path": "src/", "language": "python"},
					map[string]any{"path": "pkg/", "language": "go"},
				},
			}),
			check: func(t *testing.T, cfg Config) {
				entries := cfg["source"].([]any)
				if len(entries) != 2 {
					t.Fatalf("len(source) = %d", len(entries))
				}
				if entries[0].(map[string]any)["language"] != "python" ||
					entries[1].(map[string]any)["language"] != "go" {
					t.Fatalf("source = %#v", entries)
				}
			},
		},
		{
			name: "min_coverage is no longer a key",
			data: map[string]any{
				"source":       []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url":     "https://example.com",
				"min_coverage": 80,
			},
			wantErr: "unknown config key",
		},
		{
			name: "an unknown top-level key names itself",
			data: map[string]any{
				"source":   []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url": "https://example.com",
				"foo":      "bar",
			},
			wantErr: "unknown config key 'foo'",
		},
		{
			name: "declaring name does not weaken unknown-key rejection",
			data: map[string]any{
				"source":   []any{map[string]any{"path": "src/", "language": "python"}},
				"base_url": "https://example.com",
				"name":     "MyProject",
				"bogus":    1,
			},
			wantErr: "bogus",
		},
	})
}

func TestSearchEngineIsRequired(t *testing.T) {
	// Stated without the writer's required-key fill: there is no default,
	// so an undeclared engine is refused.
	dir := writeRawConfig(t, map[string]any{
		"source":   []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url": "https://example.com",
		"version":  "1.0.0",
		"author": map[string]any{
			"name": "Test Author", "url": "https://author.example",
		},
	})
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "search_engine") {
		t.Fatalf("expected the search_engine refusal, got %v", err)
	}
}

func TestAuthorIsRequired(t *testing.T) {
	dir := writeRawConfig(t, map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"version":       "1.0.0",
		"search_engine": "pagefind",
	})
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected the author refusal")
	}
	message := err.Error()
	for _, want := range []string{"author", "name", "url"} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal %q does not name %q", message, want)
		}
	}
}

// -- string fields ------------------------------------------------------------

func TestStringFields(t *testing.T) {
	runCases(t, []configCase{
		{
			name: "lang, author and description round-trip",
			data: baseKeys(map[string]any{"lang": "en", "description": "A great project", "author": map[string]any{"name": "Jane Doe", "url": "https://jane.dev", "same_as": []any{"https://github.com/janedoe"}}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["lang"] != "en" || cfg["description"] != "A great project" {
					t.Fatalf("lang = %#v, description = %#v", cfg["lang"], cfg["description"])
				}
				author := cfg["author"].(map[string]any)
				if author["name"] != "Jane Doe" || author["url"] != "https://jane.dev" {
					t.Fatalf("author = %#v", author)
				}
				if !reflect.DeepEqual(author["same_as"], []any{"https://github.com/janedoe"}) {
					t.Fatalf("same_as = %#v", author["same_as"])
				}
			},
		},
		{
			name: "absent optional strings load as nil",
			data: baseKeys(nil),
			check: func(t *testing.T, cfg Config) {
				for _, key := range []string{"lang", "description", "branch", "search", "version_unused"} {
					if key == "version_unused" {
						continue
					}
					if cfg[key] != nil {
						t.Fatalf("cfg[%q] = %#v, want nil", key, cfg[key])
					}
				}
			},
		},
		{name: "lang may not be empty", data: baseKeys(map[string]any{"lang": ""}), wantErr: "'lang' must be a non-empty string"},
		{name: "description may not be empty", data: baseKeys(map[string]any{"description": ""}), wantErr: "'description' must be a non-empty string"},
		{name: "branch may not be empty", data: baseKeys(map[string]any{"branch": ""}), wantErr: "'branch' must be a non-empty string"},
		{name: "branch must be a string", data: baseKeys(map[string]any{"branch": 42}), wantErr: "'branch' must be a string"},
		{name: "branch loads", data: baseKeys(map[string]any{"branch": "main"}), check: wantEqual("branch", "main")},
		{name: "twitter loads", data: baseKeys(map[string]any{"twitter": "@test"}), check: wantEqual("twitter", "@test")},
		{name: "twitter must start with @", data: baseKeys(map[string]any{"twitter": "test"}), wantErr: "invalid twitter"},
		{name: "an explicit name loads", data: baseKeys(map[string]any{"name": "MyProject"}), check: wantEqual("name", "MyProject")},
		{name: "name is optional", data: baseKeys(nil), check: wantEqual("name", nil)},
		{name: "version loads", data: map[string]any{"source": []any{map[string]any{"path": "src/", "language": "python"}}, "base_url": "https://example.com", "version": "1.2.3"}, check: wantEqual("version", "1.2.3")},
		{name: "version is optional", data: map[string]any{"source": []any{map[string]any{"path": "src/", "language": "python"}}, "base_url": "https://example.com"}, check: wantEqual("version", nil)},
		{name: "version must look like semver", data: baseKeys(map[string]any{"version": "abc"}), wantErr: "invalid version"},
		{name: "version must be a string", data: baseKeys(map[string]any{"version": 123}), wantErr: "'version' must be a string"},
		{name: "base_url loses its trailing slash", data: baseKeys(map[string]any{"base_url": "https://example.com/"}), check: wantEqual("base_url", "https://example.com")},
	})
}

func TestLangBCP47Tags(t *testing.T) {
	for _, tag := range []string{"en", "en-US", "pt-BR", "zh-Hans"} {
		t.Run("accepts "+tag, func(t *testing.T) {
			configCase{data: baseKeys(map[string]any{"lang": tag}), check: wantEqual("lang", tag)}.run(t)
		})
	}
	for _, tag := range []string{"foobar123", "e", "en_US", "123"} {
		t.Run("refuses "+tag, func(t *testing.T) {
			configCase{data: baseKeys(map[string]any{"lang": tag}), wantErr: "invalid lang"}.run(t)
		})
	}
}

func TestSearchField(t *testing.T) {
	for _, value := range []string{"icon", "bar", "hidden"} {
		t.Run("accepts "+value, func(t *testing.T) {
			configCase{data: baseKeys(map[string]any{"search": value}), check: wantEqual("search", value)}.run(t)
		})
	}
	runCases(t, []configCase{
		{name: "absent is nil", data: baseKeys(nil), check: wantEqual("search", nil)},
		{name: "an unknown mode is refused", data: baseKeys(map[string]any{"search": "fullscreen"}), wantErr: "invalid search value"},
		{name: "a non-string mode is refused", data: baseKeys(map[string]any{"search": 42}), wantErr: "invalid search value 42; must be one of: icon, bar, hidden"},
	})
}

func TestSearchEngineField(t *testing.T) {
	runCases(t, []configCase{
		{name: "pagefind is the one valid engine", data: baseKeys(map[string]any{"search_engine": "pagefind"}), check: wantEqual("search_engine", "pagefind")},
		{name: "builtin is not a value a config may carry", data: baseKeys(map[string]any{"search_engine": "builtin"}), wantErr: "invalid search_engine value"},
		{name: "fuse is not a value a config may carry", data: baseKeys(map[string]any{"search_engine": "fuse"}), wantErr: "invalid search_engine value"},
		{name: "minisearch is not a value a config may carry", data: baseKeys(map[string]any{"search_engine": "minisearch"}), wantErr: "invalid search_engine value"},
		{name: "an unknown engine is refused", data: baseKeys(map[string]any{"search_engine": "algolia"}), wantErr: "invalid search_engine value"},
	})
}

// -- the author block ---------------------------------------------------------

func TestAuthorBlock(t *testing.T) {
	runCases(t, []configCase{
		{name: "author must be an object", data: baseKeys(map[string]any{"author": "Jane Doe"}), wantErr: "'author' must be an object"},
		{name: "author needs a name", data: baseKeys(map[string]any{"author": map[string]any{"url": "https://jane.dev"}}), wantErr: "'author.name' is required"},
		{name: "author needs a url", data: baseKeys(map[string]any{"author": map[string]any{"name": "Jane Doe"}}), wantErr: "'author.url' is required"},
		{
			name:    "a site's author is a Person, so there is no type to choose",
			data:    baseKeys(map[string]any{"author": map[string]any{"name": "Jane", "url": "https://jane.dev", "type": "Person"}}),
			wantErr: "invalid author key 'type'",
		},
		{
			name:    "the handle is a meta tag's value, not part of the identity",
			data:    baseKeys(map[string]any{"author": map[string]any{"name": "Test", "url": "https://test.example", "twitter": "@author_handle"}}),
			wantErr: "invalid author key 'twitter'",
		},
		{
			name: "same_as is optional",
			data: baseKeys(map[string]any{"author": map[string]any{"name": "Jane Doe", "url": "https://jane.dev"}}),
			check: wantEqual("author", map[string]any{
				"name": "Jane Doe", "url": "https://jane.dev",
			}),
		},
		{
			name: "same_as loads in declared order",
			data: baseKeys(map[string]any{"author": map[string]any{
				"name": "Jane Doe", "url": "https://jane.example",
				"same_as": []any{"https://github.com/jane", "https://example.org/@jane"},
			}}),
			check: func(t *testing.T, cfg Config) {
				author := cfg["author"].(map[string]any)
				want := []any{"https://github.com/jane", "https://example.org/@jane"}
				if !reflect.DeepEqual(author["same_as"], want) {
					t.Fatalf("same_as = %#v", author["same_as"])
				}
			},
		},
		{
			name: "same_as must hold strings",
			data: baseKeys(map[string]any{"author": map[string]any{
				"name": "Jane Doe", "url": "https://jane.example",
				"same_as": []any{map[string]any{"url": "https://github.com/jane"}},
			}}),
			wantErr: "'author.same_as[0]' must be a string",
		},
	})
}

// -- deploy, feedback, branding ----------------------------------------------

func TestDeployBlock(t *testing.T) {
	runCases(t, []configCase{
		{
			name:    "an unknown provider is refused",
			data:    map[string]any{"source": []any{map[string]any{"path": "src/", "language": "typescript"}}, "base_url": "https://example.com", "deploy": map[string]any{"provider": "netlify"}},
			wantErr: "invalid deploy.provider value 'netlify'; must be one of: cloudflare-pages, github-pages",
		},
		{
			name:    "cloudflare-pages needs a project",
			data:    baseKeys(map[string]any{"deploy": map[string]any{"provider": "cloudflare-pages"}}),
			wantErr: "'deploy.project' is required for cloudflare-pages provider",
		},
		{
			name: "cloudflare-pages with a project loads",
			data: baseKeys(map[string]any{"deploy": map[string]any{"provider": "cloudflare-pages", "project": "my-docs"}}),
			check: func(t *testing.T, cfg Config) {
				deploy := cfg["deploy"].(map[string]any)
				if deploy["provider"] != "cloudflare-pages" || deploy["project"] != "my-docs" {
					t.Fatalf("deploy = %#v", deploy)
				}
			},
		},
	})
}

func TestFeedbackBlock(t *testing.T) {
	runCases(t, []configCase{
		{name: "absent is nil", data: baseKeys(nil), check: wantEqual("feedback", nil)},
		{
			name: "a webhook alone is valid",
			data: baseKeys(map[string]any{"feedback": map[string]any{"webhook": "https://hooks.example.com/fb"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["feedback"].(map[string]any)["webhook"] != "https://hooks.example.com/fb" {
					t.Fatalf("feedback = %#v", cfg["feedback"])
				}
			},
		},
		{
			name: "a ga id alone is valid",
			data: baseKeys(map[string]any{"feedback": map[string]any{"ga": "G-ABCDEF1234"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["feedback"].(map[string]any)["ga"] != "G-ABCDEF1234" {
					t.Fatalf("feedback = %#v", cfg["feedback"])
				}
			},
		},
		{
			name: "both keys are valid",
			data: baseKeys(map[string]any{"feedback": map[string]any{"webhook": "https://hooks.example.com/fb", "ga": "G-ABCDEF1234"}}),
			check: func(t *testing.T, cfg Config) {
				feedback := cfg["feedback"].(map[string]any)
				if feedback["webhook"] == nil || feedback["ga"] == nil {
					t.Fatalf("feedback = %#v", feedback)
				}
			},
		},
		{name: "an empty feedback object is refused", data: baseKeys(map[string]any{"feedback": map[string]any{}}), wantErr: "at least one of 'webhook' or 'ga'"},
		{name: "feedback must be an object", data: baseKeys(map[string]any{"feedback": "yes"}), wantErr: "'feedback' must be an object"},
		{name: "webhook must be a string", data: baseKeys(map[string]any{"feedback": map[string]any{"webhook": 123}}), wantErr: "'feedback.webhook' must be a string"},
		{name: "webhook may not be empty", data: baseKeys(map[string]any{"feedback": map[string]any{"webhook": ""}}), wantErr: "'feedback.webhook' must be a non-empty string"},
		{name: "ga must be a string", data: baseKeys(map[string]any{"feedback": map[string]any{"ga": 42}}), wantErr: "'feedback.ga' must be a string"},
		{name: "ga may not be empty", data: baseKeys(map[string]any{"feedback": map[string]any{"ga": ""}}), wantErr: "'feedback.ga' must be a non-empty string"},
	})
}

func TestBrandingBlock(t *testing.T) {
	runCases(t, []configCase{
		{
			name: "a complete branding block loads",
			data: baseKeys(map[string]any{"branding": map[string]any{
				"tagline":            "Build docs fast",
				"cta_text":           "Get Started",
				"cta_link":           "/quickstart",
				"logo":               "assets/logo.svg",
				"secondary_cta_text": "Learn More",
				"secondary_cta_link": "/guide",
				"features": []any{
					map[string]any{"title": "Fast", "description": "Lightning-fast builds"},
					map[string]any{"title": "Simple", "description": "Zero config needed"},
				},
			}}),
			check: func(t *testing.T, cfg Config) {
				branding := cfg["branding"].(map[string]any)
				if branding["tagline"] != "Build docs fast" || branding["cta_text"] != "Get Started" ||
					branding["logo"] != "assets/logo.svg" {
					t.Fatalf("branding = %#v", branding)
				}
				features := branding["features"].([]any)
				if len(features) != 2 || features[0].(map[string]any)["title"] != "Fast" {
					t.Fatalf("features = %#v", features)
				}
			},
		},
		{
			name: "a tagline alone is valid, and no defaults are injected",
			data: baseKeys(map[string]any{"branding": map[string]any{"tagline": "Docs made easy"}}),
			check: func(t *testing.T, cfg Config) {
				branding := cfg["branding"].(map[string]any)
				if branding["tagline"] != "Docs made easy" {
					t.Fatalf("branding = %#v", branding)
				}
				if _, present := branding["features"]; present {
					t.Fatalf("features was injected: %#v", branding)
				}
			},
		},
		{name: "branding must be an object", data: baseKeys(map[string]any{"branding": "fancy"}), wantErr: "'branding' must be an object"},
		{
			name:    "a feature card needs a title",
			data:    baseKeys(map[string]any{"branding": map[string]any{"features": []any{map[string]any{"description": "No title here"}}}}),
			wantErr: "'branding.features[0].title' is required",
		},
		{
			name:    "a feature card needs a description",
			data:    baseKeys(map[string]any{"branding": map[string]any{"features": []any{map[string]any{"title": "Fast"}}}}),
			wantErr: "'branding.features[0].description' is required",
		},
		{name: "absent is nil", data: baseKeys(nil), check: wantEqual("branding", nil)},
	})
}

// -- booleans and numbers -----------------------------------------------------

func TestBooleanAndNumberFields(t *testing.T) {
	runCases(t, []configCase{
		{name: "glossary defaults to true", data: baseKeys(nil), check: wantEqual("glossary", true)},
		{name: "glossary can be turned off", data: baseKeys(map[string]any{"glossary": false}), check: wantEqual("glossary", false)},
		{name: "feed_max_entries loads", data: baseKeys(map[string]any{"feed_max_entries": 10}), check: wantEqual("feed_max_entries", int64(10))},
		{name: "feed_max_entries is optional", data: baseKeys(nil), check: wantEqual("feed_max_entries", nil)},
		{
			name:    "feed_max_entries must be at least one",
			data:    baseKeys(map[string]any{"feed_max_entries": 0}),
			wantErr: "'feed_max_entries' must be an integer between 1 and None",
		},
		{name: "coverage_threshold defaults to 1.0", data: baseKeys(nil), check: wantEqual("coverage_threshold", 1.0)},
		{name: "coverage_threshold accepts a fraction", data: baseKeys(map[string]any{"coverage_threshold": 0.7}), check: wantEqual("coverage_threshold", 0.7)},
		{name: "coverage_threshold accepts integer zero", data: baseKeys(map[string]any{"coverage_threshold": 0}), check: wantEqual("coverage_threshold", 0.0)},
		{name: "coverage_threshold accepts integer one", data: baseKeys(map[string]any{"coverage_threshold": 1}), check: wantEqual("coverage_threshold", 1.0)},
		{
			name:    "coverage_threshold above one is refused",
			data:    baseKeys(map[string]any{"coverage_threshold": 1.5}),
			wantErr: "'coverage_threshold' must be a number between 0.0 and 1.0",
		},
		{
			name:    "a negative coverage_threshold is refused",
			data:    baseKeys(map[string]any{"coverage_threshold": -0.1}),
			wantErr: "must be a number between",
		},
		{name: "coverage_threshold must be a number", data: baseKeys(map[string]any{"coverage_threshold": "high"}), wantErr: "must be a number"},
		{name: "a boolean is not a number", data: baseKeys(map[string]any{"coverage_threshold": true}), wantErr: "must be a number"},
	})
}

// -- gen, gen_data ------------------------------------------------------------

func TestGenAndGenData(t *testing.T) {
	runCases(t, []configCase{
		{
			name: "gen_data passes through",
			data: baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{
				map[string]any{"command": "python gen.py", "output": "data/api.json", "mounts": []any{"/src"}},
			}}}),
			check: func(t *testing.T, cfg Config) {
				scripts := cfg["gen_data"].(map[string]any)["scripts"].([]any)
				if scripts[0].(map[string]any)["command"] != "python gen.py" {
					t.Fatalf("scripts = %#v", scripts)
				}
			},
		},
		{name: "gen_data is optional", data: baseKeys(nil), check: wantEqual("gen_data", nil)},
		{name: "gen_data must be an object", data: baseKeys(map[string]any{"gen_data": "bad"}), wantErr: "'gen_data' must be an object"},
		{name: "gen_data.scripts must be a list", data: baseKeys(map[string]any{"gen_data": map[string]any{"scripts": "bad"}}), wantErr: "'gen_data.scripts' must be a list"},
		{
			name:    "a script needs a command",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"output": "out.json", "mounts": []any{"/src"}}}}}),
			wantErr: "'gen_data.scripts[0].command' is required",
		},
		{
			name:    "a script needs an output",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": "echo hi", "mounts": []any{"/src"}}}}}),
			wantErr: "'gen_data.scripts[0].output' is required",
		},
		{
			name:    "a script needs mounts",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": "echo hi", "output": "out.json"}}}}),
			wantErr: "'gen_data.scripts[0].mounts' is required",
		},
		{
			name:    "a command must be a string",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": 42, "output": "out.json", "mounts": []any{"/src"}}}}}),
			wantErr: "'gen_data.scripts[0].command' must be a string",
		},
		{
			name:    "mounts must be a list",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": "echo hi", "output": "out.json", "mounts": "/src"}}}}),
			wantErr: "'gen_data.scripts[0].mounts' must be a list",
		},
		{
			name:    "a mount must be a string",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": "echo hi", "output": "out.json", "mounts": []any{123}}}}}),
			wantErr: "'gen_data.scripts[0].mounts[0]' must be a string",
		},
		{
			name:    "an unknown gen_data key is refused",
			data:    baseKeys(map[string]any{"gen_data": map[string]any{"scripts": []any{}, "timeout": 30}}),
			wantErr: "invalid gen_data key 'timeout'",
		},
		{
			name: "gen.exclude loads",
			data: baseKeys(map[string]any{"gen": map[string]any{"exclude": []any{"test_*.py", "__pycache__"}}}),
			check: func(t *testing.T, cfg Config) {
				exclude := cfg["gen"].(map[string]any)["exclude"]
				if !reflect.DeepEqual(exclude, []any{"test_*.py", "__pycache__"}) {
					t.Fatalf("exclude = %#v", exclude)
				}
			},
		},
		{name: "gen is optional", data: baseKeys(nil), check: wantEqual("gen", nil)},
		{name: "gen must be an object", data: baseKeys(map[string]any{"gen": "bad"}), wantErr: "'gen' must be an object"},
		{name: "gen.exclude must be a list", data: baseKeys(map[string]any{"gen": map[string]any{"exclude": "*.pyc"}}), wantErr: "'gen.exclude' must be a list"},
		{name: "a gen.exclude item must be a string", data: baseKeys(map[string]any{"gen": map[string]any{"exclude": []any{42}}}), wantErr: "'gen.exclude[0]' must be a string"},
		{name: "an unknown gen key is refused", data: baseKeys(map[string]any{"gen": map[string]any{"exclude": []any{}, "bogus": true}}), wantErr: "invalid gen key 'bogus'"},
	})
}

// -- root_files, redirects, examples -----------------------------------------

func TestListAndMappingFields(t *testing.T) {
	runCases(t, []configCase{
		{name: "root_files loads", data: baseKeys(map[string]any{"root_files": []any{"docs/_CLAUDE.md"}}), check: wantEqual("root_files", []any{"docs/_CLAUDE.md"})},
		{name: "a root_files item must be a string", data: baseKeys(map[string]any{"root_files": []any{123}}), wantErr: "'root_files[0]' must be a string"},
		{
			name: "redirects load",
			data: baseKeys(map[string]any{"redirects": []any{
				map[string]any{"from": "edit-release", "to": "release/edit"},
				map[string]any{"from": "old-page", "to": "new-page"},
			}}),
			check: func(t *testing.T, cfg Config) {
				redirects := cfg["redirects"].([]any)
				first := redirects[0].(map[string]any)
				if len(redirects) != 2 || first["from"] != "edit-release" || first["to"] != "release/edit" {
					t.Fatalf("redirects = %#v", redirects)
				}
			},
		},
		{name: "an empty redirects list is accepted", data: baseKeys(map[string]any{"redirects": []any{}}), check: wantEqual("redirects", []any{})},
		{name: "absent redirects default to an empty list", data: baseKeys(nil), check: wantEqual("redirects", []any{})},
		{name: "a redirect needs a from", data: baseKeys(map[string]any{"redirects": []any{map[string]any{"to": "new-page"}}}), wantErr: "'redirects[0].from' is required"},
		{name: "a redirect needs a to", data: baseKeys(map[string]any{"redirects": []any{map[string]any{"from": "old-page"}}}), wantErr: "'redirects[0].to' is required"},
		{
			name:    "an unknown redirect key is refused",
			data:    baseKeys(map[string]any{"redirects": []any{map[string]any{"from": "a", "to": "b", "bogus": true}}}),
			wantErr: "invalid <item> key 'bogus'",
		},
		{name: "examples is optional", data: baseKeys(nil), check: wantEqual("examples", nil)},
		{
			name: "a language to command-template mapping loads verbatim",
			data: baseKeys(map[string]any{"examples": map[string]any{
				"python": "uv run --directory python python {file}",
				"go":     "scripts/validate-example-go.sh {file}",
			}}),
			check: wantEqual("examples", map[string]any{
				"python": "uv run --directory python python {file}",
				"go":     "scripts/validate-example-go.sh {file}",
			}),
		},
		{
			name:    "a template without the file placeholder is refused",
			data:    baseKeys(map[string]any{"examples": map[string]any{"python": "python -c pass"}}),
			wantErr: "invalid examples.python 'python -c pass'; must contain '{file}'",
		},
		{
			name:    "a non-string template is refused",
			data:    baseKeys(map[string]any{"examples": map[string]any{"python": []any{"python", "{file}"}}}),
			wantErr: "'examples.python' must be a string",
		},
		{
			name:    "an empty template is refused",
			data:    baseKeys(map[string]any{"examples": map[string]any{"python": ""}}),
			wantErr: "'examples.python' must be a non-empty string",
		},
		{
			name:    "a malformed language key is refused",
			data:    baseKeys(map[string]any{"examples": map[string]any{"Python 3!": "python {file}"}}),
			wantErr: "invalid examples key 'Python 3!'",
		},
		{name: "examples must be an object", data: baseKeys(map[string]any{"examples": []any{"python"}}), wantErr: "'examples' must be an object"},
	})
}

// -- versions, locales, unified ----------------------------------------------

func TestVersionsLocalesUnified(t *testing.T) {
	runCases(t, []configCase{
		{
			name: "all three are optional",
			data: baseKeys(nil),
			check: func(t *testing.T, cfg Config) {
				for _, key := range []string{"versions", "locales", "unified"} {
					if cfg[key] != nil {
						t.Fatalf("cfg[%q] = %#v, want nil", key, cfg[key])
					}
				}
			},
		},
		{
			name: "versions load, with per-project overrides",
			data: baseKeys(map[string]any{"versions": []any{
				map[string]any{"version": "1.0"},
				map[string]any{"version": "2.0", "projects": map[string]any{"core": "2.0.1"}},
			}}),
			check: func(t *testing.T, cfg Config) {
				versions := cfg["versions"].([]any)
				if len(versions) != 2 || versions[0].(map[string]any)["version"] != "1.0" {
					t.Fatalf("versions = %#v", versions)
				}
				projects := versions[1].(map[string]any)["projects"].(map[string]any)
				if projects["core"] != "2.0.1" {
					t.Fatalf("projects = %#v", projects)
				}
			},
		},
		{
			name:    "duplicate version strings are refused",
			data:    baseKeys(map[string]any{"versions": []any{map[string]any{"version": "1.0"}, map[string]any{"version": "1.0"}}}),
			wantErr: "duplicate version string '1.0' in 'versions'",
		},
		{
			name:    "the retired per-version indexed flag is refused",
			data:    baseKeys(map[string]any{"versions": []any{map[string]any{"version": "1.0", "indexed": true}}}),
			wantErr: "indexed",
		},
		{
			name:    "a version entry needs a version",
			data:    baseKeys(map[string]any{"versions": []any{map[string]any{"projects": map[string]any{"core": "1.0"}}}}),
			wantErr: "'versions[0].version' is required",
		},
		{
			name:    "an unknown version-entry key is refused",
			data:    baseKeys(map[string]any{"versions": []any{map[string]any{"version": "1.0", "bogus": "val"}}}),
			wantErr: "invalid <item> key 'bogus'",
		},
		{
			name: "locales load with their flags",
			data: baseKeys(map[string]any{"locales": []any{
				map[string]any{"code": "en", "label": "English", "default": true},
				map[string]any{"code": "fa", "label": "Farsi", "rtl": true},
			}}),
			check: func(t *testing.T, cfg Config) {
				locales := cfg["locales"].([]any)
				if len(locales) != 2 {
					t.Fatalf("len(locales) = %d", len(locales))
				}
				first := locales[0].(map[string]any)
				if first["code"] != "en" || first["default"] != true {
					t.Fatalf("locales[0] = %#v", first)
				}
				if locales[1].(map[string]any)["rtl"] != true {
					t.Fatalf("locales[1] = %#v", locales[1])
				}
			},
		},
		{
			name:    "duplicate locale codes are refused",
			data:    baseKeys(map[string]any{"locales": []any{map[string]any{"code": "en", "label": "English"}, map[string]any{"code": "en", "label": "English (US)"}}}),
			wantErr: "duplicate locale code 'en' in 'locales'",
		},
		{
			name:    "two default locales are refused",
			data:    baseKeys(map[string]any{"locales": []any{map[string]any{"code": "en", "label": "English", "default": true}, map[string]any{"code": "fr", "label": "French", "default": true}}}),
			wantErr: "at most one locale may have 'default: true', found 2",
		},
		{name: "a locale needs a code", data: baseKeys(map[string]any{"locales": []any{map[string]any{"label": "English"}}}), wantErr: "'locales[0].code' is required"},
		{name: "a locale needs a label", data: baseKeys(map[string]any{"locales": []any{map[string]any{"code": "en"}}}), wantErr: "'locales[0].label' is required"},
		{
			name:    "a malformed locale code is refused",
			data:    baseKeys(map[string]any{"locales": []any{map[string]any{"code": "en_US", "label": "English US"}}}),
			wantErr: "invalid locales[0].code",
		},
		{
			name: "unified projects load",
			data: baseKeys(map[string]any{"unified": map[string]any{
				"projects": []any{
					map[string]any{"path": "packages/core", "slug": "core", "nav_title": "Core"},
					map[string]any{"path": "packages/cli"},
				},
				"exclude": []any{"*.test.*"},
			}}),
			check: func(t *testing.T, cfg Config) {
				unified := cfg["unified"].(map[string]any)
				projects := unified["projects"].([]any)
				if len(projects) != 2 || projects[0].(map[string]any)["slug"] != "core" {
					t.Fatalf("projects = %#v", projects)
				}
				if !reflect.DeepEqual(unified["exclude"], []any{"*.test.*"}) {
					t.Fatalf("exclude = %#v", unified["exclude"])
				}
			},
		},
		{
			name: "an absent slug derives from the path basename",
			data: baseKeys(map[string]any{"unified": map[string]any{"projects": []any{
				map[string]any{"path": "packages/core"},
				map[string]any{"path": "packages/cli"},
			}}}),
			check: func(t *testing.T, cfg Config) {
				projects := cfg["unified"].(map[string]any)["projects"].([]any)
				if len(projects) != 2 {
					t.Fatalf("projects = %#v", projects)
				}
			},
		},
		{
			name: "duplicate explicit slugs are refused",
			data: baseKeys(map[string]any{"unified": map[string]any{"projects": []any{
				map[string]any{"path": "a", "slug": "same"},
				map[string]any{"path": "b", "slug": "same"},
			}}}),
			wantErr: "duplicate project slug 'same' in 'unified.projects'",
		},
		{
			name: "duplicate derived slugs are refused",
			data: baseKeys(map[string]any{"unified": map[string]any{"projects": []any{
				map[string]any{"path": "org1/core"},
				map[string]any{"path": "org2/core"},
			}}}),
			wantErr: "duplicate project slug 'core' in 'unified.projects'",
		},
		{
			name:    "a unified project needs a path",
			data:    baseKeys(map[string]any{"unified": map[string]any{"projects": []any{map[string]any{"slug": "core"}}}}),
			wantErr: "'unified.projects[0].path' is required",
		},
		{
			name:    "an unknown unified key is refused",
			data:    baseKeys(map[string]any{"unified": map[string]any{"projects": []any{map[string]any{"path": "a"}}, "bogus": true}}),
			wantErr: "invalid unified key 'bogus'",
		},
		{
			name:    "an unknown unified-project key is refused",
			data:    baseKeys(map[string]any{"unified": map[string]any{"projects": []any{map[string]any{"path": "a", "bogus": true}}}}),
			wantErr: "invalid <item> key 'bogus'",
		},
		{
			name:    "unified.projects must not be empty",
			data:    baseKeys(map[string]any{"unified": map[string]any{"projects": []any{}}}),
			wantErr: "'unified.projects' must be a non-empty list",
		},
	})
}

func TestLocaleCodeSubtags(t *testing.T) {
	for _, code := range []string{"zh-Hans", "pt-BR", "zh-Hans-CN"} {
		t.Run("accepts "+code, func(t *testing.T) {
			configCase{
				data: baseKeys(map[string]any{"locales": []any{map[string]any{"code": code, "label": "A locale"}}}),
				check: func(t *testing.T, cfg Config) {
					if cfg["locales"].([]any)[0].(map[string]any)["code"] != code {
						t.Fatalf("code = %#v", cfg["locales"])
					}
				},
			}.run(t)
		})
	}
}

// -- unversioned --------------------------------------------------------------

func TestUnversioned(t *testing.T) {
	runCases(t, []configCase{
		{
			name:  "an unversioned project gets one anonymous version",
			data:  map[string]any{"base_url": "https://example.com", "unversioned": true},
			check: wantEqual("versions", []any{map[string]any{"version": ""}}),
		},
		{
			name:    "unversioned and versions contradict each other",
			data:    map[string]any{"base_url": "https://example.com", "unversioned": true, "versions": []any{map[string]any{"version": "1.0.0"}}},
			wantErr: "contradict each other",
		},
		{
			name:    "unversioned is refused for a project that declares source",
			data:    map[string]any{"base_url": "https://example.com", "unversioned": true, "source": []any{map[string]any{"path": "src/", "language": "python"}}},
			wantErr: "'unversioned': true is refused for a project that declares",
		},
	})
}

// -- posts, topology, assembly ------------------------------------------------

func TestPostsTopologyAssembly(t *testing.T) {
	base := func(extra map[string]any) map[string]any {
		data := map[string]any{
			"source":   []any{map[string]any{"path": "src/", "language": "python"}},
			"base_url": "https://example.com",
		}
		for k, v := range extra {
			data[k] = v
		}
		return data
	}
	runCases(t, []configCase{
		{
			name: "posts.dir loads",
			data: base(map[string]any{"posts": map[string]any{"dir": "stricttools/posts/"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["posts"].(map[string]any)["dir"] != "stricttools/posts/" {
					t.Fatalf("posts = %#v", cfg["posts"])
				}
			},
		},
		{name: "posts.dir must be a string", data: base(map[string]any{"posts": map[string]any{"dir": 123}}), wantErr: "'posts.dir' must be a string"},
		{
			name: "an empty posts object validates and injects nothing",
			data: base(map[string]any{"posts": map[string]any{}}),
			check: func(t *testing.T, cfg Config) {
				posts, ok := cfg["posts"].(map[string]any)
				if !ok || len(posts) != 0 {
					t.Fatalf("posts = %#v", cfg["posts"])
				}
			},
		},
		{name: "posts is optional", data: base(nil), check: wantEqual("posts", nil)},
		{
			name: "assembly.repo loads",
			data: base(map[string]any{"assembly": map[string]any{"repo": "smm-h/docs-assembly"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["assembly"].(map[string]any)["repo"] != "smm-h/docs-assembly" {
					t.Fatalf("assembly = %#v", cfg["assembly"])
				}
			},
		},
		{name: "assembly is optional", data: base(nil), check: wantEqual("assembly", nil)},
		{name: "topology is optional", data: base(nil), check: wantEqual("topology", nil)},
		{
			name: "an empty topology object validates",
			data: base(map[string]any{"topology": map[string]any{}}),
			check: func(t *testing.T, cfg Config) {
				if _, ok := cfg["topology"].(map[string]any); !ok {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
		{
			name: "topology.slug loads",
			data: base(map[string]any{"topology": map[string]any{"slug": "selfdoc"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["topology"].(map[string]any)["slug"] != "selfdoc" {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
		{name: "topology.slug must be a string", data: base(map[string]any{"topology": map[string]any{"slug": 123}}), wantErr: "'topology.slug' must be a string"},
		{
			name: "topology.docs_base loads",
			data: base(map[string]any{"topology": map[string]any{"docs_base": "https://docs.smmh.dev"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["topology"].(map[string]any)["docs_base"] != "https://docs.smmh.dev" {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
		{
			name: "topology.docs_base loses its trailing slash",
			data: base(map[string]any{"topology": map[string]any{"docs_base": "https://docs.smmh.dev/"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["topology"].(map[string]any)["docs_base"] != "https://docs.smmh.dev" {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
		{name: "topology.docs_base must be a string", data: base(map[string]any{"topology": map[string]any{"docs_base": 42}}), wantErr: "'topology.docs_base' must be a string"},
		{
			name: "topology.posts_base loses its trailing slash",
			data: base(map[string]any{"topology": map[string]any{"posts_base": "https://docs.smmh.dev/blog/"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["topology"].(map[string]any)["posts_base"] != "https://docs.smmh.dev/blog" {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
		{
			name:    "topology.assembly is retired in favour of assembly.repo",
			data:    base(map[string]any{"topology": map[string]any{"assembly": "smm-h/docs-assembly"}}),
			wantErr: "topology.assembly",
		},
		{
			name:    "the retirement names the replacement",
			data:    base(map[string]any{"topology": map[string]any{"assembly": "smm-h/docs-assembly"}}),
			wantErr: `"repo"`,
		},
		{
			name:    "topology.legacy_blog_host is retired with the redirect worker",
			data:    base(map[string]any{"topology": map[string]any{"legacy_blog_host": "blog.smmh.dev"}}),
			wantErr: "'topology.legacy_blog_host' is no longer supported",
		},
		{
			name:    "the retirement sends the reader to the DNS zone",
			data:    base(map[string]any{"topology": map[string]any{"legacy_blog_host": "blog.smmh.dev"}}),
			wantErr: "redirect rule on the DNS zone",
		},
		{
			name: "topology.projects is retired: a slug is the whole address",
			data: base(map[string]any{"topology": map[string]any{"projects": map[string]any{
				"rlsbl": "https://docs.smmh.dev/rlsbl",
			}}}),
			wantErr: "'topology.projects' is no longer supported",
		},
		{
			name: "the retirement says what a cross-project link resolves to",
			data: base(map[string]any{"topology": map[string]any{"projects": map[string]any{
				"rlsbl": "https://docs.smmh.dev/rlsbl",
			}}}),
			wantErr: "'<topology.docs_base>/<slug>/'",
		},
		{
			name: "a full topology block loads",
			data: base(map[string]any{"topology": map[string]any{
				"slug":       "selfdoc",
				"docs_base":  "https://docs.smmh.dev",
				"posts_base": "https://docs.smmh.dev/blog",
			}}),
			check: func(t *testing.T, cfg Config) {
				topology := cfg["topology"].(map[string]any)
				if topology["slug"] != "selfdoc" || topology["docs_base"] != "https://docs.smmh.dev" ||
					topology["posts_base"] != "https://docs.smmh.dev/blog" {
					t.Fatalf("topology = %#v", topology)
				}
			},
		},
		{
			name: "topology is not strict, so an extra key is accepted",
			data: base(map[string]any{"topology": map[string]any{"slug": "selfdoc", "custom_field": "allowed"}}),
			check: func(t *testing.T, cfg Config) {
				if cfg["topology"].(map[string]any)["custom_field"] != "allowed" {
					t.Fatalf("topology = %#v", cfg["topology"])
				}
			},
		},
	})
}

// -- lint_ignore --------------------------------------------------------------

// installStubLintRegistry binds a lint-code check with the severities the
// real registry carries for the codes these cases name, and removes it again
// when the test ends. The registry itself belongs to the lints package; what
// is asserted here is that a config refuses what the check refuses.
func installStubLintRegistry(t *testing.T) {
	t.Helper()
	severities := map[string]string{
		"SEO007":  "warning",
		"SEO008":  "warning",
		"LINK001": "error",
	}
	previous := LintCodeValidator
	LintCodeValidator = func(codes []string, source string) error {
		var unknown, errorSeverity []string
		for _, code := range codes {
			severity, known := severities[code]
			if !known {
				unknown = append(unknown, code)
			} else if severity == "error" {
				errorSeverity = append(errorSeverity, code+" (severity: error)")
			}
		}
		if len(unknown) > 0 {
			known := make([]string, 0, len(severities))
			for code := range severities {
				known = append(known, code)
			}
			sort.Strings(known)
			return fmt.Errorf("%s names lint code(s) the registry does not carry: %s. "+
				"Every suppressible code is declared in the lint registry; known codes are: %s.",
				source, strings.Join(unknown, ", "), strings.Join(known, ", "))
		}
		if len(errorSeverity) > 0 {
			return fmt.Errorf("%s names error-severity lint code(s), which cannot be "+
				"suppressed: %s", source, strings.Join(errorSeverity, ", "))
		}
		return nil
	}
	t.Cleanup(func() { LintCodeValidator = previous })
}

func TestLintIgnore(t *testing.T) {
	installStubLintRegistry(t)
	runCases(t, []configCase{
		{
			name:  "a list of registered codes loads unchanged",
			data:  baseKeys(map[string]any{"lint_ignore": []any{"SEO007", "SEO008"}}),
			check: wantEqual("lint_ignore", []any{"SEO007", "SEO008"}),
		},
		{
			name:    "an unregistered code is a hard error",
			data:    baseKeys(map[string]any{"lint_ignore": []any{"SEO007", "SEO0O8"}}),
			wantErr: "SEO0O8",
		},
		{
			name:    "the unregistered-code refusal names the source",
			data:    baseKeys(map[string]any{"lint_ignore": []any{"SEO007", "SEO0O8"}}),
			wantErr: "lint_ignore",
		},
		{
			name:    "an error-severity code cannot be silenced",
			data:    baseKeys(map[string]any{"lint_ignore": []any{"SEO007", "LINK001"}}),
			wantErr: "LINK001",
		},
		{
			name:    "the unsuppressable refusal names the severity",
			data:    baseKeys(map[string]any{"lint_ignore": []any{"SEO007", "LINK001"}}),
			wantErr: "error",
		},
		{
			name:    "a malformed code is refused by the item pattern before the registry",
			data:    baseKeys(map[string]any{"lint_ignore": []any{"seo007"}}),
			wantErr: "invalid lint_ignore[0] 'seo007'; must match pattern",
		},
	})
}

// -- explicit nulls -----------------------------------------------------------

func TestExplicitNulls(t *testing.T) {
	runCases(t, []configCase{
		{
			name:  "an explicit null on a defaulted string resolves to the default",
			data:  baseKeys(map[string]any{"docs": nil}),
			check: wantEqual("docs", "stricttools/docs/"),
		},
		{
			name:  "an explicit null on a list field resolves to its empty default",
			data:  baseKeys(map[string]any{"redirects": nil}),
			check: wantEqual("redirects", []any{}),
		},
		{
			name:    "an explicit null on a required field is refused",
			raw:     map[string]any{"source": []any{}, "base_url": nil, "search_engine": "pagefind", "author": map[string]any{"name": "A", "url": "https://a.example"}},
			wantErr: "missing required field 'base_url'",
		},
	})
}

// -- the exported schema walk -------------------------------------------------

func TestSchemaWalkIsWhatTheDocsTablesRead(t *testing.T) {
	byName := map[string]FieldSpec{}
	for _, spec := range Schema {
		if _, duplicate := byName[spec.Name]; duplicate {
			t.Fatalf("the schema declares %q twice", spec.Name)
		}
		byName[spec.Name] = spec
	}
	for _, name := range []string{"source", "base_url", "search_engine", "author", "versions", "locales", "unified", "topology", "assembly", "posts"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("the schema does not declare %q", name)
		}
	}
	if !byName["base_url"].Required || !byName["search_engine"].Required || !byName["author"].Required {
		t.Fatal("base_url, search_engine and author are the required fields")
	}
	if byName["source"].Required {
		t.Fatal("source is optional: a codeless project declares none")
	}
	for _, spec := range Schema {
		if spec.Description == "" {
			t.Fatalf("%q carries no description, so the generated table would have a blank cell", spec.Name)
		}
		if spec.Internal {
			t.Fatalf("%q is marked internal; no top-level field is", spec.Name)
		}
	}
}

// TestEverySchemaPatternTranslates compiles every regular expression the
// schema declares, at every depth, so an untranslatable one is a failing
// test rather than a panic inside a user's load.
func TestEverySchemaPatternTranslates(t *testing.T) {
	var walk func(spec *FieldSpec, path string)
	walk = func(spec *FieldSpec, path string) {
		for _, pattern := range []string{spec.Pattern, spec.KeyPattern} {
			if pattern == "" {
				continue
			}
			if _, err := pythonPatternToRE2(pattern); err != nil {
				t.Fatalf("%s declares pattern %q, which does not translate: %v", path, pattern, err)
			}
			matchPattern(pattern, "probe")
		}
		for i := range spec.Children {
			walk(&spec.Children[i], path+"."+spec.Children[i].Name)
		}
		if spec.ItemSpec != nil {
			walk(spec.ItemSpec, path+"[]")
		}
	}
	for i := range Schema {
		walk(&Schema[i], Schema[i].Name)
	}
}

// TestDiagnosticsMatchThePythonSurface pins the full text of the refusals
// this loader emits. Each want string below is the output the Python
// implementation produced for the same document, so a reworded diagnostic
// has to be a deliberate edit here rather than a drift nobody notices.
func TestDiagnosticsMatchThePythonSurface(t *testing.T) {
	cases := []struct {
		data map[string]any
		want string
	}{
		{map[string]any{"search": 42}, "invalid search value 42; must be one of: icon, bar, hidden"},
		{map[string]any{"feed_max_entries": 0}, "'feed_max_entries' must be an integer between 1 and None"},
		{map[string]any{"coverage_threshold": 1.5}, "'coverage_threshold' must be a number between 0.0 and 1.0"},
		{map[string]any{"coverage_threshold": "high"}, "'coverage_threshold' must be a number between 0.0 and 1.0"},
		{map[string]any{"coverage_threshold": true}, "'coverage_threshold' must be a number between 0.0 and 1.0"},
		{map[string]any{"deploy": map[string]any{"provider": "netlify"}}, "invalid deploy.provider value 'netlify'; must be one of: cloudflare-pages, github-pages"},
		{map[string]any{"examples": map[string]any{"python": "python -c pass"}}, "invalid examples.python 'python -c pass'; must contain '{file}'"},
		{map[string]any{"examples": map[string]any{"Python 3!": "python {file}"}}, `invalid examples key 'Python 3!'; must match pattern ^[a-z][a-z0-9+#._-]*$`},
		{map[string]any{"lint_ignore": []any{"seo007"}}, `invalid lint_ignore[0] 'seo007'; must match pattern ^[A-Z]+\d+$`},
		{map[string]any{"redirects": []any{map[string]any{"from": "a", "to": "b", "bogus": true}}}, "invalid <item> key 'bogus'; must be one of: from, to"},
		{map[string]any{"author": map[string]any{"name": "Jane", "url": "https://j.dev", "type": "Person"}}, "invalid author key 'type'; must be one of: name, same_as, url"},
		{map[string]any{"author": map[string]any{"name": "J", "url": "https://j.dev", "same_as": []any{map[string]any{"url": "x"}}}}, "'author.same_as[0]' must be a string"},
		{map[string]any{"unified": map[string]any{"projects": []any{}}}, "'unified.projects' must be a non-empty list"},
		{map[string]any{"locales": []any{map[string]any{"code": "en_US", "label": "x"}}}, `invalid locales[0].code 'en_US'; must match pattern ^[a-z]{2,3}(-[A-Z][a-z]{3})?(-[A-Z]{2})?$`},
		{map[string]any{"gen_data": map[string]any{"scripts": []any{map[string]any{"command": "echo hi", "output": "o", "mounts": []any{123}}}}}, "'gen_data.scripts[0].mounts[0]' must be a string"},
		{map[string]any{"source": []any{"src/"}}, `source[0] is a plain string ('src/'). Source entries must be objects with 'path' and 'language': {"path": "src/", "language": "python"}`},
		{map[string]any{"root_files": []any{123}}, "'root_files[0]' must be a string"},
		{map[string]any{"posts": map[string]any{"dir": 123}}, "'posts.dir' must be a string"},
		{map[string]any{"versions": []any{map[string]any{"version": "1.0", "indexed": true}}}, "invalid <item> key 'indexed'; must be one of: projects, version"},
		{map[string]any{"foo": "bar"}, "unknown config key 'foo'"},
		{map[string]any{"lint_ignore": []any{"SEO007", "SEO0O8"}}, `invalid lint_ignore[1] 'SEO0O8'; must match pattern ^[A-Z]+\d+$`},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			dir := writeConfig(t, baseKeys(tc.data))
			_, err := Load(dir)
			if err == nil {
				t.Fatalf("expected %q, got a loaded config", tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("diagnostic\n got: %s\nwant: %s", err.Error(), tc.want)
			}
		})
	}
}
