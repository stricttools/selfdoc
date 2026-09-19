package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/stricttools/selfdoc/internal/strictclisupport"
)

// pagefindProbeTimeout bounds each of the two probes SEARCH001 makes for the
// indexer.
const pagefindProbeTimeout = 10 * time.Second

// CheckDocs validates every directive in a project's docs templates and
// reports its coverage and its diagnostics.
//
// It walks the docs directory, parses the directives on every page, resolves
// each one, measures how much of the project's public surface the pages cover,
// and runs every lint rule over the pages and the project's published posts.
//
// projectConfig may be nil, in which case it is loaded from selfdoc.json.
// dryRun reports staleness without writing the hash store -- under a
// previewing effects handle the write is recorded rather than performed, which
// gives the same reporting and an honest preview, so the command layer leaves
// this false and lets the handle decide. versionFilter, when non-empty, skips
// the multi-version validation pass (VER001), which is what `build --version`
// wants: it is checking one version and needs no cross-version answer.
// versionOverride is the version that version-bearing generated content is
// expected to embed (VER004), overriding the version detected from the project
// manifest; a release orchestrator passes the about-to-be-released version
// here, matching what it passes to `selfdoc gen --version-override`.
func CheckDocs(
	dirPath string,
	projectConfig map[string]any,
	dryRun bool,
	versionFilter string,
	versionOverride string,
	handle *effects.Handle,
) (*CheckResult, error) {
	if projectConfig == nil {
		loaded, err := config.Load(dirPath)
		if err != nil {
			return nil, err
		}
		projectConfig = loaded
	}
	if projectConfig == nil {
		return nil, errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.",
		)
	}

	declaredDocs := configString(projectConfig, "docs", layout.DocsDefault)
	docsDir := filepath.Join(dirPath, strings.TrimRight(declaredDocs, "/"))
	if !isDir(docsDir) {
		return nil, fmt.Errorf("Docs directory '%s' not found.", declaredDocs)
	}

	res, err := resolver.MakeResolver(projectConfig, dirPath, handle)
	if err != nil {
		return nil, err
	}
	result := &CheckResult{}

	validNames, err := docs.ValidNames(projectConfig)
	if err != nil {
		return nil, err
	}

	// Every page, resolved through the shared pipeline: frontmatter,
	// resolved content, raw content and frontmatter line count.
	allDocs, err := docs.ResolveAll(projectConfig, "", dirPath, nil, handle)
	if err != nil {
		return nil, err
	}

	// Per-directive validation and coverage tracking.
	dirResults, resolvedDirectives, err := validateDirectives(
		allDocs, res, validNames, "", true,
	)
	if err != nil {
		return nil, err
	}
	result.DirectiveResults = append(result.DirectiveResults, dirResults...)

	// The directives in the root-file templates (docs/_README.md and the
	// rest). The docs walk skips them -- an underscore-prefixed template is
	// a partial, not a page -- but the directives on them still have to
	// hold.
	rootTemplateDocs, err := resolveRootTemplates(projectConfig, dirPath)
	if err != nil {
		return nil, err
	}
	if len(rootTemplateDocs) > 0 {
		rootResults, _, err := validateDirectives(
			rootTemplateDocs, res, validNames, "", false,
		)
		if err != nil {
			return nil, err
		}
		result.DirectiveResults = append(result.DirectiveResults, rootResults...)
	}

	// The strictcli refusal: a project whose CLI is declared through
	// strictcli documents it with `selfdoc gen`, not with code-help
	// directives that read the source by hand.
	hasCodeHelp := false
	for _, directiveResult := range result.DirectiveResults {
		if strings.HasPrefix(directiveResult.Directive, "code-help") {
			hasCodeHelp = true
			break
		}
	}
	sourcePaths, err := extractors.SourcePaths(projectConfig)
	if err != nil {
		return nil, err
	}
	if hasCodeHelp && strictclisupport.UsesStrictcli(sourcePaths, dirPath) {
		return nil, errors.New(
			"Project uses strictcli — use 'selfdoc gen' for CLI" +
				" documentation instead of code-help directives",
		)
	}

	// Coverage, language-agnostic through the extractor protocol.
	srcEntries, err := extractors.ResolveSourceEntries(projectConfig)
	if err != nil {
		return nil, err
	}
	if len(srcEntries) > 0 {
		coverage, err := computeCoverage(
			projectConfig, dirPath, resolvedDirectives, srcEntries, allDocs,
		)
		if err != nil {
			return nil, err
		}
		result.Coverage = coverage
	}

	// Post validation (POST001-POST007). It runs here, before the lint
	// pass, because the lint slice below is only defined for a post set
	// discovery accepted: an invalid post is reported by this pass, and
	// nothing then asks the slice to resolve a set that does not exist. The
	// diagnostics are appended in their historical position, after the lint
	// pass.
	var postCheckLints []lints.LintResult
	if _, postsDir := postsDirectory(projectConfig, dirPath); postsDir != "" {
		postCheckLints, err = CheckPosts(projectConfig, dirPath, handle)
		if err != nil {
			return nil, err
		}
	}

	// The lint rules. Posts are pages on the site, so they are merged into
	// the slice the rules run over -- keyed by their own path, with their
	// own line numbers. They are merged here and not into the docs walk
	// above: coverage and the staleness baselines are keyed by docs-tree
	// page, and a post is not one of those.
	var postDocs map[string]docs.Doc
	if len(postCheckLints) == 0 {
		postDocs, err = postLintDocs(
			projectConfig, dirPath, res, validNames, nil, false, handle,
		)
		if err != nil {
			return nil, err
		}
	}
	lintSlice := make(map[string]docs.Doc, len(allDocs)+len(postDocs))
	for key, doc := range allDocs {
		lintSlice[key] = doc
	}
	for key, doc := range postDocs {
		lintSlice[key] = doc
	}
	result.Lints, err = runLints(
		lintSlice, dirPath, docsDir, projectConfig, resolvedDirectives, handle,
	)
	if err != nil {
		return nil, err
	}

	// SEARCH001: the indexer every build runs has to be on this machine.
	// Pagefind is the engine, so the check is unconditional -- a build with
	// no indexer produces a site whose search dialog answers nothing.
	if !pagefindAvailable(handle) {
		result.Lints = append(result.Lints, lints.MustLintResult(
			"selfdoc.json", nil, "SEARCH001",
			"pagefind is not installed, so the build cannot index this "+
				"site. Install with: pip install 'pagefind[bin]' or "+
				"npm install -g pagefind",
		))
	}

	// XREF002: a directive's path resolves, but names a file that is not on
	// disk.
	for _, resolved := range resolvedDirectives {
		pathArg := resolved.Attrs["path"]
		if pathArg == "" || resolved.SourceEntry == nil {
			continue
		}
		entry := resolved.SourceEntry
		resolvedPath := entry.Extractor.ResolvePath(pathArg, []string{entry.Path}, dirPath)
		if resolvedPath == "" || !(isFile(resolvedPath) || isDir(resolvedPath)) {
			result.Lints = append(result.Lints, lints.MustLintResult(
				resolved.File, nil, "XREF002",
				fmt.Sprintf(
					"directive path '%s' resolves but file does not exist on disk",
					pathArg,
				),
			))
		}
	}

	// LANG001: a declared source entry names a language selfdoc has no
	// extractor for.
	//
	// The Python asked whether the entry's extractor was the stub instance;
	// here the same question is asked of the language, which is what
	// decides whether a stub was substituted -- a language selfdoc supports
	// but this binary did not link is an error at resolution time, never a
	// stub.
	for _, entry := range srcEntries {
		if !extractors.IsKnownLanguage(entry.Language) {
			result.Lints = append(result.Lints, lints.MustLintResult(
				"selfdoc.json", nil, "LANG001",
				fmt.Sprintf(
					"No extractor for language '%s' (source path: %s)",
					entry.Language, entry.Path,
				),
			))
		}
	}

	// CLI001 and CLI002: the CLI reference pages of a strictcli project.
	cliSchema, err := strictclisupport.ReadSchemaJSON(dirPath)
	if err != nil {
		return nil, err
	}
	if cliSchema != nil {
		cliLints, err := checkCLIPages(cliSchema, projectConfig, dirPath, docsDir)
		if err != nil {
			return nil, err
		}
		result.Lints = append(result.Lints, cliLints...)
	}

	// Project-level version consistency.
	result.Lints = append(result.Lints, checkVersionConsistency(projectConfig, dirPath)...)
	versionMatch, err := checkVersionMatch(projectConfig, dirPath, versionOverride)
	if err != nil {
		return nil, err
	}
	result.Lints = append(result.Lints, versionMatch...)

	// Description staleness and source-docstring drift. The hash keys are
	// prefixed with the locale code, matching the build, so gen and check
	// use the same key space in the hash store.
	driftDirectives := driftDirectivesOf(resolvedDirectives)
	hashes, err := schemaHashes(cliSchema)
	if err != nil {
		return nil, err
	}

	// STALE001/DRIFT001 turn on the ownership predicate (machine-owned
	// state), not on the generated-and-seeded frontmatter flag. A
	// machine-owned page cannot be hand-fixed, so a held baseline would
	// deadlock -- those pages are exempt and their baselines advance. A
	// hand-described generated page gets full staleness protection.
	localePrefix := localePrefixOf(projectConfig)
	exemptKeys, err := machineOwnedKeys(allDocs, dirPath, cliSchema, localePrefix)
	if err != nil {
		return nil, err
	}

	staleWarnings, driftWarnings, err := staleness.UpdateHashes(
		prefixKeys(docs.StalenessDocs(allDocs), localePrefix),
		dirPath, dryRun,
		prefixKeys(driftDirectives, localePrefix),
		prefixKeys(hashes, localePrefix),
		exemptKeys, handle,
	)
	if err != nil {
		return nil, err
	}
	for _, warning := range staleWarnings {
		result.Lints = append(result.Lints, lints.MustLintResult(
			warning.Page, nil, "STALE001", warning.Message,
		))
	}
	for _, warning := range driftWarnings {
		result.Lints = append(result.Lints, lints.MustLintResult(
			warning.Page, nil, "DRIFT001", warning.Message,
		))
	}

	// The post validation results, produced above (before the lint pass) so
	// the post lint slice is only built for a post set discovery accepted.
	result.Lints = append(result.Lints, postCheckLints...)

	// Manifest freshness (STALE002).
	manifestLints, err := checkManifestFreshness(projectConfig, dirPath)
	if err != nil {
		return nil, err
	}
	result.Lints = append(result.Lints, manifestLints...)

	// Emitted-reference resolution (LINK001) over the built tree. Every
	// address the build emits comes from one function, and this is the
	// assertion that the addresses it produced name files that exist: an
	// internal link, a canonical, a sitemap entry or a feed link that
	// resolves to nothing is a broken site. A project with no build output
	// has nothing to check.
	//
	// A project the site mounts under its slug passes that mount in: its
	// output root is not the served root, so the references that cross the
	// boundary in either direction are answered by the assembly's own pass
	// over the whole tree and not by this one.
	mountPrefix := ""
	if urlBuilder := build.MakeURLBuilder(projectConfig); urlBuilder != nil {
		mountPrefix = urlBuilder.MountPrefix()
	}
	// A site-level region is written from the assembled site's own data --
	// the curated project listing, the posts across every project -- and its
	// links address that site: other projects' subtrees, the site-level blog.
	// The project that carries the region writes none of those, so its own
	// build cannot resolve them and the assembly's pass over the whole tree
	// is where they are answered.
	outputLints, err := resolution.CheckProjectOutputResolution(
		filepath.Join(
			dirPath,
			strings.TrimRight(configString(projectConfig, "output", layout.OutputDefault), "/"),
		),
		configString(projectConfig, "base_url", ""),
		mountPrefix,
		[]string{sitedirectives.RegionTag},
		currentPageSources(allDocs, localePrefixOf(projectConfig)),
	)
	if err != nil {
		return nil, err
	}
	result.Lints = append(result.Lints, outputLints...)

	// The older versions, when multi-version is configured. The working
	// tree covers the latest one; each older one is extracted from its git
	// tag and put through directive validation and the lint rules, with
	// every finding labelled by the version. A versionFilter skips this
	// entirely -- the caller is building a single version and needs no
	// cross-version answer.
	versions := configList(projectConfig, "versions")
	if len(versions) > 1 && versionFilter == "" {
		latestVersion := versionEntryString(versions[len(versions)-1])
		for _, entry := range versions {
			versionString := versionEntryString(entry)
			if versionString == latestVersion {
				continue // already validated above, from the working tree
			}
			cacheDir, err := build.ExtractVersionContent(
				versionString, projectConfig, dirPath, handle,
			)
			if err != nil {
				result.Lints = append(result.Lints, lints.MustLintResult(
					"["+versionString+"]", nil, "VER001",
					"Could not extract content for version "+versionString,
				))
				continue
			}

			versionResolver, err := resolver.MakeResolver(projectConfig, cacheDir, handle)
			if err != nil {
				return nil, err
			}
			versionDocs, err := docs.ResolveAll(projectConfig, "", cacheDir, nil, handle)
			if err != nil {
				return nil, err
			}

			versionDirResults, versionResolved, err := validateDirectives(
				versionDocs, versionResolver, validNames,
				"["+versionString+"] ", false,
			)
			if err != nil {
				return nil, err
			}
			result.DirectiveResults = append(result.DirectiveResults, versionDirResults...)

			versionDocsDir := filepath.Join(
				cacheDir, strings.TrimRight(declaredDocs, "/"),
			)
			if !isDir(versionDocsDir) {
				continue
			}
			versionLints, err := runLints(
				versionDocs, cacheDir, versionDocsDir, projectConfig, versionResolved, handle,
			)
			if err != nil {
				return nil, err
			}
			for _, lint := range versionLints {
				// A relabelled diagnostic is a new one: a
				// LintResult's severity is the registry's answer
				// for its code and nothing rewrites a diagnostic
				// after the fact.
				result.Lints = append(result.Lints, lints.MustLintResult(
					"["+versionString+"] "+lint.File(),
					lint.Line(), lint.Code(), lint.Message(),
				))
			}
		}
	}

	return result, nil
}

