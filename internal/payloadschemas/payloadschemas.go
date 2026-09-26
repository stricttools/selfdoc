// Package payloadschemas declares the JSON Schemas of selfdoc's machine-mode
// payloads.
//
// strictcli's machine mode (--json) writes one document to stdout -- the
// envelope -- and a command's machine output is the envelope's "payload"
// member. Every such command declares that payload's schema at registration
// time, and the framework validates the value against the declaration where it
// writes the envelope: a deviating document fails the run instead of reaching
// a consumer.
//
// The declarations are built through strictcli's own schema builders, which
// produce exactly the literal an author could have written by hand and pass
// the identical registration-time validation over the framework's closed
// keyword subset. --dump-schema publishes them verbatim, which makes each one
// the single artifact a consumer generates against.
package payloadschemas

import (
	"sort"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/selfdoc/internal/lints"
)

// merge folds every fragment into one schema object, later keys winning.
//
// The builders each produce one keyword (SchemaEnum builds {"enum": ...},
// SchemaType builds {"type": ...}), and a constrained scalar needs both, so
// this is how the two are written as one literal.
func merge(fragments ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, fragment := range fragments {
		for key, value := range fragment {
			out[key] = value
		}
	}
	return out
}

// LintCodes is the sorted set of lint codes selfdoc check can emit, derived
// from the shipped registry rather than restated here.
//
// Deriving it is the whole point: internal/lints's embedded document is the
// single place a code is declared, so registering one cannot leave the
// published contract behind.
func LintCodes() []any {
	codes := append([]string(nil), lints.Registered().Codes()...)
	sort.Strings(codes)
	out := make([]any, len(codes))
	for i, code := range codes {
		out[i] = code
	}
	return out
}

// LintSeverities is the closed set of severities a diagnostic carries.
func LintSeverities() []any { return []any{"error", "warning"} }

// Check is the payload of `selfdoc check`: one directive result per directive
// found, the coverage block (null when the project has no source to cover),
// every lint the run kept after suppression, and the exit code the command
// will terminate with.
func Check() map[string]any {
	directive := strictcli.SchemaObject(
		map[string]any{
			"file":      strictcli.SchemaType("string"),
			"line":      strictcli.SchemaType("integer"),
			"directive": strictcli.SchemaType("string"),
			"status": merge(
				strictcli.SchemaType("string"),
				strictcli.SchemaEnum("OK", "FAILED"),
			),
			"error": strictcli.SchemaType("string"),
		},
		[]string{"file", "line", "directive", "status", "error"},
		false,
	)

	coverage := strictcli.SchemaObject(
		map[string]any{
			"total_public":         strictcli.SchemaType("integer"),
			"referenced":           strictcli.SchemaType("integer"),
			"documented":           strictcli.SchemaType("integer"),
			"referenced_symbols":   strictcli.SchemaArray(strictcli.SchemaType("string")),
			"documented_symbols":   strictcli.SchemaArray(strictcli.SchemaType("string")),
			"unreferenced_symbols": strictcli.SchemaArray(strictcli.SchemaType("string")),
		},
		[]string{
			"total_public", "referenced", "documented",
			"referenced_symbols", "documented_symbols",
			"unreferenced_symbols",
		},
		false,
	)
	// A project with no source to cover emits null here, so the declaration
	// carries the type list rather than the bare object type.
	coverage["type"] = []any{"object", "null"}

	lint := strictcli.SchemaObject(
		map[string]any{
			"file": strictcli.SchemaType("string"),
			"line": strictcli.SchemaType("integer", "null"),
			"code": merge(
				strictcli.SchemaType("string"),
				map[string]any{"enum": LintCodes()},
			),
			"message": strictcli.SchemaType("string"),
			"severity": merge(
				strictcli.SchemaType("string"),
				map[string]any{"enum": LintSeverities()},
			),
		},
		[]string{"file", "line", "code", "message", "severity"},
		false,
	)

	return strictcli.SchemaObject(
		map[string]any{
			"directives": strictcli.SchemaArray(directive),
			"coverage":   coverage,
			"lints":      strictcli.SchemaArray(lint),
			"exit_code": merge(
				strictcli.SchemaType("integer"),
				strictcli.SchemaEnum(0, 1),
			),
		},
		[]string{"directives", "coverage", "lints", "exit_code"},
		false,
	)
}

