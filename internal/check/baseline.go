package check

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/ownership"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/stricttools/selfdoc/internal/strictclisupport"
)

// AcceptError reports that `selfdoc baseline accept` cannot accept a named
// page.
type AcceptError struct {
	// Message is the whole refusal, already naming every offending page
	// and why it was refused.
	Message string
}

// Error returns the refusal.
func (e *AcceptError) Error() string { return e.Message }

// AcceptedBaseline is one page whose baseline was advanced, with the error the
// advance cleared.
type AcceptedBaseline struct {
	// Page is the page identifier, exactly as `selfdoc check` shows it.
	Page string
	// Code is the error that was cleared: "STALE001" or "DRIFT001".
	Code string
}

// StalenessState is what [ComputeStalenessState] measured.
type StalenessState struct {
	// Current maps each page identifier -- locale-prefixed when locales
	// are configured, matching the hash store's keys -- to its full
	// current hash entry.
	Current staleness.Store
	// Stored is the loaded baseline, the hash store's own contents.
	Stored staleness.Store
	// ErrorPages maps a page identifier to the lint code of its
	// outstanding error: "STALE001" or "DRIFT001".
	ErrorPages map[string]string
}

// machineOwnedKeys returns the locale-prefixed page keys whose description is
// machine-owned.
//
// These pages are exempt from the STALE001/DRIFT001 baseline hold: their
// description is a machine placeholder (recognized by the ownership predicate
// via template match or the recorded seed hash), so holding the baseline would
// deadlock -- they cannot be hand-fixed. A hand-described generated page
// (whose text is NOT machine-classified) is absent from this set and therefore
// receives full staleness protection.
func machineOwnedKeys(
	allDocs map[string]docs.Doc,
	dirPath string,
	cliStructure *strictclisupport.Structure,
	localePrefix string,
) (map[string]bool, error) {
	stored, err := staleness.LoadHashes(dirPath)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for relPath, doc := range allDocs {
		key := relPath
		if localePrefix != "" {
			key = localePrefix + "/" + relPath
		}
		owned, err := ownership.IsMachineOwned(
			relPath, doc.Frontmatter, stored[key].SeedHash, cliStructure,
		)
		if err != nil {
			return nil, err
		}
		if owned {
			keys[key] = true
		}
	}
	return keys, nil
}

// schemaHashes are the per-CLI-page schema hashes, keyed by each page's
// filename, computed from the per-command schema slices.
func schemaHashes(cliSchema *strictclisupport.Structure) (map[string]string, error) {
	hashes := map[string]string{}
	if cliSchema == nil {
		return hashes, nil
	}
	for _, command := range cliSchema.Commands {
		hash, err := staleness.ComputeSchemaHash(strictclisupport.PlainValue(command))
		if err != nil {
			return nil, err
		}
		hashes["cli-"+strictclisupport.CommandName(command)+".md"] = hash
	}
	for _, group := range cliSchema.Groups {
		hash, err := staleness.ComputeSchemaHash(strictclisupport.PlainValue(group))
		if err != nil {
			return nil, err
		}
		hashes["cli-"+strictclisupport.CommandName(group)+".md"] = hash
	}
	return hashes, nil
}

// localePrefixOf is the locale segment the hash store keys a page under: the
// first declared locale's code, and "" for a project declaring none.
func localePrefixOf(projectConfig map[string]any) string {
	locales := configList(projectConfig, "locales")
	if len(locales) == 0 {
		return ""
	}
	first, isMap := locales[0].(map[string]any)
	if !isMap {
		return ""
	}
	return configString(first, "code", "")
}

// prefixKeys returns a map with every key prefixed by the locale segment,
// leaving it untouched when the project declares no locale.
func prefixKeys[V any](values map[string]V, localePrefix string) map[string]V {
	if localePrefix == "" {
		return values
	}
	prefixed := make(map[string]V, len(values))
	for key, value := range values {
		prefixed[localePrefix+"/"+key] = value
	}
	return prefixed
}

