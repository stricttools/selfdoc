// Package config loads and validates a project's selfdoc.json.
//
// The whole schema is one declarative table, [Schema], and one recursive
// validator walks it. A loaded config is a plain map keyed by every
// top-level field name in [Schema] -- present unconditionally, carrying the
// field's default when the document omitted it -- so every consumer reads a
// key rather than asking whether it exists.
//
// # Value shapes
//
// A raw document is decoded the way Python's json module decodes it, because
// the validator's type rules were written against those shapes: a JSON
// object is a map[string]any, an array is a []any, an integer literal is an
// int64, a number carrying a fraction or an exponent is a float64, a string
// is a string, true/false is a bool, and null is a nil any. [ValidateConfig]
// accepts a document assembled in memory under the same rules, and
// additionally tolerates a plain int where an int64 would appear.
//
// # Nested values are validated in place
//
// The validator rewrites nested lists and objects as it descends -- a
// transform's result, a coerced float -- and because Go maps and slices are
// reference types the caller's own document sees those rewrites, exactly as
// the Python did. A caller that needs its raw document untouched copies it
// first.
package config

import "github.com/stricttools/selfdoc/internal/layout"

// FieldType is the value shape a [FieldSpec] accepts.
type FieldType string

// The field types the validator knows. The string values are the ones the
// Python surface's FieldType enum carried, so a diagnostic that prints a
// type prints the same word.
const (
	// FieldStr accepts a JSON string.
	FieldStr FieldType = "str"
	// FieldBool accepts a JSON boolean.
	FieldBool FieldType = "bool"
	// FieldInt accepts a JSON integer literal, never a boolean.
	FieldInt FieldType = "int"
	// FieldFloat accepts any JSON number, coercing an integer to a float.
	FieldFloat FieldType = "float"
	// FieldDict accepts a JSON object.
	FieldDict FieldType = "dict"
	// FieldList accepts a JSON array.
	FieldList FieldType = "list"
)

// FieldSpec is the declaration of a single configuration field: its type,
// whether it is required, what it defaults to, and every constraint the
// validator applies to it.
//
// One spec describes one field, and a spec's children (for an object) or
// ItemSpec (for an array, or for an object with an open key set) describe
// what sits inside it, so the whole schema is one value rather than a set of
// validation functions.
type FieldSpec struct {
	// Name is the key this field carries in the document. For a list
	// element or an open-key-set value the Python surface used the
	// placeholders "<item>" and "<language>", which appear in the
	// unknown-key and malformed-key diagnostics, so they are kept.
	Name string
	// Type is the value shape this field accepts.
	Type FieldType
	// Required refuses an absent field.
	Required bool
	// Default is the value an absent optional field resolves to. It is a
	// nil any for the fields whose absence is itself the information.
	Default any
	// DefaultFactory builds the value an absent optional field resolves
	// to, for the fields that default to an empty collection: every call
	// returns a fresh one, so two loaded configs never share a slice or a
	// map. It wins over Default when both are set.
	DefaultFactory func() any
	// Choices is the closed value set a string field may carry.
	Choices []string
	// Pattern is the regular expression a string field must match,
	// anchored at the start only -- the semantics of Python's re.match,
	// which this schema was written against. The spelling stored here is
	// the Python one, because a rejection prints it; it is translated to
	// its RE2 equivalent before matching.
	Pattern string
	// MustContain is a literal substring a string field must contain.
	// Says what a regex would say, but the rejection message names the
	// missing text instead of printing a pattern the reader has to decode.
	MustContain string
	// Description is the field's one-line documentation, rendered by the
	// table-config-schema directive and the config-schema docs page.
	Description string
	// AllowEmpty permits an empty string on a string field.
	//
	// This is the inverse of the Python surface's non_empty, so that the
	// Go zero value carries the Python default (non_empty=True, an empty
	// string refused). A Python spec written non_empty=False is spelled
	// AllowEmpty: true here.
	AllowEmpty bool
	// MinVal and MaxVal bound a numeric field. Each is nil, an int64 or a
	// float64; a rejection renders them the way Python renders the same
	// value, including "None" for an absent bound.
	MinVal any
	MaxVal any
	// MinLength is the shortest a list may be. A nil pointer means
	// unbounded.
	MinLength *int
	// Children is the closed key set of an object field. Each child is
	// validated under "<parent path>.<child name>", and a child declared
	// required whose key is absent is refused.
	Children []FieldSpec
	// ItemSpec is the spec every element of a list is validated against.
	// For a childless object (an open key set) it is the spec every VALUE
	// is validated against -- the object analogue of the list case.
	ItemSpec *FieldSpec
	// StrictKeys refuses a key of an object field that no child declares.
	StrictKeys bool
	// KeyPattern is the regular expression every KEY of a childless
	// object must match. A closed Children set cannot express an
	// open-but-well-formed key set (fence language names, for instance),
	// so those keys are constrained by shape. Same anchoring and
	// translation as Pattern.
	KeyPattern string
	// Transform rewrites a validated string value. It runs last, after
	// every constraint has passed.
	Transform func(any) any
	// Internal hides the field from the generated configuration tables:
	// it is a runtime key rather than something an author writes.
	Internal bool
}