// SpellCorpus is the payload of `selfdoc spell-corpus`: the sweep's inputs
// (how many words the vendored list and selfdoc's built-in baseline hold), one
// entry per project visited with how many words its own vocabulary accepts,
// and the corpus-wide flagged total. "error" is set instead
// of results for a project that could not be read.
func SpellCorpus() map[string]any {
	misspelling := strictcli.SchemaObject(
		map[string]any{
			"file":        strictcli.SchemaType("string"),
			"line":        strictcli.SchemaType("integer"),
			"column":      strictcli.SchemaType("integer"),
			"word":        strictcli.SchemaType("string"),
			"suggestions": strictcli.SchemaArray(strictcli.SchemaType("string")),
		},
		[]string{"file", "line", "column", "word", "suggestions"},
		false,
	)

	project := strictcli.SchemaObject(
		map[string]any{
			"project":        strictcli.SchemaType("string"),
			"pages":          strictcli.SchemaType("integer"),
			"accepted_terms": strictcli.SchemaType("integer"),
			"error":          strictcli.SchemaType("string", "null"),
			"misspellings":   strictcli.SchemaArray(misspelling),
		},
		[]string{"project", "pages", "accepted_terms", "error", "misspellings"},
		false,
	)

	return strictcli.SchemaObject(
		map[string]any{
			"root":           strictcli.SchemaType("string"),
			"baseline_terms": strictcli.SchemaType("integer"),
			"wordlist_words": strictcli.SchemaType("integer"),
			"projects":       strictcli.SchemaArray(project),
			"total":          strictcli.SchemaType("integer"),
		},
		[]string{
			"root", "baseline_terms", "wordlist_words",
			"projects", "total",
		},
		false,
	)
}

// Quality is the payload of `selfdoc quality`: one project's score.
//
// doc_ratio is null when there is no source to divide by, and next_step is
// null at tier 5 where nothing is left to do. The selfdoc block carries only
// has_selfdoc for a project that has no readable selfdoc.json.
func Quality() map[string]any {
	adoption := strictcli.SchemaObject(
		map[string]any{
			"has_selfdoc":       strictcli.SchemaType("boolean"),
			"auto_readme":       strictcli.SchemaType("boolean"),
			"auto_claude":       strictcli.SchemaType("boolean"),
			"custom_directives": strictcli.SchemaType("integer"),
			"has_posts":         strictcli.SchemaType("boolean"),
			"directive_count":   strictcli.SchemaType("integer"),
		},
		[]string{"has_selfdoc"},
		false,
	)

	return strictcli.SchemaObject(
		map[string]any{
			"project":    strictcli.SchemaType("string"),
			"path":       strictcli.SchemaType("string"),
			"tier":       merge(strictcli.SchemaType("integer"), strictcli.SchemaEnum(0, 1, 2, 3, 4, 5)),
			"tier_name":  strictcli.SchemaType("string"),
			"code_loc":   strictcli.SchemaType("integer"),
			"test_loc":   strictcli.SchemaType("integer"),
			"source_loc": strictcli.SchemaType("integer"),
			"doc_loc":    strictcli.SchemaType("integer"),
			"doc_files":  strictcli.SchemaType("integer"),
			"doc_ratio":  strictcli.SchemaType("number", "null"),
			// "-" is the grade of a project with no source to compare
			// against, which is not the same answer as failing.
			"content_grade": merge(
				strictcli.SchemaType("string"),
				strictcli.SchemaEnum("A", "B", "C", "D", "F", "-"),
			),
			"selfdoc":   adoption,
			"next_step": strictcli.SchemaType("string", "null"),
		},
		[]string{
			"project", "path", "tier", "tier_name", "code_loc", "test_loc",
			"source_loc", "doc_loc", "doc_files", "doc_ratio", "content_grade",
			"selfdoc", "next_step",
		},
		false,
	)
}

