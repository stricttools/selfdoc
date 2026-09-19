package unifiedcheck

import (
	"errors"

	"github.com/stricttools/selfdoc/internal/blog/unified"
	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/lints"
)

// CommonSlug is the attribution prefix the docs-site's own pages carry.
//
// The docs-site is the (N+1)th project of a unified build, and its pages are
// mounted under "common", so its diagnostics are labelled the same way a
// constituent project's are rather than being left unattributed.
const CommonSlug = "common"

// CheckUnified checks every constituent project of a unified build, plus the
// docs-site's own content, and returns one merged verdict.
//
// It walks the projects the config's "unified" block names, loads each one's
// own selfdoc.json, and runs [check.CheckDocs] against it. Every directive
// result, every diagnostic and every covered symbol is prefixed with the
// project's mount slug, and the coverage counts of the constituent projects
// are summed. The docs-site's own content is checked last, under
// [CommonSlug]; its coverage is not merged, because the docs-site declares
// the site rather than the API the count measures.
//
// projectConfig may be nil, in which case it is loaded from selfdoc.json in
// dirPath. dryRun reports staleness without writing the hash store.
//
// A project directory with no selfdoc.json becomes a UNIFIED001 diagnostic
// and a project whose check refuses becomes a UNIFIED002 diagnostic carrying
// the refusal's message; in both cases the remaining projects are still
// checked. A config that cannot be read, and a declared project path that
// does not name a directory, are errors: the whole run is described by those
// declarations, so a typo in one is not something to report as a finding
// somewhere far downstream.
func CheckUnified(
	projectConfig map[string]any,
	dirPath string,
	dryRun bool,
	handle *effects.Handle,
) (*check.CheckResult, error) {
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

	unifiedConfig, _ := projectConfig["unified"].(map[string]any)
	if unifiedConfig == nil {
		return nil, errors.New("No 'unified' section in selfdoc.json")
	}

	aggregate := &check.CheckResult{}

	// Every constituent project, checked against its own config.
	for _, projectEntry := range unifiedProjects(unifiedConfig) {
		slug := unified.ProjectSlug(projectEntry)
		projectPath, err := unified.ResolveProjectPath(projectEntry, dirPath)
		if err != nil {
			return nil, err
		}
		projConfig, err := config.Load(projectPath)
		if err != nil {
			return nil, err
		}
		if projConfig == nil {
			aggregate.Lints = append(aggregate.Lints, lints.MustLintResult(
				"["+slug+"]", nil, "UNIFIED001",
				"No selfdoc.json in project '"+slug+"'",
			))
			continue
		}

		projResult, err := check.CheckDocs(
			projectPath, projConfig, dryRun, "", "", handle,
		)
		if err != nil {
			aggregate.Lints = append(aggregate.Lints, lints.MustLintResult(
				"["+slug+"]", nil, "UNIFIED002", err.Error(),
			))
			continue
		}

		mergeAttributed(aggregate, projResult, slug)
		mergeCoverage(aggregate, projResult, slug)
	}

	// The docs-site's own content: the common pages.
	commonResult, err := check.CheckDocs(
		dirPath, projectConfig, dryRun, "", "", handle,
	)
	if err != nil {
		aggregate.Lints = append(aggregate.Lints, lints.MustLintResult(
			"["+CommonSlug+"]", nil, "UNIFIED002", err.Error(),
		))
		return aggregate, nil
	}
	mergeAttributed(aggregate, commonResult, CommonSlug)

	return aggregate, nil
}

// mergeAttributed appends one project's directive results and diagnostics to
// the aggregate, with the project's slug prefixed onto every file.
//
// A diagnostic is immutable -- its severity is the registry's answer for its
// code -- so relabelling one produces a new diagnostic for the same code,
// which reads its severity out of the registry again.
func mergeAttributed(aggregate, projResult *check.CheckResult, slug string) {
	prefix := "[" + slug + "] "
	for _, directiveResult := range projResult.DirectiveResults {
		directiveResult.File = prefix + directiveResult.File
		aggregate.DirectiveResults = append(
			aggregate.DirectiveResults, directiveResult,
		)
	}
	for _, lint := range projResult.Lints {
		aggregate.Lints = append(aggregate.Lints, lints.MustLintResult(
			prefix+lint.File(), lint.Line(), lint.Code(), lint.Message(),
		))
	}
}

// mergeCoverage sums one project's coverage counts into the aggregate and
// appends its symbol identifiers with the project's slug prefixed.
//
// The aggregate's coverage stays nil until a project reports one, so a
// unified site whose projects declare no source has no coverage measurement
// rather than a measurement of zero.
func mergeCoverage(aggregate, projResult *check.CheckResult, slug string) {
	if projResult.Coverage == nil {
		return
	}
	if aggregate.Coverage == nil {
		aggregate.Coverage = &check.CoverageStats{}
	}
	aggregate.Coverage.Total += projResult.Coverage.Total
	aggregate.Coverage.ReferencedCount += projResult.Coverage.ReferencedCount
	aggregate.Coverage.DocumentedCount += projResult.Coverage.DocumentedCount
	aggregate.Coverage.ReferencedSymbols = appendPrefixed(
		aggregate.Coverage.ReferencedSymbols,
		projResult.Coverage.ReferencedSymbols, slug,
	)
	aggregate.Coverage.DocumentedSymbols = appendPrefixed(
		aggregate.Coverage.DocumentedSymbols,
		projResult.Coverage.DocumentedSymbols, slug,
	)
	aggregate.Coverage.UnreferencedSymbols = appendPrefixed(
		aggregate.Coverage.UnreferencedSymbols,
		projResult.Coverage.UnreferencedSymbols, slug,
	)
}

// appendPrefixed appends every symbol identifier to dst with the slug
// prefixed onto it.
func appendPrefixed(dst, symbols []string, slug string) []string {
	for _, symbol := range symbols {
		dst = append(dst, "["+slug+"] "+symbol)
	}
	return dst
}

// unifiedProjects is the project entries a unified config declares, dropping
// anything that is not an object.
//
// The config schema requires the list and requires it to name at least one
// project, so this never answers empty for a config the loader accepted.
func unifiedProjects(unifiedConfig map[string]any) []map[string]any {
	raw, _ := unifiedConfig["projects"].([]any)
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}