// ComputeStalenessState computes the current page hashes and the pages frozen
// in an error state.
//
// It runs the same content, description, source-docstring and schema hashing
// that [CheckDocs] uses for STALE001/DRIFT001 detection, but never writes the
// hash store.
//
// projectConfig may be nil, in which case it is loaded from selfdoc.json.
func ComputeStalenessState(
	dirPath string, projectConfig map[string]any, handle *effects.Handle,
) (StalenessState, error) {
	if projectConfig == nil {
		loaded, err := config.Load(dirPath)
		if err != nil {
			return StalenessState{}, err
		}
		projectConfig = loaded
	}
	if projectConfig == nil {
		return StalenessState{}, errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.",
		)
	}

	docsDir := filepath.Join(
		dirPath, strings.TrimRight(configString(projectConfig, "docs", layout.DocsDefault), "/"),
	)
	if !isDir(docsDir) {
		return StalenessState{}, fmt.Errorf(
			"Docs directory '%s' not found.",
			configString(projectConfig, "docs", layout.DocsDefault),
		)
	}

	res, err := resolver.MakeResolver(projectConfig, dirPath, handle)
	if err != nil {
		return StalenessState{}, err
	}
	validNames, err := docs.ValidNames(projectConfig)
	if err != nil {
		return StalenessState{}, err
	}

	allDocs, err := docs.ResolveAll(projectConfig, "", dirPath, nil, handle)
	if err != nil {
		return StalenessState{}, err
	}
	_, resolvedDirectives, err := validateDirectives(allDocs, res, validNames, "", true)
	if err != nil {
		return StalenessState{}, err
	}

	driftDirectives := driftDirectivesOf(resolvedDirectives)

	cliSchema, err := strictclisupport.ReadSchemaJSON(dirPath)
	if err != nil {
		return StalenessState{}, err
	}
	hashes, err := schemaHashes(cliSchema)
	if err != nil {
		return StalenessState{}, err
	}

	localePrefix := localePrefixOf(projectConfig)
	prefixedDocs := prefixKeys(docs.StalenessDocs(allDocs), localePrefix)
	prefixedDirectives := prefixKeys(driftDirectives, localePrefix)
	prefixedSchema := prefixKeys(hashes, localePrefix)

	// Machine-owned pages are exempt from the staleness/drift hold (see
	// [CheckDocs]); the exempt set comes from the ownership predicate,
	// prefixed to line up with the docs.
	skeletonPages, err := machineOwnedKeys(allDocs, dirPath, cliSchema, localePrefix)
	if err != nil {
		return StalenessState{}, err
	}

	current, err := staleness.ComputeCurrentHashes(
		prefixedDocs, dirPath, prefixedDirectives, prefixedSchema,
	)
	if err != nil {
		return StalenessState{}, err
	}
	staleWarnings, driftWarnings, err := staleness.UpdateHashes(
		prefixedDocs, dirPath, true,
		prefixedDirectives, prefixedSchema, skeletonPages, handle,
	)
	if err != nil {
		return StalenessState{}, err
	}

	errorPages := map[string]string{}
	for _, warning := range staleWarnings {
		errorPages[warning.Page] = "STALE001"
	}
	for _, warning := range driftWarnings {
		if _, present := errorPages[warning.Page]; !present {
			errorPages[warning.Page] = "DRIFT001"
		}
	}

	stored, err := staleness.LoadHashes(dirPath)
	if err != nil {
		return StalenessState{}, err
	}
	return StalenessState{Current: current, Stored: stored, ErrorPages: errorPages}, nil
}

// driftDirectivesOf groups the resolved directives by the page they sit on, in
// the narrowed form the drift measurement reads.
func driftDirectivesOf(resolvedDirectives []ResolvedDirective) map[string][]staleness.PageDirective {
	grouped := map[string][]staleness.PageDirective{}
	for _, resolved := range resolvedDirectives {
		grouped[resolved.File] = append(
			grouped[resolved.File], resolved.StalenessDirective(),
		)
	}
	return grouped
}

// AcceptBaselines advances the stored baseline of each named page to its
// current hashes.
//
// A deliberate, auditable human action meaning "reviewed: the page content
// changed but the existing frontmatter description is still accurate". Each
// named page must currently be frozen in a STALE001/DRIFT001 error state;
// accepting advances its baseline exactly as if the description had been
// rewritten, so the next check passes for that page.
//
// pages are page identifiers exactly as `selfdoc check` shows them (for
// instance "en/cli-index.md"). projectConfig may be nil, in which case it is
// loaded from selfdoc.json.
//
// Nothing is written when any named page is invalid: a page that is not a
// documentation page of this project, one with no baseline yet, and one that
// is not currently stale or drifted are each refused, and one refusal refuses
// the whole call.
func AcceptBaselines(
	pages []string, dirPath string, projectConfig map[string]any, handle *effects.Handle,
) ([]AcceptedBaseline, error) {
	seen := map[string]bool{}
	var ordered []string
	for _, page := range pages {
		if !seen[page] {
			seen[page] = true
			ordered = append(ordered, page)
		}
	}
	if len(ordered) == 0 {
		return nil, &AcceptError{
			Message: "No page named. Name at least one page to accept.",
		}
	}

	state, err := ComputeStalenessState(dirPath, projectConfig, handle)
	if err != nil {
		return nil, err
	}

	var refusals []string
	for _, page := range ordered {
		switch {
		case !hasKey(state.Current, page):
			refusals = append(refusals, fmt.Sprintf(
				"'%s': not a documentation page in this project "+
					"(name it exactly as shown in 'selfdoc check' output, "+
					"e.g. 'en/index.md')", page,
			))
		case !hasKey(state.Stored, page):
			refusals = append(refusals, fmt.Sprintf(
				"'%s': has no baseline yet -- run 'selfdoc gen' or "+
					"'selfdoc check' to record one before accepting", page,
			))
		case state.ErrorPages[page] == "":
			refusals = append(refusals, fmt.Sprintf(
				"'%s': is not stale or drifted -- nothing to accept. An "+
					"edited frontmatter description clears a STALE001 or "+
					"DRIFT001 finding on its own, so a page whose "+
					"description was just rewritten is already cleared and "+
					"needs no accept; accept is for the other course, where "+
					"the description was reviewed and left as it is", page,
			))
		}
	}
	if len(refusals) > 0 {
		return nil, &AcceptError{
			Message: "Cannot accept baseline:\n  " + strings.Join(refusals, "\n  "),
		}
	}

	var accepted []AcceptedBaseline
	for _, page := range ordered {
		state.Stored[page] = state.Current[page]
		accepted = append(accepted, AcceptedBaseline{
			Page: page, Code: state.ErrorPages[page],
		})
	}
	if err := staleness.SaveHashes(state.Stored, dirPath, handle); err != nil {
		return nil, err
	}
	return accepted, nil
}

// hasKey reports whether a hash store carries an entry for page, which is a
// different question from the entry being empty: the Python's `page not in
// hashes` test is about the key.
func hasKey(store staleness.Store, page string) bool {
	_, present := store[page]
	return present
}
