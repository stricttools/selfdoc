package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/strictcli/go/strictcli"
)

// sourceEntry is one entry of the scaffolded config's "source" array.
type sourceEntry struct {
	Path     string
	Language string
}

// detectSourceEntries names the directories a language's sources are expected
// under, in the shapes the three first-class layouts really take.
func detectSourceEntries(dir, language string) []sourceEntry {
	isDir := func(name string) bool {
		info, err := os.Stat(filepath.Join(dir, name))
		return err == nil && info.IsDir()
	}
	isFile := func(name string) bool {
		info, err := os.Stat(filepath.Join(dir, name))
		return err == nil && !info.IsDir()
	}

	var paths []string
	switch language {
	case "python":
		for _, candidate := range []string{"src", "lib"} {
			if isDir(candidate) {
				paths = append(paths, candidate+"/")
				break
			}
		}
		if len(paths) == 0 {
			for _, entry := range sortedDirEntries(dir) {
				if isDir(entry) && isFile(filepath.Join(entry, "__init__.py")) {
					paths = append(paths, entry+"/")
					break
				}
			}
		}
	case "go":
		for _, candidate := range []string{"pkg", "internal", "cmd"} {
			if isDir(candidate) {
				paths = append(paths, candidate+"/")
			}
		}
	case "typescript":
		for _, candidate := range []string{"src", "lib"} {
			if isDir(candidate) {
				paths = append(paths, candidate+"/")
				break
			}
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}

	entries := make([]sourceEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, sourceEntry{Path: path, Language: language})
	}
	return entries
}

// sortedDirEntries lists dir's immediate names in sorted order, which is what
// makes the detection above deterministic.
func sortedDirEntries(dir string) []string {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	sort.Strings(names)
	return names
}

// detectMainModule names the module the starter page's ref directive points
// at: a top-level Python package, else the project directory's own name.
func detectMainModule(dir string) string {
	for _, entry := range sortedDirEntries(dir) {
		if strings.HasPrefix(entry, ".") || entry == "tests" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, entry))
		if err != nil || !info.IsDir() {
			continue
		}
		if init, err := os.Stat(filepath.Join(dir, entry, "__init__.py")); err == nil && !init.IsDir() {
			return entry
		}
	}
	return projectName(dir)
}

// projectName is the project directory's own name.
func projectName(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return filepath.Base(abs)
}

func (c *cli) registerInit() {
	c.app.Command("init", "Initialize selfdoc in this repository: write selfdoc.json (versioned at the version the project's manifest states, 0.0.0 when it states none), the ownership manifests of .stricttools/docs, .stricttools/docs-state and .stricttools/docs-cache, and a starter docs page",
		c.cmdInit,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.StringFlag("base-url", "Base URL the generated site will be served from (e.g. 'https://docs.example.com'). Required: it is the site's own address, which selfdoc cannot infer, and every canonical link, sitemap entry and feed URL is built from it", strictcli.Required()),
			strictcli.StringFlag("author-name", "Display name of the site's author. Required: every page carries structured data naming who wrote it, and a name is a fact about a person that selfdoc cannot invent", strictcli.Required()),
			strictcli.StringFlag("author-url", "Canonical URL identifying the site's author (e.g. 'https://you.example'). Required alongside --author-name: the structured data names an identity, and an identity has an address", strictcli.Required()),
			strictcli.BoolFlag("auto-commit", "Automatically commit the generated selfdoc.json and the starter page template to git. Omitted, it commits; pass --no-auto-commit to leave the files uncommitted", strictcli.Optional()),
		),
	)
}

// newProjectVersion is the version init declares for a project whose manifest
// states none yet.
const newProjectVersion = "0.0.0"

// indexRel is the starter page init writes, relative to the project root.
const indexRel = layout.DocsRel + "/index.md"