// LayoutDump is the payload of `selfdoc layout dump`: selfdoc's claim on a
// repository's tool-state directory, as the fleet check reads it.
//
// The directories are the one place a consumer learns which paths selfdoc
// owns, which side of the authorship line each sits on, whether the repository
// commits it, and what it used to be called -- so a description of the layout
// is generated from this rather than restated per repository.
func LayoutDump() map[string]any {
	directory := strictcli.SchemaObject(
		map[string]any{
			"name":             strictcli.SchemaType("string"),
			"path":             strictcli.SchemaType("string"),
			"side":             merge(strictcli.SchemaType("string"), strictcli.SchemaEnum("handwritten", "generated")),
			"commitment":       merge(strictcli.SchemaType("string"), strictcli.SchemaEnum("committed", "uncommitted")),
			"description":      strictcli.SchemaType("string"),
			"deprecated_names": strictcli.SchemaArray(strictcli.SchemaType("string")),
			"manifest_path":    strictcli.SchemaType("string"),
			"manifest_content": strictcli.SchemaType("string"),
		},
		[]string{
			"name", "path", "side", "commitment", "description", "deprecated_names",
			"manifest_path", "manifest_content",
		},
		false,
	)
	return strictcli.SchemaObject(
		map[string]any{
			"tool":          strictcli.SchemaType("string"),
			"root":          strictcli.SchemaType("string"),
			"manifest_file": strictcli.SchemaType("string"),
			"ignore_file":   strictcli.SchemaType("string"),
			"directories":   strictcli.SchemaArray(directory),
		},
		[]string{"tool", "root", "manifest_file", "ignore_file", "directories"},
		false,
	)
}

// LayoutMigrate is the payload of `selfdoc layout migrate`: every step of the
// move, whether a dry run recorded it or a live run performed it, and whether
// the move was committed.
func LayoutMigrate() map[string]any {
	move := strictcli.SchemaObject(
		map[string]any{
			"from": strictcli.SchemaType("string"),
			"to":   strictcli.SchemaType("string"),
		},
		[]string{"from", "to"},
		false,
	)
	rewrite := strictcli.SchemaObject(
		map[string]any{
			"path":    strictcli.SchemaType("string"),
			"changes": strictcli.SchemaArray(strictcli.SchemaType("string")),
		},
		[]string{"path", "changes"},
		false,
	)
	return strictcli.SchemaObject(
		map[string]any{
			"previous_root":         strictcli.SchemaType("string"),
			"root":                  strictcli.SchemaType("string"),
			"moves":                 strictcli.SchemaArray(move),
			"writes":                strictcli.SchemaArray(strictcli.SchemaType("string")),
			"rewrites":              strictcli.SchemaArray(rewrite),
			"deletes":               strictcli.SchemaArray(strictcli.SchemaType("string")),
			"removed_previous_root": strictcli.SchemaType("boolean"),
			"committed":             strictcli.SchemaType("boolean"),
		},
		[]string{"previous_root", "root", "moves", "writes", "rewrites", "deletes", "removed_previous_root", "committed"},
		false,
	)
}

// LayoutValidate is the payload of `selfdoc layout validate`: whether this
// repository's tool-state directory is laid out as declared, and every problem
// found when it is not.
func LayoutValidate() map[string]any {
	problem := strictcli.SchemaObject(
		map[string]any{
			"check":   merge(strictcli.SchemaType("string"), strictcli.SchemaEnum("ownership", "side", "hidden", "ignore-file")),
			"message": strictcli.SchemaType("string"),
		},
		[]string{"check", "message"},
		false,
	)
	return strictcli.SchemaObject(
		map[string]any{
			"root":     strictcli.SchemaType("string"),
			"ok":       strictcli.SchemaType("boolean"),
			"problems": strictcli.SchemaArray(problem),
		},
		[]string{"root", "ok", "problems"},
		false,
	)
}
