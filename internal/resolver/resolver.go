// Package resolver dispatches one directive to whatever can answer it.
//
// A resolver is built for a project and called once per directive. It tries
// the content directives first (they are language-agnostic), then the
// project's own custom directives, and then the language extractors -- which
// for a multi-language project means deciding which language owns the path the
// directive names.
//
// # Custom directives run out of process
//
// The `directives` config key maps a directive name to a script, and the
// contract that key has always had is Python's: the script defines
// resolve(attrs, config, body) and returns Markdown. That contract is kept, so
// the script is still loaded and called by Python: an embedded driver
// (driver.py) is handed to python3, the script's path as its argument and one
// JSON object on standard input, and the Markdown it prints is what replaces
// the directive.
//
// The driver runs as a declared read through the effects handle, so a
// --dry-run resolves directives like any other run.
//
// A script that cannot be loaded, one with no callable resolve, one that
// raises, and a machine with no python3 are each a hard error naming the
// directive and the script. The Python this replaces swallowed all four into
// an inline note on the page; a page that says "custom directive 'api'
// failed" where its API reference belongs is not a page anybody wanted
// published, and the note was as easy to miss as any other paragraph.
//
// # Directives compiled into the binary
//
// The same config key also accepts a [BuiltinDirective]: a function this
// binary carries, registered by the caller that owns it and resolved in
// process at the point a script would have been. The registrars are the
// site-level directives an assembled site's home project carries, which render
// from the assembly's manifests -- state a build is handed and no config
// document can hold. They arrived as shipped Python shim scripts before there
// was one binary to compile them into.
package resolver

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/content"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

//go:embed driver.py
var driverFS embed.FS

// driverSource is the program python3 is handed on its command line.
//
// It reaches the interpreter as an argument rather than as a file so that
// resolving a directive writes nothing anywhere -- the same shape the Python
// extractor's driver uses.
var driverSource = func() string {
	data, err := driverFS.ReadFile("driver.py")
	if err != nil {
		panic("resolver: the embedded driver is missing: " + err.Error())
	}
	return string(data)
}()

// driverTimeout bounds one custom-directive run. A directive script reads a
// file or two and prints a table; a minute is generous, and a script that
// hangs would otherwise hang the build.
const driverTimeout = 60 * time.Second

// languageGroup is one configured language with every source path declared for
// it, and the extractor that reads them.
type languageGroup struct {
	language  string
	extractor extractors.Extractor
	paths     []string
}

// Resolver resolves directives for one project.
//
// It is not safe for concurrent use: LastSourceEntry is per-call state that a
// second caller would overwrite.
type Resolver struct {
	// LastSourceEntry is the source entry the last resolved directive's
	// path belonged to, or nil when the directive named no path, named one
	// that resolved nowhere, or was not a code directive at all. The
	// coverage measurement reads it to learn which source a page covered.
	LastSourceEntry *extractors.SourceEntry

	config        map[string]any
	payloadConfig map[string]any
	baseDir       string
	custom        map[string]string
	builtin       map[string]BuiltinDirective
	groups        []languageGroup
	handle        *effects.Handle
}

// BuiltinDirective resolves one directive in process, from whatever state the
// registering caller captured.
//
// It is the second value the "directives" config key accepts. A string names a
// script, which the Python driver loads and calls; a BuiltinDirective is a
// directive compiled into this binary and handed to [MakeResolver] by the
// caller that owns it, at the one point in the dispatch order the scripts
// occupy. The site-level directives of an assembled site's home project are
// the registrars: they render from the assembly's manifests, which the home
// project's build receives and no config document can hold.
//
// Both values live under one key because a directive name is either known to
// the catalog or declared there, and the name set the config declares is what
// decides which markers a page may carry. A separate key would leave a
// registered directive unknown to that check and refused before it ever
// reached a resolver.
type BuiltinDirective func(attrs map[string]string, body []string) (string, error)

