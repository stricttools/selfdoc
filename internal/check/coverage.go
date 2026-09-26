package check

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/excludes"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/gen"
)

// isSkeletonPage reports whether a page is a bare auto-generated skeleton.
//
// A page is a skeleton when its frontmatter declares both `generated: true`
// and `seeded: true` -- the description is machine-emitted and has not been
// hand-edited. A generated page whose description was customized (the seeded
// marker removed) counts as documented.
func isSkeletonPage(frontmatter map[string]any) bool {
	generated, isBool := frontmatter["generated"].(bool)
	if !isBool || !generated {
		return false
	}
	seeded, isBool := frontmatter["seeded"].(bool)
	return isBool && seeded
}

// languageGroup is one language's declared source paths with its extractor.
type languageGroup struct {
	language  string
	extractor extractors.Extractor
	paths     []string
}

// computeCoverage counts the public symbols in a project's sources against the
// ones its directives document.
//
// Multi-language: it iterates over every source entry, using each entry's
// extractor to discover public symbols and resolve file paths. Two tiers are
// tracked -- "referenced" is a symbol any directive's resolved output names,
// and "documented" is a symbol named on a non-skeleton page (hand-written, or
// generated with a customized description).
//
// A file whose module path matches a gen.exclude pattern is skipped, so a
// deliberately internal module does not drag coverage down.
//
// allDocs enables the two-tier skeleton detection; pass nil to measure the
// referenced tier alone, which is what the documented tier then equals.
func computeCoverage(
	projectConfig map[string]any,
	baseDir string,
	resolvedDirectives []ResolvedDirective,
	sourceEntries []extractors.SourceEntry,
	allDocs map[string]docs.Doc,
) (*CoverageStats, error) {
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	stats := &CoverageStats{}

	genExcludes := stringList(configDict(projectConfig, "gen")["exclude"])

	skeletonPages := map[string]bool{}
	for relPath, doc := range allDocs {
		if isSkeletonPage(doc.Frontmatter) {
			skeletonPages[relPath] = true
		}
	}

	// Source entries grouped by language, so every path of one language is
	// collected together.
	groups := map[string]*languageGroup{}
	var groupOrder []string
	for _, entry := range sourceEntries {
		group, present := groups[entry.Language]
		if !present {
			group = &languageGroup{language: entry.Language, extractor: entry.Extractor}
			groups[entry.Language] = group
			groupOrder = append(groupOrder, entry.Language)
		}
		group.paths = append(group.paths, entry.Path)
	}

	// Every source file and its public symbols, across every language,
	// keyed by the file's path relative to the project root.
	allSymbols := map[string][]string{}

	for _, language := range groupOrder {
		group := groups[language]
		extensions := group.extractor.FileExtensions()
		for _, sourcePath := range group.paths {
			srcDir := filepath.Join(absBaseDir, sourcePath)
			if !isDir(srcDir) {
				continue
			}
			files, err := walkSourceFiles(absBaseDir, srcDir, extensions, group.language)
			if err != nil {
				return nil, err
			}
			for _, fullPath := range files {
				relToBase, err := filepath.Rel(absBaseDir, fullPath)
				if err != nil {
					return nil, err
				}
				if len(genExcludes) > 0 {
					modPath, hasModPath := gen.FileToModulePath(
						fullPath, absBaseDir, group.language,
					)
					if hasModPath && excludes.IsExcluded(modPath, genExcludes) {
						continue
					}
					// The containing package path is
					// checked too, mirroring gen: a
					// package-level pattern never matches
					// the per-file module path, which for
					// Go carries the file stem.
					pkgPath := filepath.ToSlash(filepath.Dir(relToBase))
					if pkgPath != "" && pkgPath != "." &&
						excludes.IsExcluded(pkgPath, genExcludes) {
						continue
					}
				}
				symbols, err := group.extractor.PublicSymbols(fullPath)
				if err != nil {
					return nil, err
				}
				if len(symbols) > 0 {
					allSymbols[relToBase] = symbols
				}
			}
		}
	}

	// Two sets: every symbol any directive matched, and the subset matched
	// by a directive on a non-skeleton page.
	referencedSet := map[string]bool{}
	documentedSet := map[string]bool{}
	skeletonPageOf := map[string]string{}

	for _, resolved := range resolvedDirectives {
		// A directive that references source files carries a path
		// attribute.
		pathArg := resolved.Attrs["path"]
		if pathArg == "" {
			continue
		}
		// A content directive answered it, so there is no source entry
		// and nothing to measure.
		if resolved.SourceEntry == nil {
			continue
		}

		group, present := groups[resolved.SourceEntry.Language]
		if !present {
			continue
		}

		resolvedPath := resolved.SourceEntry.Extractor.ResolvePath(
			pathArg, group.paths, absBaseDir,
		)
		if resolvedPath == "" {
			continue
		}

		isSkeleton := skeletonPages[resolved.File]
		record := func(qualified string) {
			referencedSet[qualified] = true
			if !isSkeleton {
				documentedSet[qualified] = true
				return
			}
			// The page is what the report names as the cause, so the
			// first skeleton page to claim a symbol is remembered with
			// it. A symbol a non-skeleton page also names never reaches
			// the report's skeleton-only list.
			if _, present := skeletonPageOf[qualified]; !present {
				skeletonPageOf[qualified] = resolved.File
			}
		}

		// Directory-based resolution (a Go package): every file under
		// the directory is a candidate.
		if isDir(resolvedPath) {
			dirAbs, err := filepath.Abs(resolvedPath)
			if err != nil {
				return nil, err
			}
			for relPath, symbols := range allSymbols {
				fileDir := filepath.Dir(filepath.Join(absBaseDir, relPath))
				absFileDir, err := filepath.Abs(fileDir)
				if err != nil {
					return nil, err
				}
				if !strings.HasPrefix(absFileDir, dirAbs) {
					continue
				}
				for _, symbol := range symbols {
					if extractors.SymbolHeadingPattern(symbol).MatchString(resolved.Content) {
						record(relPath + ":" + symbol)
					}
				}
			}
			continue
		}

		// File-based resolution.
		relPath, err := filepath.Rel(absBaseDir, resolvedPath)
		if err != nil {
			return nil, err
		}
		symbols, present := allSymbols[relPath]
		if !present {
			continue
		}
		if resolved.Name == "ref" {
			for _, symbol := range symbols {
				if extractors.SymbolHeadingPattern(symbol).MatchString(resolved.Content) {
					record(relPath + ":" + symbol)
				}
			}
			continue
		}
		// A targeted directive (table-schema, code-test and the rest)
		// names its symbol in the target attribute.
		target := resolved.Attrs["target"]
		if target != "" && containsString(symbols, target) {
			record(relPath + ":" + target)
			continue
		}
		// No target -- every symbol is checked against the content.
		for _, symbol := range symbols {
			if extractors.SymbolHeadingPattern(symbol).MatchString(resolved.Content) {
				record(relPath + ":" + symbol)
			}
		}
	}

	for _, relPath := range sortedKeys(allSymbols) {
		for _, symbol := range allSymbols[relPath] {
			qualified := relPath + ":" + symbol
			stats.Total++
			if !referencedSet[qualified] {
				stats.UnreferencedSymbols = append(stats.UnreferencedSymbols, qualified)
				continue
			}
			stats.ReferencedCount++
			stats.ReferencedSymbols = append(stats.ReferencedSymbols, qualified)
			if documentedSet[qualified] {
				stats.DocumentedCount++
				stats.DocumentedSymbols = append(stats.DocumentedSymbols, qualified)
				continue
			}
			if page := skeletonPageOf[qualified]; page != "" {
				if stats.SkeletonPagesBySymbol == nil {
					stats.SkeletonPagesBySymbol = map[string]string{}
				}
				stats.SkeletonPagesBySymbol[qualified] = page
			}
		}
	}

	return stats, nil
}

