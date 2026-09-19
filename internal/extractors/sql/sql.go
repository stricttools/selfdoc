// Package sql resolves selfdoc's directives against PostgreSQL DDL.
//
// No database connection is required: the DDL document is read with patterns.
// The four directives it serves are ref, prose-desc, table-schema and
// table-config.
//
// # Never auto-detected
//
// Detection always answers false. A .sql file in a repository says nothing
// about what the repository is -- a Python service, a Go binary and a
// Terraform module can all carry one -- so a SQL source path is declared in
// selfdoc.json and never inferred.
//
// # What the document says about itself
//
// The documentation of a schema is its COMMENT ON statements, and they are the
// only prose a DDL document carries: a table's description, a column's, a
// view's, a type's and a function's each come from one, in either string
// spelling SQL offers -- single-quoted with ” for an embedded quote, or
// dollar-quoted with an optional tag. A comment set to NULL removes a comment
// rather than being one.
//
// Comments are stripped before anything reads the document's structure, with
// string literals kept whole, so a -- inside a literal is not mistaken for the
// start of a comment.
package sql

import (
	"os"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// Extractor reads PostgreSQL DDL.
type Extractor struct {
	extractors.Base
}

// New builds the SQL extractor.
func New() extractors.Extractor {
	extractor := &Extractor{}
	extractor.Base = extractors.NewBase("sql", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"table-config": extractors.HandleTableConfig,
	})
	return extractor
}

func init() { extractors.Register("sql", New) }

// Detect always reports false: SQL is declared in selfdoc.json, never
// auto-detected.
func (e *Extractor) Detect(string) bool { return false }

// FileExtensions is the one extension a DDL document carries.
func (e *Extractor) FileExtensions() []string { return []string{".sql"} }

// ResolvePath resolves a directive's path argument to a SQL file or directory.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveSQLPath(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists the objects a DDL document creates: its tables, views,
// types and functions, by their unqualified names.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}
	return extractSymbols(source), nil
}

// SymbolDetails reports what a DDL document says about one function's
// parameters and return type. A table, a view and a type answer nothing, and so
// does a name the document does not create.
func (e *Extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}
	return functionSymbolDetails(stripComments(source), symbol, parseComments(source)), nil
}

// resolveSQLPath resolves a path argument to a SQL file or a directory of them,
// trying each declared source path as a prefix and then the base directory, and
// admitting a path written without its .sql extension.
func resolveSQLPath(pathArg string, sourcePaths []string, baseDir string) string {
	var candidates []string
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, pathArg))

	for _, candidate := range candidates {
		if extractors.IsDir(candidate) && hasSQLFile(candidate) {
			return candidate
		}
		if extractors.IsFile(candidate) {
			return candidate
		}
		if sqlCandidate := candidate + ".sql"; extractors.IsFile(sqlCandidate) {
			return sqlCandidate
		}
	}

	return ""
}

// hasSQLFile reports whether a directory holds any DDL document.
func hasSQLFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			return true
		}
	}
	return false
}
