// Tests for the ownership predicate: which descriptions selfdoc may
// overwrite. The property under test throughout is the inverse -- handwritten
// text is never classified machine-owned -- because a false positive there
// silently destroys someone's prose on the next gen.
package ownership

import (
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/strictclisupport"
	"github.com/stricttools/selfdoc/internal/util"
)

// moduleTemplate is the current module template instantiated for the module
// the frontmatter below titles.
var moduleTemplate = instantiate(ModuleDescTemplate, "mylib.config")

// owned runs IsMachineOwned and fails the test on an error.
func owned(t *testing.T, relPath string, frontmatter util.Frontmatter, seedHash string,
	structure *strictclisupport.Structure) bool {
	t.Helper()
	result, err := IsMachineOwned(relPath, frontmatter, seedHash, structure)
	if err != nil {
		t.Fatalf("classifying %s: %v", relPath, err)
	}
	return result
}

// -- The dispatch by page kind ----------------------------------------------

func TestIsMachineOwnedModulePages(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter util.Frontmatter
		seedHash    string
		want        bool
	}{
		{
			name: "the current template on a generated page is owned",
			frontmatter: util.Frontmatter{
				"generated": true, "seeded": true,
				"title": "mylib.config", "description": moduleTemplate,
			},
			want: true,
		},
		{
			name: "the historical template is still recognized as machine text",
			frontmatter: util.Frontmatter{
				"generated": true, "title": "mylib.config",
				"description": "Documentation for mylib.config",
			},
			want: true,
		},
		{
			name: "a non-generated page is never owned, template text or not",
			frontmatter: util.Frontmatter{
				"generated": false, "seeded": true,
				"title": "mylib.config", "description": moduleTemplate,
			},
		},
		{
			name: "a page with no generated key is never owned",
			frontmatter: util.Frontmatter{
				"title":       "mylib.config",
				"description": "Some custom description that is quite detailed.",
			},
		},
		{
			// The critical inverse: handwritten text on a generated, seeded
			// page with no matching seed hash is not owned.
			name: "handwritten text is not owned even on a seeded page",
			frontmatter: util.Frontmatter{
				"generated": true, "seeded": true, "title": "mylib.config",
				"description": "A carefully hand-authored account of the module.",
			},
		},
		{
			name: "the generated key must be a boolean, not the string \"true\"",
			frontmatter: util.Frontmatter{
				"generated": "true", "seeded": true,
				"title": "mylib.config", "description": moduleTemplate,
			},
		},
		{
			// Nothing to classify, so the frontmatter's own skeleton signal
			// answers.
			name:        "a generated and seeded page with no description is owned",
			frontmatter: util.Frontmatter{"generated": true, "seeded": true},
			want:        true,
		},
		{
			name:        "a generated page with no description and no seeded flag is not owned",
			frontmatter: util.Frontmatter{"generated": true},
		},
		{
			name: "text matching the recorded seed hash is owned",
			frontmatter: util.Frontmatter{
				"generated": true, "seeded": true, "title": "mylib.config",
				"description": "A one-line docstring summary that was machine-seeded.",
			},
			seedHash: DescriptionSeedHash("A one-line docstring summary that was machine-seeded."),
			want:     true,
		},
		{
			name: "the template for a DIFFERENT module is not owned",
			frontmatter: util.Frontmatter{
				"generated": true, "title": "mylib.other",
				"description": moduleTemplate,
			},
		},
		{
			name: "the template with no title to instantiate it is not owned",
			frontmatter: util.Frontmatter{
				"generated": true, "description": moduleTemplate,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := owned(t, "mylib-config.md", test.frontmatter, test.seedHash, nil)
			if got != test.want {
				t.Errorf("machine-owned = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsMachineOwnedGeneratedIndex(t *testing.T) {
	tests := []struct {
		name        string
		description string
		seedHash    string
		want        bool
	}{
		{
			name:        "the current index format is owned",
			description: "API reference index for mylib covering 4 modules",
			want:        true,
		},
		{
			name:        "the format without a project name is owned",
			description: "API reference index covering 1 module",
			want:        true,
		},
		{
			// Machine residue from an older selfdoc: recognized so the next
			// gen reseeds it.
			name: "a legacy machine phrase is owned",
			description: "Complete auto-generated API reference index — browse all " +
				"modules, classes, and functions with their signatures and " +
				"docstrings.",
			want: true,
		},
		{
			name:        "the shortest legacy phrase is owned",
			description: "Auto-generated API reference index",
			want:        true,
		},
		{
			name:        "a handwritten index description is not owned",
			description: "Everything this library exposes, grouped by what it is for.",
		},
		{
			name:        "a near-miss of the current format is not owned",
			description: "API reference index for mylib covering many modules",
		},
		{
			name:        "handwritten text matching a recorded seed hash is owned",
			description: "Everything this library exposes, grouped by what it is for.",
			seedHash:    DescriptionSeedHash("Everything this library exposes, grouped by what it is for."),
			want:        true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontmatter := util.Frontmatter{
				"generated": true, "title": "API Reference",
				"description": test.description,
			}
			got := owned(t, "gen-index.md", frontmatter, test.seedHash, nil)
			if got != test.want {
				t.Errorf("machine-owned = %v, want %v", got, test.want)
			}
		})
	}
}

// TestLegacyIndexDescriptionsAreEachRecognized pins the whole legacy set: gen
// reads it to decide what it may reseed, so a member that stopped being
// recognized would freeze that text on every page carrying it.
func TestLegacyIndexDescriptionsAreEachRecognized(t *testing.T) {
	for _, legacy := range LegacyIndexDescriptions {
		if !IsMachineOwnedIndexDescription(legacy, "") {
			t.Errorf("the legacy description %q is not recognized as machine text", legacy)
		}
	}
}

func TestIsMachineOwnedCLIPages(t *testing.T) {
	const buildHelp = "Build the documentation site from the templates in docs/ " +
		"and write it to the output directory."
	structure := &strictclisupport.Structure{
		AppName:  "mytool",
		Commands: []*strictclisupport.Object{commandEntry("build", buildHelp)},
		Groups:   []*strictclisupport.Object{commandEntry("post", "Manage blog posts")},
	}

	tests := []struct {
		name        string
		relPath     string
		description string
		seedHash    string
		structure   *strictclisupport.Structure
		want        bool
	}{
		{
			name:        "the index's long-form default is owned",
			relPath:     "cli-index.md",
			description: "Complete CLI reference for mytool — all available commands, subcommands, flags, arguments, and usage examples with detailed descriptions.",
			structure:   structure,
			want:        true,
		},
		{
			name:        "a handwritten index description is not owned",
			relPath:     "cli-index.md",
			description: "Every command this tool answers, with the flags each one takes.",
			structure:   structure,
		},
		{
			name:        "a command page carrying its help's first sentence is owned",
			relPath:     "cli-build.md",
			description: "Build the documentation site from the templates in docs/ and write it to the output directory.",
			structure:   structure,
			want:        true,
		},
		{
			name:        "a handwritten command description is not owned",
			relPath:     "cli-build.md",
			description: "What the build does, and the three things that make it fail.",
			structure:   structure,
		},
		{
			name:        "a group page carrying the long-form default is owned",
			relPath:     "cli-post.md",
			description: "Reference for the mytool post command group — subcommands, flags, arguments, and usage details for the post group in the mytool CLI.",
			structure:   structure,
			want:        true,
		},
		{
			// No schema entry to recompute against, so only the recorded seed
			// hash can answer -- and here nothing is recorded.
			name:        "a CLI page the schema does not name is not owned",
			relPath:     "cli-vanished.md",
			description: "Reference for a command that no longer exists.",
			structure:   structure,
		},
		{
			name:        "a CLI page the schema does not name is owned when its seed hash matches",
			relPath:     "cli-vanished.md",
			description: "Reference for a command that no longer exists.",
			seedHash:    DescriptionSeedHash("Reference for a command that no longer exists."),
			structure:   structure,
			want:        true,
		},
		{
			name:        "with no schema at all, only the seed hash answers",
			relPath:     "cli-build.md",
			description: "Build the documentation site from the templates in docs/ and write it to the output directory.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontmatter := util.Frontmatter{
				"generated": true, "description": test.description,
			}
			got := owned(t, test.relPath, frontmatter, test.seedHash, test.structure)
			if got != test.want {
				t.Errorf("machine-owned = %v, want %v", got, test.want)
			}
		})
	}
}

// commandEntry builds one command or group entry in the shape a dumped
// schema carries it.
func commandEntry(name, help string) *strictclisupport.Object {
	entry := extractors.NewJSONObject()
	entry.Set("name", name)
	entry.Set("help", help)
	return entry
}

// TestALocalePrefixedPathIsClassifiedByItsBasename covers the keying the
// callers use: the store and the docs map are locale-prefixed, and the page
// kind is decided by the filename at the end of that path.
func TestALocalePrefixedPathIsClassifiedByItsBasename(t *testing.T) {
	frontmatter := util.Frontmatter{
		"generated":   true,
		"title":       "API Reference",
		"description": "API reference index for mylib covering 4 modules",
	}
	if !owned(t, "en/gen-index.md", frontmatter, "", nil) {
		t.Error("a locale-prefixed generated index was not classified as the index")
	}
}

// -- NormalizeDescription ---------------------------------------------------

func TestNormalizeDescription(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "an absent value", value: nil},
		{name: "surrounding whitespace is stripped", value: "  A page.  ", want: "A page."},
		{name: "one layer of double quotes is stripped", value: `"A page."`, want: "A page."},
		{name: "one layer of single quotes is stripped", value: "'A page.'", want: "A page."},
		{name: "mismatched quotes are left alone", value: `"A page.'`, want: `"A page.'`},
		{name: "an inner quote is left alone", value: `A "quoted" page.`, want: `A "quoted" page.`},
		{name: "only one layer is stripped", value: `""A page.""`, want: `"A page."`},
		{name: "a bare boolean renders as Python renders it", value: true, want: "True"},
		{name: "a bare number renders as its digits", value: int64(42), want: "42"},
		{name: "the empty string", value: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeDescription(test.value); got != test.want {
				t.Errorf("normalized to %q, want %q", got, test.want)
			}
		})
	}
}