func (c *cli) cmdInit(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	baseURL := strictcli.Get[string](kwargs, "base_url")
	authorName := strictcli.Get[string](kwargs, "author_name")
	authorURL := strictcli.Get[string](kwargs, "author_url")
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)
	dir := c.dir()

	if info, err := os.Stat(filepath.Join(dir, "selfdoc.json")); err == nil && !info.IsDir() {
		c.println("selfdoc.json already exists. Aborting.")
		return strictcli.Exit(1)
	}

	if strings.TrimSpace(baseURL) == "" {
		return c.failf("--base-url must be a non-empty URL.")
	}

	// The author is a fact about a person, so it is an input, never a guess.
	// A scaffolded config that omitted it would emit a site whose structured
	// data named nobody -- or, as it once did, an organisation invented from
	// the directory name.
	if strings.TrimSpace(authorName) == "" {
		return c.failf("--author-name must be a non-empty name.")
	}
	if strings.TrimSpace(authorURL) == "" {
		return c.failf("--author-url must be a non-empty URL.")
	}

	// A project with no detectable language is a codeless project: a
	// portfolio or personal site that is nothing but markdown pages. It gets
	// a config with no 'source' key at all -- an empty array would declare
	// the same thing more verbosely -- and a starter page with no
	// code-extraction directive.
	detected, err := extractors.DetectLanguages(dir)
	if err != nil {
		return c.fail(err)
	}

	var sourceEntries []sourceEntry
	var detectedLanguages []string
	for _, entry := range detected {
		sourceEntries = append(sourceEntries, detectSourceEntries(dir, entry.Language)...)
		detectedLanguages = append(detectedLanguages, entry.Language)
	}

	primaryLanguage := ""
	mainModule := ""
	if len(detected) > 0 {
		primaryLanguage = detected[0].Language
		mainModule = detectMainModule(dir)
	}
	name := projectName(dir)

	// The config is written in the versioned form: "version" and "versions"
	// at the version the project's own manifest states, or 0.0.0 for a new
	// project that states none yet. It is never "unversioned": true, which
	// gen refuses as soon as the project has source code -- a codeless site
	// that later gains code would otherwise have to be re-declared by hand.
	initVersion := util.DetectProjectVersion(dir, "")
	if initVersion == "" {
		initVersion = newProjectVersion
	}
	versionDeclaration := []jsonPair{
		{"version", initVersion},
		{"versions", []any{jsonObject{{"version", initVersion}}}},
	}

	// Running init is the repository adopting selfdoc, so this is where the
	// ownership manifests of the directories a project needs are written --
	// before anything else, so a directory another tool owns refuses the
	// whole init with nothing written.
	grantedManifests, err := layout.GrantInit(handle, dir)
	if err != nil {
		return c.fail(err)
	}

	// Everything the loader and the build require is written into the file,
	// so the emitted config is buildable with no hand-editing. base_url comes
	// from the caller; the version declaration above and the single default
	// locale are the honest starting point for a new site.
	resolvedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	resolvedAuthorName := strings.TrimSpace(authorName)
	resolvedAuthorURL := strings.TrimRight(strings.TrimSpace(authorURL), "/")

	document := jsonObject{
		{"base_url", resolvedBase},
		{"author", jsonObject{
			{"name", resolvedAuthorName},
			{"url", resolvedAuthorURL},
		}},
	}
	if len(sourceEntries) > 0 {
		items := make([]any, 0, len(sourceEntries))
		for _, entry := range sourceEntries {
			items = append(items, jsonObject{
				{"path", entry.Path},
				{"language", entry.Language},
			})
		}
		document = append(document, jsonPair{"source", items})
	}
	document = append(document,
		jsonPair{"docs", layout.DocsDefault},
		jsonPair{"output", layout.OutputDefault},
	)
	document = append(document, versionDeclaration...)
	document = append(document,
		jsonPair{"locales", []any{jsonObject{
			{"code", "en"},
			{"label", "English"},
			{"default", true},
		}}},
		jsonPair{"search_engine", "pagefind"},
	)

	if err := handle.AtomicWrite(filepath.Join(dir, "selfdoc.json"),
		[]byte(encodeJSON(document, 0)+"\n"), effects.ModeDefault); err != nil {
		return c.fail(err)
	}

	if err := layout.EnsureDir(handle, dir, layout.DocsRel); err != nil {
		return c.fail(err)
	}

	// The API reference section only appears when there is source code to
	// extract from -- in a codeless project a 'ref' directive is a hard
	// error, not an empty section.
	indexPath := layout.Path(dir, indexRel)
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		today := time.Now().Format("2006-01-02")
		frontmatter, err := util.RenderFrontmatter([]util.FrontmatterField{
			{Key: "title", Value: name},
			{Key: "description", Value: "Documentation for " + name},
			{Key: "date", Value: util.FrontmatterDate(today)},
		})
		if err != nil {
			return c.fail(err)
		}
		starter := frontmatter +
			"\n" +
			"# " + name + "\n" +
			"\n" +
			"Welcome to the " + name + " documentation.\n"
		if len(detected) > 0 {
			starter += "\n## API Reference\n\n" +
				`:-: ref path="` + mainModule + `" lang="` + primaryLanguage + `"` + "\n"
		}
		if err := handle.Write(indexPath, []byte(starter), effects.ModeDefault); err != nil {
			return c.fail(err)
		}
	}

	sourcePaths := make([]string, 0, len(sourceEntries))
	for _, entry := range sourceEntries {
		sourcePaths = append(sourcePaths, entry.Path)
	}
	if len(detected) > 0 {
		c.printf("Initialized selfdoc for %s project '%s'\n", strings.Join(detectedLanguages, ", "), name)
	} else {
		c.printf("Initialized selfdoc for codeless project '%s' (no source code detected)\n", name)
	}
	c.println("  Created: selfdoc.json")
	c.printf("  Created: %s\n", indexRel)
	for _, rel := range grantedManifests {
		c.printf("  Created: %s\n", rel)
	}
	if len(sourcePaths) > 0 {
		c.printf("  Source:  %s\n", strings.Join(sourcePaths, ", "))
	}
	c.printf("  Base URL: %s\n", resolvedBase)
	c.printf("  Author:   %s <%s>\n", resolvedAuthorName, resolvedAuthorURL)
	c.printf("\nRun 'selfdoc build' to generate documentation.\n")

	if autoCommit {
		committed := append([]string{"selfdoc.json", indexRel}, grantedManifests...)
		committed = append(committed, layout.Root+"/"+layout.IgnoreFileName)
		if _, _, err := gitcommit.AutoCommit(
			committed, "selfdoc init", dir, handle,
		); err != nil {
			return c.fail(err)
		}
	}

	return strictcli.Exit(0)
}