// MakeResolver builds the resolver for a project.
//
// config is the loaded selfdoc.json, baseDir the project root every relative
// path is resolved against, and handle the effects handle the custom-directive
// driver runs under.
func MakeResolver(config map[string]any, baseDir string, handle *effects.Handle) (*Resolver, error) {
	absolute := baseDir
	if resolved, err := filepath.Abs(baseDir); err == nil {
		absolute = resolved
	}

	custom := map[string]string{}
	builtin := map[string]BuiltinDirective{}
	if declared, isObject := config["directives"].(map[string]any); isObject {
		for name, script := range declared {
			switch value := script.(type) {
			case string:
				custom[name] = value
			case BuiltinDirective:
				builtin[name] = value
			}
		}
	}

	entries, err := extractors.ResolveSourceEntries(config)
	if err != nil {
		return nil, err
	}

	// Group the source entries by language, so one group collects every
	// path declared for it, in first-appearance order.
	index := map[string]int{}
	var groups []languageGroup
	for _, entry := range entries {
		position, seen := index[entry.Language]
		if !seen {
			position = len(groups)
			index[entry.Language] = position
			groups = append(groups, languageGroup{
				language:  entry.Language,
				extractor: entry.Extractor,
			})
		}
		groups[position].paths = append(groups[position].paths, entry.Path)
	}

	return &Resolver{
		config:        config,
		payloadConfig: scriptOnlyConfig(config, builtin),
		baseDir:       absolute,
		custom:        custom,
		builtin:       builtin,
		groups:        groups,
		handle:        handle,
	}, nil
}

// scriptOnlyConfig is config as a custom directive's script payload names it:
// the same document with every [BuiltinDirective] entry dropped from its
// "directives" mapping.
//
// The payload is JSON, and a Go function has no JSON form -- so a project that
// registers a built-in directive beside a script one would otherwise fail the
// script's encoding. A built-in has no script to name, so a script that reads
// config["directives"] to find its siblings sees the scripts, which is what
// the key meant before a built-in could be registered under it.
func scriptOnlyConfig(config map[string]any, builtin map[string]BuiltinDirective) map[string]any {
	if len(builtin) == 0 {
		return config
	}
	declared, isObject := config["directives"].(map[string]any)
	if !isObject {
		return config
	}
	scripts := make(map[string]any, len(declared))
	for name, script := range declared {
		if _, isBuiltin := builtin[name]; isBuiltin {
			continue
		}
		scripts[name] = script
	}
	trimmed := make(map[string]any, len(config))
	for key, value := range config {
		trimmed[key] = value
	}
	trimmed["directives"] = scripts
	return trimmed
}

