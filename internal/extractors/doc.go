// Package extractors defines the language-extractor protocol, the shared
// behavior every extractor embeds, and the registry that resolves a language
// name to its extractor.
//
// An extractor answers directives out of source code. The build pipeline hands
// it a directive name, the directive's attributes, its body lines, the
// project's declared source paths and the project's base directory; the
// extractor returns the Markdown that replaces the directive. It also answers
// the three discovery questions the coverage and quality measurements ask of a
// file: which symbols it exports, what one symbol's parameters and return value
// are, and what the module's own documentation says.
//
// # The base
//
// Base carries everything the language extractors share: the directive
// dispatch table, the no-op defaults for the optional protocol methods, and the
// formatting helpers that turn source-derived text into Markdown (symbol
// headings and spans, doc-comment renesting, Google-style docstring sections,
// the config-file tables, brace-block and comment-block scanning). A language
// package embeds Base, implements Detect, and populates the dispatch table.
//
// # The registry
//
// Each language package registers its own factory from an init function, so a
// consumer links a language in by importing its package. KnownLanguages is the
// authority on which names selfdoc ships an extractor for: a name on that list
// with no registered factory is a wiring mistake and is reported as an error
// rather than silently answered with a stub. A name that is not on the list is
// a language selfdoc has no extractor for, and NewStub answers it in band -- an
// unsupported-language directive renders an error marker on the page instead of
// aborting the build, which is what the unsupported-source-language lint reports at check time.
package extractors