// VALIDDeployProviders is the closed set of hosting providers a deploy block
// may name.
var VALIDDeployProviders = []string{"cloudflare-pages", "github-pages"}

// VALIDSearchEngines is the closed set of engines that may answer a site's
// search UI. One member today, and the list is still the enumeration a
// config is checked against: the key is the extension point, so a second
// engine is a member added here rather than a new mechanism.
var VALIDSearchEngines = []string{"pagefind"}

func ptrInt(v int) *int { return &v }

// stripTrailingSlashes is the transform every base-URL-shaped field carries,
// so a declared address with a trailing slash and one without produce the
// same links.
func stripTrailingSlashes(v any) any {
	s := v.(string)
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func emptyList() any { return []any{} }

func emptyDict() any { return map[string]any{} }

// Schema declares every top-level field of selfdoc.json, in the order a
// loaded config's keys are resolved. It is the single authority: the
// unknown-key refusal, the resolved config's key set, and the generated
// configuration tables all read it.
//
// Treat it as immutable. It is a package-level slice only so the generated
// documentation can walk it.
var Schema = []FieldSpec{
	// --- required fields ---
	// 'source' is optional: a codeless project (a portfolio or personal
	// site that is nothing but markdown pages) has no code to extract
	// from and declares no source entries. Absent normalizes to an empty
	// list rather than nil so every consumer can iterate it
	// unconditionally. Directives that need source code refuse instead of
	// rendering a placeholder note.
	{
		Name:           "source",
		Type:           FieldList,
		Required:       false,
		AllowEmpty:     true,
		DefaultFactory: emptyList,
		ItemSpec: &FieldSpec{
			Name:       "<item>",
			Type:       FieldDict,
			StrictKeys: true,
			Children: []FieldSpec{
				{
					Name:        "path",
					Type:        FieldStr,
					Required:    true,
					Description: "Source directory or file path.",
				},
				{
					Name:        "language",
					Type:        FieldStr,
					Required:    true,
					Description: "Programming language for this source entry.",
				},
			},
			Description: "Source entry with path and language.",
		},
		Description: "List of source entries to extract documentation from.",
	},
	{
		Name:        "base_url",
		Type:        FieldStr,
		Required:    true,
		Transform:   stripTrailingSlashes,
		Description: "Base URL of the generated site, used for canonical links and SEO.",
	},
	// --- optional string fields ---
	{
		Name:        "version",
		Type:        FieldStr,
		Required:    false,
		Default:     nil,
		Pattern:     `^\d+\.\d+\.\d+`,
		Description: "Project version. When present, used by deploy instead of reading from the project manifest (VERSION, pyproject.toml or package.json).",
	},
	{
		Name:        "docs",
		Type:        FieldStr,
		Default:     layout.DocsDefault,
		Description: "Directory containing the handwritten Markdown documentation templates. It lives inside the tool-state directory, and a value outside it is refused as the layout selfdoc used before.",
	},
	{
		Name:        "output",
		Type:        FieldStr,
		Default:     layout.OutputDefault,
		Description: "Output directory for generated HTML files. It lives inside the tool-state directory, in the uncommitted cache.",
	},
	// Absent keeps the convention every standalone repo relies on: the
	// project root's CHANGELOG.md becomes the changelog page. That
	// convention reads "the root changelog is this project's changelog",
	// which is false in a workspace whose root file rolls up several
	// independently versioned projects -- and nothing in the build can
	// tell which of them a given site documents. So the answer is
	// declared, not guessed: name the file, and a name that does not
	// exist is an error rather than a silently missing page.
	{
		Name:        "changelog",
		Type:        FieldStr,
		Required:    false,
		Default:     nil,
		Description: "Path to the changelog document published as the site's changelog page, relative to the project root. Absent means the project root's CHANGELOG.md is used if it exists; declare it when that file is not this site's changelog.",
	},
	{
		Name:    "theme",
		Type:    FieldStr,
		Default: "minimal",
		Description: "Visual theme for the generated site. One of the themes selfdoc " +
			"ships -- 'minimal', 'clean' or 'tinymoon'. A build's --theme " +
			"flag overrides this for that build only, without writing " +
			"anything back here.",
	},
	{
		Name:        "repo",
		Type:        FieldStr,
		Default:     nil,
		Description: "GitHub repository URL shown in the site header.",
	},
	{
		Name:        "lang",
		Type:        FieldStr,
		Default:     nil,
		Pattern:     `^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`,
		Description: "BCP 47 language tag for the site content (e.g. 'en', 'pt-BR').",
	},
	{
		Name:    "name",
		Type:    FieldStr,
		Default: nil,
		Description: "Explicit project name. Used as the single source of truth for " +
			"the manifest name and the auto-generated API reference index " +
			"description. When absent, the name is derived heuristically " +
			"(single-source basename or project directory basename).",
	},
	{
		Name:        "description",
		Type:        FieldStr,
		Default:     nil,
		Description: "Short description of the project, used in meta tags and SEO.",
	},
	{
		Name:        "branch",
		Type:        FieldStr,
		Default:     nil,
		Description: "Git branch used for source links in the generated site.",
	},
	{
		Name:        "search",
		Type:        FieldStr,
		Default:     nil,
		Choices:     []string{"icon", "bar", "hidden"},
		Description: "Search UI mode: icon button, full bar, or hidden.",
	},
	{
		Name:     "search_engine",
		Type:     FieldStr,
		Required: true,
		Choices:  VALIDSearchEngines,
		Description: "Search engine that answers this site's search UI. Required and " +
			"never inferred: every site builds a search UI, so the engine " +
			"behind it is declared, not defaulted.",
	},
	{
		Name:        "code_icons",
		Type:        FieldStr,
		Default:     "colorful",
		Choices:     []string{"colorful", "monochrome", "none"},
		Description: "Style of language icons shown on code blocks.",
	},
	// --- optional boolean fields ---
	{
		Name:        "line_numbers",
		Type:        FieldBool,
		Default:     false,
		Description: "Show line numbers in code blocks.",
	},
	{
		Name:        "run_button",
		Type:        FieldBool,
		Default:     false,
		Description: "Show a run button on code blocks for supported languages.",
	},
	{
		Name:        "page_nav",
		Type:        FieldBool,
		Default:     true,
		Description: "Show previous/next navigation links between pages.",
	},
	{
		Name:        "page_progress",
		Type:        FieldBool,
		Default:     true,
		Description: "Show a reading progress bar at the top of each page.",
	},
	{
		Name:        "glossary",
		Type:        FieldBool,
		Default:     true,
		Description: "Auto-generate a glossary page from dfn terms.",
	},
	// --- optional float field ---
	{
		Name:    "coverage_threshold",
		Type:    FieldFloat,
		Default: 1.0,
		MinVal:  0.0,
		MaxVal:  1.0,
		Description: "Minimum fraction of public symbols that must be documented " +
			"for selfdoc check to pass (0.0-1.0). Default 1.0 requires " +
			"100% coverage.",
	},
	// --- optional int field ---
	{
		Name:        "feed_max_entries",
		Type:        FieldInt,
		Default:     nil,
		MinVal:      int64(1),
		Description: "Maximum number of entries in the Atom feed, sorted by most recent.",
	},
	// --- optional list fields ---
	{
		Name:           "lint_ignore",
		Type:           FieldList,
		DefaultFactory: emptyList,
		AllowEmpty:     true,
		ItemSpec: &FieldSpec{
			Name:        "<item>",
			Type:        FieldStr,
			Pattern:     `^[A-Z]+\d+$`,
			Description: "Warning-severity lint rule ID to ignore (e.g. SEO007, SEO008, XREF001).",
		},
		Description: "List of warning-severity lint rule IDs to suppress (e.g. 'SEO007', 'SEO008'). Error-severity codes cannot be suppressed and are refused at load.",
	},
	{
		Name:           "root_files",
		Type:           FieldList,
		DefaultFactory: emptyList,
		AllowEmpty:     true,
		ItemSpec: &FieldSpec{
			Name:        "<item>",
			Type:        FieldStr,
			Description: "Underscore-prefixed template path in docs/.",
		},
		Description: "List of underscore-prefixed template paths in docs/ for root file generation.",
	},
	{
		Name:           "redirects",
		Type:           FieldList,
		Required:       false,
		DefaultFactory: emptyList,
		AllowEmpty:     true,
		ItemSpec: &FieldSpec{
			Name:       "<item>",
			Type:       FieldDict,
			StrictKeys: true,
			Children: []FieldSpec{
				{
					Name:        "from",
					Type:        FieldStr,
					Required:    true,
					Description: "Old page slug to redirect from.",
				},
				{
					Name:        "to",
					Type:        FieldStr,
					Required:    true,
					Description: "New page slug to redirect to.",
				},
			},
			Description: "Redirect entry mapping old slug to new slug.",
		},
		Description: "Page-level redirects expanded across all locale/version combos.",
	},
	// --- optional dict fields ---
	{
		Name:       "deploy",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: false,
		Children: []FieldSpec{
			{
				Name:        "provider",
				Type:        FieldStr,
				Required:    true,
				Choices:     []string{"cloudflare-pages", "github-pages"},
				Description: "Hosting provider for deployment.",
			},
			{
				Name:        "project",
				Type:        FieldStr,
				Required:    false,
				Description: "Project name on the hosting provider (required for cloudflare-pages).",
			},
		},
		Description: "Deployment configuration for publishing the generated site.",
	},
	{
		Name:           "directives",
		Type:           FieldDict,
		DefaultFactory: emptyDict,
		StrictKeys:     false,
		Description:    "Custom directive mappings from directive name to source file path.",
	},
	{
		Name:    "examples",
		Type:    FieldDict,
		Default: nil,
		// Keys are fenced-block language names, an open set (any language
		// may appear after a fence), so they are constrained by shape
		// rather than by a closed Children set: a malformed key is
		// rejected, an unanticipated-but-well-formed language is not.
		KeyPattern: `^[a-z][a-z0-9+#._-]*$`,
		ItemSpec: &FieldSpec{
			Name:        "<language>",
			Type:        FieldStr,
			MustContain: "{file}",
			Description: "Validator command template; '{file}' is replaced with the" +
				" path of the assembled snippet.",
		},
		Description: "Validator command templates keyed by code-block language, used by" +
			" 'selfdoc check' to execute fenced blocks marked 'validate'." +
			" Each template must contain the '{file}' placeholder. Absent" +
			" means example validation is off.",
	},
	// The one identity a site's structured data names. Required, because
	// every page carries structured data and an author is one of the facts
	// it states: an absent block used to mint an Organization named after
	// the project directory, which stated a legal entity nobody had
	// declared. There is no schema.org type to choose -- a site has one
	// author and the emitters render it as a Person.
	{
		Name:       "author",
		Type:       FieldDict,
		Required:   true,
		StrictKeys: true,
		Children: []FieldSpec{
			{
				Name:        "name",
				Type:        FieldStr,
				Required:    true,
				Description: "The author's display name, as every page's structured data states it.",
			},
			{
				Name:        "url",
				Type:        FieldStr,
				Required:    true,
				Description: "The author's canonical URL -- the address that identifies them.",
			},
			{
				Name:       "same_as",
				Type:       FieldList,
				Required:   false,
				AllowEmpty: true,
				ItemSpec: &FieldSpec{
					Name:        "<item>",
					Type:        FieldStr,
					Description: "An external URL naming the same author (a profile, a directory entry).",
				},
				Description: "External identity URLs, emitted as the Person's sameAs.",
			},
		},
		Description: "The site's author: one Person, named in every page's structured" +
			" data. Required -- there is no inferred author.",
	},
	{
		Name:        "twitter",
		Type:        FieldStr,
		Default:     nil,
		Pattern:     `^@`,
		Description: "Twitter/X handle (starts with @) for the twitter:site meta tag.",
	},
	{
		Name:       "feedback",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: false,
		Children: []FieldSpec{
			{
				Name:        "webhook",
				Type:        FieldStr,
				Required:    false,
				Description: "Webhook URL for collecting user feedback.",
			},
			{
				Name:        "ga",
				Type:        FieldStr,
				Required:    false,
				Description: "Google Analytics measurement ID.",
			},
		},
		Description: "Feedback collection configuration (at least one of webhook or ga required).",
	},
	{
		Name:       "branding",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: false,
		Children: []FieldSpec{
			{
				Name:        "tagline",
				Type:        FieldStr,
				Required:    false,
				Description: "Short tagline displayed on the landing page.",
			},
			{
				Name:        "cta_text",
				Type:        FieldStr,
				Required:    false,
				Description: "Primary call-to-action button text.",
			},
			{
				Name:        "cta_link",
				Type:        FieldStr,
				Required:    false,
				Description: "Primary call-to-action button URL.",
			},
			{
				Name:        "logo",
				Type:        FieldStr,
				Required:    false,
				Description: "Path to a logo image file.",
			},
			{
				Name:        "secondary_cta_text",
				Type:        FieldStr,
				Required:    false,
				Description: "Secondary call-to-action button text.",
			},
			{
				Name:        "secondary_cta_link",
				Type:        FieldStr,
				Required:    false,
				Description: "Secondary call-to-action button URL.",
			},
			{
				Name:       "features",
				Type:       FieldList,
				Required:   false,
				AllowEmpty: true,
				ItemSpec: &FieldSpec{
					Name: "<item>",
					Type: FieldDict,
					Children: []FieldSpec{
						{
							Name:        "title",
							Type:        FieldStr,
							Required:    true,
							Description: "Feature card title.",
						},
						{
							Name:        "description",
							Type:        FieldStr,
							Required:    true,
							Description: "Feature card description.",
						},
					},
					Description: "Feature card object.",
				},
				Description: "List of feature cards shown on the landing page.",
			},
		},
		Description: "Landing page branding and call-to-action configuration.",
	},
	{
		Name:       "auto_detect",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: true,
		Children: []FieldSpec{
			{
				Name:        "steps",
				Type:        FieldBool,
				Required:    false,
				Description: "Auto-detect step guide blocks in documentation.",
			},
			{
				Name:        "api_entries",
				Type:        FieldBool,
				Required:    false,
				Description: "Auto-detect API entry cards in documentation.",
			},
		},
		Description: "Automatic content detection settings for step guides and API entries.",
	},
	{
		Name:       "gen",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: true,
		Children: []FieldSpec{
			{
				Name:       "exclude",
				Type:       FieldList,
				Required:   false,
				AllowEmpty: true,
				ItemSpec: &FieldSpec{
					Name:        "<item>",
					Type:        FieldStr,
					Description: "Glob pattern to exclude from generation.",
				},
				Description: "List of glob patterns to exclude from doc generation.",
			},
		},
		Description: "Configuration for the gen command.",
	},
	{
		Name:       "gen_data",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: true,
		Children: []FieldSpec{
			{
				Name:       "scripts",
				Type:       FieldList,
				Required:   false,
				AllowEmpty: true,
				ItemSpec: &FieldSpec{
					Name: "<item>",
					Type: FieldDict,
					Children: []FieldSpec{
						{
							Name:        "command",
							Type:        FieldStr,
							Required:    true,
							Description: "Shell command to execute for data generation.",
						},
						{
							Name:        "output",
							Type:        FieldStr,
							Required:    true,
							Description: "Output file path relative to docs/ for generated data.",
						},
						{
							Name:       "mounts",
							Type:       FieldList,
							Required:   true,
							AllowEmpty: true,
							ItemSpec: &FieldSpec{
								Name:        "<item>",
								Type:        FieldStr,
								Description: "File path to mount into the script environment.",
							},
							Description: "List of files to mount into the script environment.",
						},
					},
					Description: "Data generation script configuration.",
				},
				Description: "List of data generation scripts to run before build.",
			},
		},
		Description: "Configuration for the gen-data command.",
	},
	{
		Name:        "schema_types",
		Type:        FieldDict,
		Default:     nil,
		StrictKeys:  false,
		Description: "Mapping from page type to schema.org @type (e.g. guide -> TechArticle).",
	},
	// --- multi-version / multi-locale / unified fields ---
	{
		Name:       "versions",
		Type:       FieldList,
		Default:    nil,
		AllowEmpty: true,
		ItemSpec: &FieldSpec{
			Name:       "<item>",
			Type:       FieldDict,
			StrictKeys: true,
			Children: []FieldSpec{
				{
					Name:        "version",
					Type:        FieldStr,
					Required:    true,
					Description: "Version string (e.g. '1.0', 'latest').",
				},
				{
					Name:        "projects",
					Type:        FieldDict,
					Required:    false,
					StrictKeys:  false,
					Description: "Per-project version overrides (project name -> version string).",
				},
			},
			Description: "Version entry.",
		},
		Description: "List of documentation versions to build.",
	},
	{
		Name:    "unversioned",
		Type:    FieldBool,
		Default: nil,
		Description: "Declares that this project has no public version -- a personal " +
			"site or portfolio that publishes no artifact. It replaces the " +
			"'versions' array (declaring both is an error) and is refused " +
			"for a project that declares 'source', because code is the " +
			"thing that gets released and therefore carries a version. An " +
			"unversioned project's pages show no version badge, offer no " +
			"version search filter and no version picker.",
	},
	{
		Name:       "locales",
		Type:       FieldList,
		Default:    nil,
		AllowEmpty: true,
		ItemSpec: &FieldSpec{
			Name:       "<item>",
			Type:       FieldDict,
			StrictKeys: false,
			Children: []FieldSpec{
				{
					Name:        "code",
					Type:        FieldStr,
					Required:    true,
					Pattern:     `^[a-z]{2,3}(-[A-Z][a-z]{3})?(-[A-Z]{2})?$`,
					Description: "BCP 47 locale code (e.g. 'en', 'pt-BR', 'zh-Hans-CN').",
				},
				{
					Name:        "label",
					Type:        FieldStr,
					Required:    true,
					Description: "Human-readable locale label (e.g. 'English').",
				},
				{
					Name:        "default",
					Type:        FieldBool,
					Required:    false,
					Description: "Whether this locale is the default.",
				},
				{
					Name:        "rtl",
					Type:        FieldBool,
					Required:    false,
					Description: "Whether this locale uses right-to-left text direction.",
				},
			},
			Description: "Locale entry.",
		},
		Description: "List of locales for multi-language documentation.",
	},
	{
		Name:       "unified",
		Type:       FieldDict,
		Default:    nil,
		StrictKeys: true,
		Children: []FieldSpec{
			{
				Name:      "projects",
				Type:      FieldList,
				Required:  true,
				MinLength: ptrInt(1),
				ItemSpec: &FieldSpec{
					Name:       "<item>",
					Type:       FieldDict,
					StrictKeys: true,
					Children: []FieldSpec{
						{
							Name:        "path",
							Type:        FieldStr,
							Required:    true,
							Description: "Path to the project directory.",
						},
						{
							Name:        "slug",
							Type:        FieldStr,
							Required:    false,
							Description: "URL slug for the project (defaults to path basename).",
						},
						{
							Name:        "nav_title",
							Type:        FieldStr,
							Required:    false,
							Description: "Display title in the navigation.",
						},
					},
					Description: "Unified project entry.",
				},
				Description: "List of projects to unify into a single documentation site.",
			},
			{
				Name:       "exclude",
				Type:       FieldList,
				Required:   false,
				AllowEmpty: true,
				ItemSpec: &FieldSpec{
					Name:        "<item>",
					Type:        FieldStr,
					Description: "Glob pattern to exclude from unified build.",
				},
				Description: "Glob patterns to exclude from the unified build.",
			},
		},
		Description: "Configuration for unified multi-project documentation.",
	},
	{
		Name:        "posts",
		Type:        FieldDict,
		Description: "Blog post configuration.",
		Children: []FieldSpec{
			{
				Name: "dir", Type: FieldStr, Default: layout.PostsDefault,
				Description: "Directory containing post markdown files.",
			},
			{
				Name: "repo", Type: FieldStr,
				Description: "GitHub repository for archiving resolved post content (e.g., owner/posts).",
			},
		},
	},
	{
		Name:        "topology",
		Type:        FieldDict,
		Description: "Deployment topology for multi-project unified sites.",
		Children: []FieldSpec{
			{
				Name: "slug", Type: FieldStr, Required: false,
				Description: "This project's URL path segment.",
			},
			{
				Name: "docs_base", Type: FieldStr, Required: false,
				Transform:   stripTrailingSlashes,
				Description: "Base URL for docs (e.g., 'https://docs.smmh.dev').",
			},
			{
				Name: "posts_base", Type: FieldStr, Required: false,
				Transform:   stripTrailingSlashes,
				Description: "Canonical base URL under which blog posts and the unified blog index are served. This is a path on the docs site, not a separate host (e.g., 'https://docs.smmh.dev/blog').",
			},
		},
	},
	{
		Name:        "assembly",
		Type:        FieldDict,
		Description: "Assembly configuration for unified site deployment.",
		Children: []FieldSpec{
			{
				Name: "repo", Type: FieldStr,
				Description: "GitHub repository for the assembly (e.g., owner/repo).",
			},
			{
				Name: "pages_project", Type: FieldStr,
				Description: "Cloudflare Pages project the assembled site deploys to. Used by both 'assembly init' (project creation) and the generated deploy workflow, so the two can never diverge. No default.",
			},
		},
	},
}