// walkSourceFiles returns the source files under srcDir that coverage counts,
// in walk order with each directory's files sorted.
//
// The exclusions are structural rather than configured: environment and build
// directories are pruned, as are the scratch directories at projectRoot's top
// level, and a test file is not part of a project's public
// surface. Go names one "*_test.go", TypeScript and JavaScript name one
// "*.test.*" or "*.spec.*", Python names one "test_*.py" or "conftest.py", and
// any language puts them in a tests, test or __tests__ directory. For Go the
// toolchain's own ignored directories are pruned too -- a package under
// testdata, vendor, or a "." or "_" prefixed directory is not built, not
// documented by a generated page, and therefore not a surface to measure.
func walkSourceFiles(projectRoot, srcDir string, extensions []string, language string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(srcDir, func(walked string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if walked != srcDir && (excludes.ShouldSkipDir(entry.Name()) ||
			excludes.IsScratchDir(projectRoot, walked)) {
			return fs.SkipDir
		}
		if walked != srcDir && language == "go" && excludes.GoToolchainIgnoresDir(entry.Name()) {
			return fs.SkipDir
		}
		relToSrc, err := filepath.Rel(srcDir, walked)
		if err != nil {
			return err
		}
		for _, part := range strings.Split(filepath.ToSlash(relToSrc), "/") {
			if part == "tests" || part == "test" || part == "__tests__" {
				return nil
			}
		}
		names, err := os.ReadDir(walked)
		if err != nil {
			return err
		}
		var fileNames []string
		for _, name := range names {
			if !name.IsDir() {
				fileNames = append(fileNames, name.Name())
			}
		}
		sort.Strings(fileNames)
		for _, fileName := range fileNames {
			if !hasAnySuffix(fileName, extensions) {
				continue
			}
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			if isTestVariant(fileName, extensions) {
				continue
			}
			if strings.HasPrefix(fileName, "test_") && strings.HasSuffix(fileName, ".py") {
				continue
			}
			if fileName == "conftest.py" {
				continue
			}
			files = append(files, filepath.Join(walked, fileName))
		}
		return nil
	})
	return files, err
}

// hasAnySuffix reports whether name ends with any of the suffixes.
func hasAnySuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// isTestVariant reports whether name is a ".test.<ext>" or ".spec.<ext>" file
// for one of the language's extensions.
func isTestVariant(name string, extensions []string) bool {
	for _, extension := range extensions {
		if strings.HasSuffix(name, ".test"+extension) ||
			strings.HasSuffix(name, ".spec"+extension) {
			return true
		}
	}
	return false
}
