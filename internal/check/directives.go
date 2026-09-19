package check

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/catalog"
	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/strictclisupport"
	"github.com/stricttools/selfdoc/internal/util"
)

// errorMarkerPrefix opens the block-quoted note a resolver leaves in place of
// a directive it could not answer. A resolved output starting with it is a
// resolution failure rather than content.
const errorMarkerPrefix = "> *[selfdoc:"

// markerTrimSet is the punctuation stripped off both ends of an error marker
// to recover the message inside it -- Python's str.strip("> *[]").
const markerTrimSet = "> *[]"

// validateDirectives validates the directives across a set of documentation
// templates.
//
// Every template is parsed, every directive's attribute contract is enforced,
// and every directive is resolved; the per-directive OK/FAILED verdicts come
// back in page order.
//
// filePrefix is prepended to each page's path in the results, which is how a
// multi-version run labels an older version's findings ("[0.1.0] "). With
// collectResolved the successfully resolved directives come back too, carrying
// the source entry that answered them, which is what the coverage measurement
// and the drift measurement read.
//
// An attribute-contract violation and a hard resolver refusal both stop the
// walk: the first is a defect in the template and the second means the page
// lost content it asked for, and neither is a warning-level "this directive
// did not resolve".
func validateDirectives(
	docsDict map[string]docs.Doc,
	res *resolver.Resolver,
	validNames directives.NameSet,
	filePrefix string,
	collectResolved bool,
) ([]DirectiveResult, []ResolvedDirective, error) {
	var directiveResults []DirectiveResult
	var resolvedDirectives []ResolvedDirective

	for _, relPath := range sortedKeys(docsDict) {
		doc := docsDict[relPath]
		parsed, err := directives.ParseDirectives(doc.Raw, validNames)
		if err != nil {
			return nil, nil, err
		}
		displayFile := relPath
		if filePrefix != "" {
			displayFile = filePrefix + relPath
		}

		for _, directive := range parsed {
			fileLine := directive.LineNumber + doc.FrontmatterLines
			directiveStr := strings.TrimSpace(
				directive.Name + " " + renderAttrs(directive.Attrs, directive.AttrOrder),
			)
			// A hard error (exit 1) on an unknown or a missing
			// required attribute. Distinct from the resolution
			// failures below, which are warning-level.
			if err := catalog.ValidateDirectiveAttrs(
				directive.Name, directive.Attrs, displayFile, fileLine,
			); err != nil {
				return nil, nil, err
			}
			resolved, err := res.Resolve(
				directive.Name, directive.Attrs, directive.Body,
			)
			if err != nil {
				// Schema discovery ambiguity or absence is a
				// hard error, not a warning-level resolution
				// failure -- it propagates.
				var discovery *strictclisupport.SchemaDiscoveryError
				if errors.As(err, &discovery) {
					return nil, nil, err
				}
				directiveResults = append(directiveResults, DirectiveResult{
					File:      displayFile,
					Line:      fileLine,
					Directive: directiveStr,
					Outcome:   StatusFailed,
					Error:     err.Error(),
				})
				continue
			}
			if strings.HasPrefix(resolved, errorMarkerPrefix) {
				message := strings.Trim(resolved, markerTrimSet)
				message = strings.TrimPrefix(message, "selfdoc: ")
				directiveResults = append(directiveResults, DirectiveResult{
					File:      displayFile,
					Line:      fileLine,
					Directive: directiveStr,
					Outcome:   StatusFailed,
					Error:     message,
				})
				continue
			}
			directiveResults = append(directiveResults, DirectiveResult{
				File:      displayFile,
				Line:      fileLine,
				Directive: directiveStr,
				Outcome:   StatusOK,
			})
			if collectResolved && len(directive.Attrs) > 0 {
				resolvedDirectives = append(resolvedDirectives, ResolvedDirective{
					Name:        directive.Name,
					Attrs:       directive.Attrs,
					Content:     resolved,
					File:        relPath,
					SourceEntry: res.LastSourceEntry,
				})
			}
		}
	}

	return directiveResults, resolvedDirectives, nil
}

// renderAttrs renders a directive's attributes as the report shows them, in
// the order the source wrote them.
//
// The report quotes a directive back to its author, who reads the quote
// against the template they typed, so `ref path="." lang="go"` must not come
// back re-alphabetized. A key the order does not name -- which the parser does
// not produce, but a hand-built map could -- is rendered after the ordered
// ones, sorted, rather than dropped.
func renderAttrs(attrs map[string]string, order []string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	seen := make(map[string]bool, len(attrs))
	for _, key := range order {
		if _, ok := attrs[key]; ok && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(attrs)-len(keys))
	for key := range attrs {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+`="`+attrs[key]+`"`)
	}
	return strings.Join(parts, " ")
}

// resolveRootTemplates reads a project's root-file templates into the shape
// the docs walk produces, so the directives in them are validated too.
//
// The templates are underscore-prefixed and therefore skipped by the docs
// walk, but they carry directives that have to hold. Nothing is resolved here
// -- validateDirectives does its own resolution -- so the Resolved member
// carries the raw body.
//
// A template that is not on disk is skipped: gen reports that at gen time.
func resolveRootTemplates(config map[string]any, baseDir string) (map[string]docs.Doc, error) {
	rootFiles := stringList(config["root_files"])
	if len(rootFiles) == 0 {
		return nil, nil
	}

	result := map[string]docs.Doc{}
	for _, templatePath := range rootFiles {
		fullPath := filepath.Join(baseDir, templatePath)
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		content := string(raw)
		block, err := util.ReadFrontmatter(content, templatePath, util.KindPage)
		if err != nil {
			return nil, err
		}
		fmLineCount := len(strings.Split(content, "\n")) - len(strings.Split(block.Body, "\n"))
		result[templatePath] = docs.Doc{
			Frontmatter:      block.Values,
			Resolved:         block.Body,
			Raw:              block.Body,
			FrontmatterLines: fmLineCount,
		}
	}

	return result, nil
}
