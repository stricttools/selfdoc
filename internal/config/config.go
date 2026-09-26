package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// Config is a loaded selfdoc.json: every top-level name in [Schema] is a
// key, carrying either the document's value or the field's default.
type Config = map[string]any

// ConfigError is returned when selfdoc.json is present but invalid.
type ConfigError struct {
	// Message is the diagnostic, which names the offending field path and,
	// where a remedy exists, the declaration to write instead.
	Message string
}

func (e *ConfigError) Error() string { return e.Message }

func configErrorf(format string, args ...any) *ConfigError {
	return &ConfigError{Message: fmt.Sprintf(format, args...)}
}

// LintCodeValidator polices a config's "lint_ignore" list: it is called with
// the declared codes and the name of the source that declared them, and any
// error it returns becomes a [ConfigError].
//
// It is a seam rather than a direct call because the lint-code registry is
// the lints package's to own, and this package must stay loadable without
// it. The command layer installs the registry-backed check at startup; while
// it is nil the list's codes are accepted as written, which is why installing
// it is part of wiring the binary and not optional.
var LintCodeValidator func(codes []string, source string) error

// missingSentinel marks "no value present in the document", which is
// distinct from an explicit JSON null: an absent optional field resolves to
// its default, while an explicit null is handled per type.
type missingSentinel struct{}