// Resolve resolves one directive into the Markdown that replaces it.
//
// A directive that cannot be answered from the project's own files renders an
// error marker and no error: one bad directive degrades one region of one page
// instead of failing the build. An error is the hard-error family -- a
// directive that reads source code in a project that declares none, an
// ambiguous path, a custom directive that failed -- where a marker would hide
// the fact that the page lost the content it asked for.
func (r *Resolver) Resolve(name string, attrs map[string]string, body []string) (string, error) {
	r.LastSourceEntry = nil

	// Content directives first: callouts, the glossary, the tree, the
	// dependency and endpoint tables, the listings, var and cv.
	rendered, isContent, err := content.ResolveContent(
		name, attrs, body, r.baseDir, r.config,
	)
	if err != nil {
		return "", err
	}
	if isContent {
		return rendered, nil
	}

	// A directive registered by the caller, resolved in process. It sits
	// where a script sits -- ahead of every catalog name that extracts from
	// source code, behind the content directives -- because that is where
	// the shim scripts these replaced were dispatched from.
	if resolve, isBuiltin := r.builtin[name]; isBuiltin {
		return resolve(attrs, body)
	}

	// A custom directive takes priority over a built-in name.
	if script, isCustom := r.custom[name]; isCustom {
		return r.runCustomDirective(name, script, attrs, body)
	}

	// Everything that reaches this point extracts from source code, and a
	// codeless project has none. Rendering a placeholder note here would
	// silently turn a page that asks for an API reference into a page that
	// has none.
	if len(r.groups) == 0 {
		return "", fmt.Errorf(
			"Directive :-: %s extracts from source code, but selfdoc.json "+
				"declares no 'source' entries. Either remove the directive, "+
				`or declare the code it should read: "source": `+
				`[{"path": "src/", "language": "python"}]`,
			name,
		)
	}

	// One language group: dispatch directly, since no ambiguity is
	// possible.
	if len(r.groups) == 1 {
		group := r.groups[0]
		rendered, err := group.extractor.Extract(
			name, attrs, body, group.paths, r.baseDir,
		)
		if err != nil {
			return "", err
		}
		if pathArg := attrs["path"]; pathArg != "" {
			if group.extractor.ResolvePath(pathArg, group.paths, r.baseDir) != "" {
				r.LastSourceEntry = &extractors.SourceEntry{
					Path:      group.paths[0],
					Language:  group.language,
					Extractor: group.extractor,
				}
			}
		}
		return rendered, nil
	}

	// Multi-language dispatch: find which group can resolve the path.
	pathArg := attrs["path"]
	langFilter := attrs["lang"]

	// A declared lang narrows the candidates to that language.
	candidates := r.groups
	if langFilter != "" {
		candidates = nil
		for _, group := range r.groups {
			if group.language == langFilter {
				candidates = append(candidates, group)
			}
		}
	}

	var matches []languageGroup
	for _, group := range candidates {
		if group.extractor.ResolvePath(pathArg, group.paths, r.baseDir) != "" {
			matches = append(matches, group)
		}
	}

	if len(matches) == 1 {
		group := matches[0]
		r.LastSourceEntry = &extractors.SourceEntry{
			Path:      group.paths[0],
			Language:  group.language,
			Extractor: group.extractor,
		}
		return group.extractor.Extract(name, attrs, body, group.paths, r.baseDir)
	}

	if len(matches) > 1 {
		languages := make([]string, 0, len(matches))
		for _, group := range matches {
			languages = append(languages, group.language)
		}
		return "", fmt.Errorf(
			"Ambiguous directive :::%s path=%s resolves in multiple languages: %s",
			name, util.PythonRepr(pathArg), strings.Join(languages, ", "),
		)
	}

	// Zero matches.
	if langFilter != "" && len(candidates) == 0 {
		// The lang attribute named a language this project does not
		// declare.
		configured := make([]string, 0, len(r.groups))
		for _, group := range r.groups {
			configured = append(configured, group.language)
		}
		return fmt.Sprintf(
			"> *[selfdoc: lang=%s not found in configured source languages: %s]*",
			util.PythonRepr(langFilter), strings.Join(configured, ", "),
		), nil
	}

	// The first candidate's extractor reports the failure: it produces the
	// "not found" marker naming the path that resolved nowhere.
	group := candidates[0]
	return group.extractor.Extract(name, attrs, body, group.paths, r.baseDir)
}

// runCustomDirective runs the project's script for name and returns the
// Markdown it printed.
func (r *Resolver) runCustomDirective(
	name, script string, attrs map[string]string, body []string,
) (string, error) {
	scriptPath := filepath.Join(r.baseDir, script)
	if info, err := os.Stat(scriptPath); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf(
			"custom directive '%s' failed: custom directive script not found: %s",
			name, scriptPath,
		)
	}

	payload, err := util.PythonJSON(map[string]any{
		"attrs":    stringMap(attrs),
		"config":   r.payloadConfig,
		"body":     stringList(body),
		"base_dir": r.baseDir,
	})
	if err != nil {
		return "", fmt.Errorf(
			"custom directive '%s' failed: its payload cannot be encoded: %w",
			name, err,
		)
	}

	argv := []string{scriptPath}
	if strings.HasSuffix(scriptPath, ".py") {
		// The contract is Python's, so a Python script is loaded and
		// called by the embedded driver rather than executed.
		argv = []string{"python3", "-c", driverSource, scriptPath}
	}

	result, err := r.handle.Run(
		argv,
		effects.Read(),
		effects.CaptureOutput(),
		effects.Stdin(payload),
		effects.Timeout(driverTimeout),
	)
	if err != nil {
		return "", fmt.Errorf(
			"custom directive '%s' failed: running %s: %w",
			name, scriptPath, err,
		)
	}
	if result.ExitCode != 0 {
		reason := strings.TrimSpace(string(result.Stderr))
		if reason == "" {
			reason = fmt.Sprintf("the script exited %d", result.ExitCode)
		}
		return "", fmt.Errorf("custom directive '%s' failed: %s", name, reason)
	}
	return string(result.Stdout), nil
}

// stringMap is attrs as the payload carries it: an object, never null, so the
// script's resolve always receives a mapping.
func stringMap(attrs map[string]string) map[string]any {
	out := make(map[string]any, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return out
}

// stringList is body as the payload carries it: an array, never null, so the
// script's resolve always receives a list.
func stringList(body []string) []any {
	out := make([]any, 0, len(body))
	for _, line := range body {
		out = append(out, line)
	}
	return out
}