// versionEntryString reads the version out of one entry of the "versions"
// array.
func versionEntryString(entry any) string {
	value, isMap := entry.(map[string]any)
	if !isMap {
		return ""
	}
	return configString(value, "version", "")
}

// pagefindAvailable reports whether the indexer every build runs answers on
// this machine.
//
// Both probes the Python made are kept: the module under the interpreter, and
// the standalone binary. The Python probed its own interpreter (sys.executable)
// because it was itself a Python program; here the interpreter is named
// explicitly, since a Go binary has no such thing and python3 is already a
// hard requirement of the Python extractor.
func pagefindAvailable(handle *effects.Handle) bool {
	probes := [][]string{
		{"python3", "-m", "pagefind", "--version"},
		{"pagefind", "--version"},
	}
	for _, argv := range probes {
		result, err := handle.Run(
			argv,
			effects.Read(),
			effects.CaptureOutput(),
			effects.Timeout(pagefindProbeTimeout),
		)
		if err != nil {
			continue
		}
		if result.ExitCode == 0 {
			return true
		}
	}
	return false
}

// checkCLIPages runs CLI001 and CLI002 over a strictcli project's CLI
// reference pages.
//
// CLI001 reports a page that was never generated, and a flag token the page
// does not mention. CLI002 reports help text too short to document anything.
func checkCLIPages(
	cliSchema *strictclisupport.Structure, config map[string]any, dirPath, docsDir string,
) ([]lints.LintResult, error) {
	var results []lints.LintResult

	for _, pageName := range strictclisupport.ExpectedCLIPageFilenames(cliSchema) {
		// A CLI reference page is authored in either docs root -- gen
		// writes it into the generated one -- so it is looked up through
		// the same two-root resolution the build walks.
		pagePath, present := docs.FindPage(config, docsDir, dirPath, pageName)
		commandName := strings.TrimSuffix(strings.TrimPrefix(pageName, "cli-"), ".md")
		if !present {
			results = append(results, lints.MustLintResult(
				pageName, nil, "CLI001",
				fmt.Sprintf("missing CLI page for command '%s'", commandName),
			))
			continue
		}
		raw, err := os.ReadFile(pagePath)
		if err != nil {
			return nil, err
		}
		pageContent := string(raw)
		if commandName == "index" {
			continue // the index page documents no individual flags
		}

		var flags []any
		found := false
		for _, command := range cliSchema.Commands {
			if strictclisupport.CommandName(command) == commandName {
				flags = strictclisupport.CommandFlags(command)
				found = true
				break
			}
		}
		if !found {
			for _, group := range cliSchema.Groups {
				if strictclisupport.CommandName(group) != commandName {
					continue
				}
				for _, subcommand := range strictclisupport.GroupCommands(group) {
					flags = append(flags, strictclisupport.CommandFlags(subcommand)...)
				}
				break
			}
		}

		// The tokens an invocation can type, not the flag NAMES: a
		// member-spelled selector's own name is never typed, its choices
		// are, and a choice's scoped flags are tokens like any other.
		// Comparing against names would demand that a page document a
		// token no user can write.
		seen := map[string]bool{}
		for _, token := range strictclisupport.IterFlagTokens(flags) {
			if seen[token] {
				continue
			}
			seen[token] = true
			if !strings.Contains(pageContent, token) {
				results = append(results, lints.MustLintResult(
					pageName, nil, "CLI001",
					fmt.Sprintf("flag '%s' not documented", token),
				))
			}
		}
	}

	checkHelpLength := func(elementKind, elementName, helpText, pageFile string) {
		if helpText == "" || runeLen(helpText) >= minHelpLength {
			return
		}
		results = append(results, lints.MustLintResult(
			pageFile, nil, "CLI002",
			fmt.Sprintf(
				"%s '%s' help text too short (%d chars, minimum %d)",
				elementKind, elementName, runeLen(helpText), minHelpLength,
			),
		))
	}

	for _, command := range cliSchema.Commands {
		commandName := strictclisupport.CommandName(command)
		pageFile := "cli-" + commandName + ".md"
		checkHelpLength("command", commandName, strictclisupport.CommandHelp(command), pageFile)
		for _, flag := range strictclisupport.IterFlagHelp(strictclisupport.CommandFlags(command)) {
			checkHelpLength("flag", flag.Label, flag.Help, pageFile)
		}
		for _, arg := range strictclisupport.CommandArgs(command) {
			entry, isObject := arg.(*strictclisupport.Object)
			if !isObject {
				continue
			}
			checkHelpLength(
				"arg", strictclisupport.Field(entry, "name"),
				strictclisupport.Field(entry, "help"), pageFile,
			)
		}
	}

	for _, group := range cliSchema.Groups {
		groupName := strictclisupport.CommandName(group)
		pageFile := "cli-" + groupName + ".md"
		checkHelpLength("group", groupName, strictclisupport.CommandHelp(group), pageFile)
		for _, subcommand := range strictclisupport.GroupCommands(group) {
			subcommandName := strictclisupport.CommandName(subcommand)
			checkHelpLength(
				"command", groupName+" "+subcommandName,
				strictclisupport.CommandHelp(subcommand), pageFile,
			)
			for _, flag := range strictclisupport.IterFlagHelp(
				strictclisupport.CommandFlags(subcommand),
			) {
				checkHelpLength("flag", flag.Label, flag.Help, pageFile)
			}
			for _, arg := range strictclisupport.CommandArgs(subcommand) {
				entry, isObject := arg.(*strictclisupport.Object)
				if !isObject {
					continue
				}
				checkHelpLength(
					"arg", strictclisupport.Field(entry, "name"),
					strictclisupport.Field(entry, "help"), pageFile,
				)
			}
		}
	}

	return results, nil
}

// currentPageSources maps the output-relative path of every page this run
// resolved to that page's current resolved Markdown.
//
// It is what tells the emitted-reference pass which of a built page's
// references its source still carries: nothing invalidates the built tree, so
// a page there renders whatever the last build resolved. A page this run did
// not resolve -- another locale's, an archived version's -- is absent, and is
// checked exactly as it was before.
func currentPageSources(allDocs map[string]docs.Doc, localePrefix string) map[string]string {
	sources := make(map[string]string, len(allDocs)*2)
	for relPath, doc := range allDocs {
		outputPath := html.MdToHTMLPath(relPath)
		// Both mounts this page can have: a project with one locale
		// serves it from the output root, and a project with several
		// serves it under its locale segment. Nothing is served from
		// both, so registering both names cannot mis-key a page.
		sources[outputPath] = doc.Resolved
		if localePrefix != "" {
			sources[localePrefix+"/"+outputPath] = doc.Resolved
		}
	}
	return sources
}