func TestDescriptionSeedHash(t *testing.T) {
	// The hash covers the NORMALIZED text, so a quoted and an unquoted
	// spelling of one description are the same seed.
	if DescriptionSeedHash(`"Core module."`) != DescriptionSeedHash("Core module.") {
		t.Error("quoting changed the seed hash")
	}
	if DescriptionSeedHash("Core module.") == DescriptionSeedHash("Core modules.") {
		t.Error("two different descriptions hashed the same")
	}
	if got := len(DescriptionSeedHash("Core module.")); got != 64 {
		t.Errorf("digest is %d characters, want a 64-character SHA-256 hex digest", got)
	}
}

// -- The per-kind predicates on their own -----------------------------------

func TestIsMachineOwnedModuleDescription(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		moduleName string
		seedHash   string
		want       bool
	}{
		{
			name: "the current template", value: moduleTemplate,
			moduleName: "mylib.config", want: true,
		},
		{
			name: "the historical template", value: "Documentation for mylib.config",
			moduleName: "mylib.config", want: true,
		},
		{
			name: "a quoted current template", value: `"` + moduleTemplate + `"`,
			moduleName: "mylib.config", want: true,
		},
		{
			name: "an empty description", value: "   ", moduleName: "mylib.config",
		},
		{
			name: "handwritten prose", value: "The configuration loader, and what it refuses.",
			moduleName: "mylib.config",
		},
		{
			name:       "handwritten prose with a matching seed hash",
			value:      "The configuration loader, and what it refuses.",
			moduleName: "mylib.config",
			seedHash:   DescriptionSeedHash("The configuration loader, and what it refuses."),
			want:       true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsMachineOwnedModuleDescription(test.value, test.moduleName, test.seedHash)
			if got != test.want {
				t.Errorf("machine-owned = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsMachineOwnedCLIDescriptionRecomputesFromTheSchema(t *testing.T) {
	// A help text long enough that a prior selfdoc's truncation of it is
	// recognizable as machine text on its length alone -- the family a static
	// set of templates could never cover.
	const help = "Publish the built documentation to the assembly without a " +
		"release: build the docs locally, push the built site, its manifest " +
		"and its membership record into the assembly repository, then " +
		"dispatch the shared-only workflow."
	truncated := help[:120]

	got, err := IsMachineOwnedCLIDescription(
		truncated, strictclisupport.KindCommand, "publish", "mytool", help, "")
	if err != nil {
		t.Fatalf("classifying a truncated default: %v", err)
	}
	if !got {
		t.Errorf("the truncated default %q was not recognized", truncated)
	}

	withEllipsis := help[:120] + "..."
	got, err = IsMachineOwnedCLIDescription(
		withEllipsis, strictclisupport.KindCommand, "publish", "mytool", help, "")
	if err != nil {
		t.Fatalf("classifying a truncated default with an ellipsis: %v", err)
	}
	if !got {
		t.Errorf("the truncated default %q was not recognized", withEllipsis)
	}

	handwritten := "What publishing does to the assembly, and when not to."
	got, err = IsMachineOwnedCLIDescription(
		handwritten, strictclisupport.KindCommand, "publish", "mytool", help, "")
	if err != nil {
		t.Fatalf("classifying handwritten text: %v", err)
	}
	if got {
		t.Errorf("handwritten text %q was classified machine-owned", handwritten)
	}
}