// Load reads and validates selfdoc.json from dir.
//
// It returns a nil Config and a nil error when the file does not exist --
// "this directory is not a selfdoc project" is an answer, not a failure. A
// malformed or invalid document is a [ConfigError].
func Load(dir string) (Config, error) {
	configPath := filepath.Join(dir, "selfdoc.json")

	info, err := os.Stat(configPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	// A repository still on the layout before this one is refused before its
	// config is judged: the one command that reads it is the move, and a
	// config defect found first would hide the move the repository needs.
	if err := layout.RefuseUnmigrated(dir); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	raw, err := DecodeDocument(data)
	if err != nil {
		return nil, configErrorf("selfdoc.json is not valid JSON: %v", err)
	}
	validated, err := ValidateConfig(raw)
	if err != nil {
		return nil, err
	}
	if err := refuseOldLayout(dir, validated); err != nil {
		return nil, err
	}
	return validated, nil
}

// refuseOldLayout is where every command that reads project state meets the
// repository that has not been moved to the stricttools/ layout yet.
//
// The check sits in the loader because the loader is what every such command
// runs first, and because the paths it judges are the config's own. There is
// no dual reading and no migrator: a repository is moved once, by hand, and
// refused until it is.
func refuseOldLayout(dir string, validated Config) error {
	postsDir := ""
	if posts, ok := validated["posts"].(map[string]any); ok {
		postsDir, _ = posts["dir"].(string)
	}
	docsDir, _ := validated["docs"].(string)
	outputDir, _ := validated["output"].(string)
	return layout.RefuseOldLayout(dir, docsDir, outputDir, postsDir)
}

// ValidateConfig validates a raw config document and returns the resolved
// config.
//
// This is what [Load] runs on the parsed contents of selfdoc.json; it is
// separate so a config assembled in memory goes through the same rules as
// one read from disk. The accepted value shapes are the ones [DecodeDocument]
// produces.
func ValidateConfig(raw any) (Config, error) {
	document, ok := raw.(map[string]any)
	if !ok {
		return nil, &ConfigError{Message: "selfdoc.json must be a JSON object"}
	}

	// Migration error: top-level "language" is no longer supported
	if _, present := document["language"]; present {
		return nil, &ConfigError{Message: "Top-level 'language' field is no longer supported. " +
			`Move language into each source entry: ` +
			`"source": [{"path": "src/", "language": "python"}]`}
	}

	// Migration errors: three retired topology keys. The block is not strict,
	// so a key nothing declares is read by nobody; each retired one is
	// refused by name instead, saying what took its place.
	if rawTopology, isMap := document["topology"].(map[string]any); isMap {
		if _, present := rawTopology["assembly"]; present {
			return nil, &ConfigError{Message: "'topology.assembly' is no longer supported. The assembly repo " +
				`has one home: "assembly": {"repo": "owner/repo"}`}
		}
		if _, present := rawTopology["projects"]; present {
			return nil, &ConfigError{Message: "'topology.projects' is no longer supported. " +
				"Every project the site serves is mounted under its own " +
				"slug, so a link into another project resolves to " +
				"'<topology.docs_base>/<slug>/' for any slug and there is " +
				"nothing to declare per project. Delete the key."}
		}
		if _, present := rawTopology["legacy_blog_host"]; present {
			return nil, &ConfigError{Message: "'topology.legacy_blog_host' is no longer supported. " +
				"The assembly emits no redirect worker: a host that should " +
				"answer somewhere else is a redirect rule on the DNS zone, " +
				"applied where the site is hosted rather than generated into " +
				"its output. Delete the key and add the zone rule."}
		}
	}

	// Check for unknown root-level keys. The keys are visited in sorted
	// order, not the document's order, so the named key is reproducible.
	known := make(map[string]bool, len(Schema))
	for _, spec := range Schema {
		known[spec.Name] = true
	}
	for _, key := range sortedKeys(document) {
		if !known[key] {
			return nil, configErrorf("unknown config key %s", util.PythonRepr(key))
		}
	}

	// The engine is declared, never inferred. Every selfdoc site builds a
	// search UI -- 'search: "hidden"' still answers Cmd/Ctrl+K -- so the
	// engine behind it has to be named in the config. The valid set has
	// one member, which is still an explicit declaration: the key is the
	// extension point, and its absence must not silently pick anything.
	// The generic "missing required field" message would not name the
	// value, so the requirement says it here instead.
	if document["search_engine"] == nil {
		return nil, &ConfigError{Message: "missing required field 'search_engine'. Every site builds a " +
			`search UI, so declare the engine that answers it: ` +
			`"search_engine": "pagefind" (the only valid value).`}
	}

	// The author is declared, never inferred. Every page this build writes
	// carries structured data that states who wrote it; with no block to
	// read, the emitters used to mint an Organization named after the
	// project directory, so a site published a legal entity nobody had
	// declared. The generic "missing required field" message would not say
	// what to write, so the requirement says it here instead.
	if document["author"] == nil {
		return nil, &ConfigError{Message: "missing required field 'author'. Every page carries structured " +
			"data naming who wrote it, and there is no inferred author: " +
			`"author": {"name": "Your Name", "url": "https://you.example"} ` +
			"-- optionally with " +
			`"same_as": ["https://github.com/you"] for external profiles.`}
	}

	// Migration error: source items must be dicts, not strings
	if rawSource, isList := document["source"].([]any); isList {
		for i, item := range rawSource {
			if text, isStr := item.(string); isStr {
				return nil, configErrorf(
					"source[%d] is a plain string (%s). "+
						"Source entries must be objects with 'path' and 'language': "+
						`{"path": "src/", "language": "python"}`,
					i, util.PythonRepr(text))
			}
		}
	}

	// Validate each field against its schema spec
	result := make(Config, len(Schema))
	for i := range Schema {
		spec := &Schema[i]
		var rawValue any = missingSentinel{}
		if present, ok := document[spec.Name]; ok {
			rawValue = present
		}
		validated, err := validateField(spec, rawValue, spec.Name)
		if err != nil {
			return nil, err
		}
		result[spec.Name] = validated
	}

	if err := postValidate(result); err != nil {
		return nil, err
	}
	return result, nil
}

// sortedKeys returns m's keys in code-point order, so every diagnostic that
// names one of several offending keys names the same one on every run.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateField validates a single config value against its [FieldSpec] and
// returns the validated (and possibly transformed) value.
//
// Nested lists and objects are rewritten in place as the walk descends,
// which is how a transform applied deep inside a document reaches the
// caller's own value.
func validateField(spec *FieldSpec, value any, path string) (any, error) {
	// Handle missing values
	_, isAbsent := value.(missingSentinel)
	if isAbsent || value == nil {
		if isAbsent && spec.Required {
			return nil, configErrorf("missing required field '%s'", path)
		}
		if isAbsent {
			if spec.DefaultFactory != nil {
				return spec.DefaultFactory(), nil
			}
			return spec.Default, nil
		}
		// value is an explicit null -- for object and array types that are
		// not required, treat it the same as absent
		if !spec.Required {
			if spec.Type == FieldDict || spec.Type == FieldList {
				if spec.DefaultFactory != nil {
					return spec.DefaultFactory(), nil
				}
				return spec.Default, nil
			}
			return spec.Default, nil
		}
		return nil, configErrorf("missing required field '%s'", path)
	}

	switch spec.Type {
	case FieldStr:
		text, ok := value.(string)
		if !ok {
			if len(spec.Choices) > 0 {
				return nil, configErrorf("invalid %s value %s; must be one of: %s",
					path, util.PythonRepr(value), strings.Join(spec.Choices, ", "))
			}
			return nil, configErrorf("'%s' must be a string", path)
		}
		if !spec.AllowEmpty && text == "" {
			return nil, configErrorf("'%s' must be a non-empty string", path)
		}
		if len(spec.Choices) > 0 && !containsString(spec.Choices, text) {
			return nil, configErrorf("invalid %s value %s; must be one of: %s",
				path, util.PythonRepr(text), strings.Join(spec.Choices, ", "))
		}
		if spec.Pattern != "" && !matchPattern(spec.Pattern, text) {
			return nil, configErrorf("invalid %s %s; must match pattern %s",
				path, util.PythonRepr(text), spec.Pattern)
		}
		if spec.MustContain != "" && !strings.Contains(text, spec.MustContain) {
			return nil, configErrorf("invalid %s %s; must contain %s",
				path, util.PythonRepr(text), util.PythonRepr(spec.MustContain))
		}
		if spec.Transform != nil {
			return spec.Transform(text), nil
		}
		return text, nil

	case FieldBool:
		flag, ok := value.(bool)
		if !ok {
			return nil, configErrorf("'%s' must be a boolean", path)
		}
		return flag, nil

	case FieldInt:
		number, ok := asInt(value)
		if !ok {
			return nil, configErrorf("'%s' must be an integer%s", path, rangeSuffix(spec))
		}
		if spec.MinVal != nil && float64(number) < boundValue(spec.MinVal) {
			return nil, configErrorf("'%s' must be an integer between %s and %s",
				path, formatBound(spec.MinVal), formatBound(spec.MaxVal))
		}
		if spec.MaxVal != nil && float64(number) > boundValue(spec.MaxVal) {
			return nil, configErrorf("'%s' must be an integer between %s and %s",
				path, formatBound(spec.MinVal), formatBound(spec.MaxVal))
		}
		return number, nil

	case FieldFloat:
		if _, isBool := value.(bool); isBool {
			return nil, configErrorf("'%s' must be a number%s", path, rangeSuffix(spec))
		}
		number, ok := asFloat(value)
		if !ok {
			return nil, configErrorf("'%s' must be a number%s", path, rangeSuffix(spec))
		}
		if spec.MinVal != nil && number < boundValue(spec.MinVal) {
			return nil, configErrorf("'%s' must be a number between %s and %s",
				path, formatBound(spec.MinVal), formatBound(spec.MaxVal))
		}
		if spec.MaxVal != nil && number > boundValue(spec.MaxVal) {
			return nil, configErrorf("'%s' must be a number between %s and %s",
				path, formatBound(spec.MinVal), formatBound(spec.MaxVal))
		}
		return number, nil

	case FieldList:
		items, ok := value.([]any)
		if !ok {
			if spec.MinLength != nil && *spec.MinLength > 0 {
				return nil, configErrorf("'%s' must be a non-empty list", path)
			}
			return nil, configErrorf("'%s' must be a list", path)
		}
		if spec.MinLength != nil && len(items) < *spec.MinLength {
			return nil, configErrorf("'%s' must be a non-empty list", path)
		}
		if spec.ItemSpec != nil {
			for i, item := range items {
				validated, err := validateField(spec.ItemSpec, item, fmt.Sprintf("%s[%d]", path, i))
				if err != nil {
					return nil, err
				}
				items[i] = validated
			}
		}
		return items, nil

	case FieldDict:
		object, ok := value.(map[string]any)
		if !ok {
			return nil, configErrorf("'%s' must be an object", path)
		}

		// No children = open key set (directives, examples, and the like).
		// Keys may still be shape-constrained and values may still carry
		// their own spec; without either this is just an object check.
		if len(spec.Children) == 0 {
			if spec.KeyPattern != "" {
				for _, key := range sortedKeys(object) {
					if !matchPattern(spec.KeyPattern, key) {
						return nil, configErrorf("invalid %s key %s; must match pattern %s",
							spec.Name, util.PythonRepr(key), spec.KeyPattern)
					}
				}
			}
			if spec.ItemSpec != nil {
				for _, key := range sortedKeys(object) {
					validated, err := validateField(spec.ItemSpec, object[key], path+"."+key)
					if err != nil {
						return nil, err
					}
					object[key] = validated
				}
			}
			return object, nil
		}

		// Check for unknown keys in strict mode
		if spec.StrictKeys {
			knownKeys := make([]string, 0, len(spec.Children))
			for i := range spec.Children {
				knownKeys = append(knownKeys, spec.Children[i].Name)
			}
			sort.Strings(knownKeys)
			for _, key := range sortedKeys(object) {
				if !containsString(knownKeys, key) {
					return nil, configErrorf("invalid %s key %s; must be one of: %s",
						spec.Name, util.PythonRepr(key), strings.Join(knownKeys, ", "))
				}
			}
		}

		// Validate children that are present or required
		for i := range spec.Children {
			child := &spec.Children[i]
			childPath := path + "." + child.Name
			if present, ok := object[child.Name]; ok {
				validated, err := validateField(child, present, childPath)
				if err != nil {
					return nil, err
				}
				object[child.Name] = validated
			} else if child.Required {
				return nil, configErrorf("'%s' is required", childPath)
			}
			// If not required and not present, leave the object as-is
			// (don't inject defaults for sub-object fields)
		}

		return object, nil
	}

	// Unreachable for valid FieldType values
	return nil, configErrorf("unknown field type %s for '%s'", util.PythonRepr(string(spec.Type)), path)
}

// rangeSuffix renders the " between X and Y" a type-mismatch diagnostic
// carries when the field declares both bounds.
func rangeSuffix(spec *FieldSpec) string {
	if spec.MinVal != nil && spec.MaxVal != nil {
		return fmt.Sprintf(" between %s and %s",
			formatBound(spec.MinVal), formatBound(spec.MaxVal))
	}
	return ""
}

// boundValue reads a declared bound as a float64 for comparison.
func boundValue(v any) float64 {
	if f, ok := asFloat(v); ok {
		return f
	}
	panic(fmt.Sprintf("config: bound %#v is not a number", v))
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// postValidate applies the cross-field rules, after every individual field
// has been validated, and rewrites config where a declaration implies a
// value (the unversioned rewrite).
func postValidate(config Config) error {
	// Feedback at-least-one: if feedback is present, at least one of
	// webhook or ga must be set
	if feedback, ok := config["feedback"].(map[string]any); ok {
		if feedback["webhook"] == nil && feedback["ga"] == nil {
			return &ConfigError{Message: "'feedback' must contain at least one of 'webhook' or 'ga'"}
		}
	}

	// Deploy.project conditional: cloudflare-pages requires project
	if deploy, ok := config["deploy"].(map[string]any); ok {
		project, _ := deploy["project"].(string)
		if deploy["provider"] == "cloudflare-pages" && project == "" {
			return &ConfigError{Message: "'deploy.project' is required for cloudflare-pages provider"}
		}
	}

	// Locales validation: unique codes, at most one default
	if locales, ok := config["locales"].([]any); ok {
		seenCodes := map[string]bool{}
		for _, entry := range locales {
			locale, _ := entry.(map[string]any)
			code, _ := locale["code"].(string)
			if seenCodes[code] {
				return configErrorf("duplicate locale code %s in 'locales'", util.PythonRepr(code))
			}
			seenCodes[code] = true
		}
		defaults := 0
		for _, entry := range locales {
			locale, _ := entry.(map[string]any)
			if flag, ok := locale["default"].(bool); ok && flag {
				defaults++
			}
		}
		if defaults > 1 {
			return configErrorf("at most one locale may have 'default: true', found %d", defaults)
		}
	}

	// A project with no public version says so, and the declaration is
	// what every version-shaped emitter reads: one anonymous version,
	// whose empty string means no badge, no search filter, no picker and
	// no version segment in any address. Nothing is sniffed and no number
	// is invented.
	if unversioned, ok := config["unversioned"].(bool); ok && unversioned {
		if config["versions"] != nil {
			return &ConfigError{Message: "'unversioned': true and 'versions' contradict each other. " +
				"A project either has public versions or declares it has " +
				"none -- remove one of the two."}
		}
		if source, ok := config["source"].([]any); ok && len(source) > 0 {
			return &ConfigError{Message: "'unversioned': true is refused for a project that declares " +
				"'source'. Code is what gets released, so it carries a " +
				`version: declare it, e.g. "versions": [{"version": ` +
				`"0.1.0"}].`}
		}
		config["versions"] = []any{map[string]any{"version": ""}}
	}

	// Versions validation: unique version strings
	if versions, ok := config["versions"].([]any); ok {
		seenVersions := map[string]bool{}
		for _, entry := range versions {
			version, _ := entry.(map[string]any)
			name, _ := version["version"].(string)
			if seenVersions[name] {
				return configErrorf("duplicate version string %s in 'versions'", util.PythonRepr(name))
			}
			seenVersions[name] = true
		}
	}

	// lint_ignore validation: every suppressed code must be a real one,
	// and must be suppressible at all. A mistyped code suppresses nothing
	// and an error-severity code may not be silenced, so both are refused
	// at load rather than sitting in the config looking effective.
	if lintIgnore, ok := config["lint_ignore"].([]any); ok && len(lintIgnore) > 0 && LintCodeValidator != nil {
		codes := make([]string, 0, len(lintIgnore))
		for _, entry := range lintIgnore {
			code, _ := entry.(string)
			codes = append(codes, code)
		}
		if err := LintCodeValidator(codes, "'lint_ignore'"); err != nil {
			return &ConfigError{Message: err.Error()}
		}
	}

	// Unified validation: unique project slugs (explicit or derived from path)
	if unified, ok := config["unified"].(map[string]any); ok {
		projects, _ := unified["projects"].([]any)
		seenSlugs := map[string]bool{}
		for _, entry := range projects {
			project, _ := entry.(map[string]any)
			slug, _ := project["slug"].(string)
			if slug == "" {
				path, _ := project["path"].(string)
				slug = posixBasename(strings.TrimRight(path, "/"))
			}
			if seenSlugs[slug] {
				return configErrorf("duplicate project slug %s in 'unified.projects'", util.PythonRepr(slug))
			}
			seenSlugs[slug] = true
		}
	}

	return nil
}

// UnversionedVersion is what a project declaring "unversioned": true is
// dispatched and recorded under wherever a version string is required.
//
// The assembly's dispatch payload, its derived membership record and the
// commit message a deploy writes all carry a version. A project with no public
// version has nothing to put there, and the empty string is not an answer: the
// membership record refuses an empty field, because a record that lost its
// version used to read the same as one that never had one. This literal is the
// answer -- it is never a number, so nothing can mistake it for a release, and
// every reader that renders a version treats it as "no version to show".
const UnversionedVersion = "unversioned"

// IsUnversioned reports whether config declares the project has no public
// version.
//
// It reads the declaration, not the rewritten "versions" array [postValidate]
// derives from it: the array is what the build addresses pages with, and the
// declaration is what says the project has no version at all.
func IsUnversioned(config map[string]any) bool {
	declared, isBool := config["unversioned"].(bool)
	return isBool && declared
}

// OutputRel is the build output directory a project's config declares,
// relative to the project root, or the layout's default when it declares none.
func OutputRel(cfg Config) string {
	if out, ok := cfg["output"].(string); ok {
		return out
	}
	return layout.OutputDefault
}
