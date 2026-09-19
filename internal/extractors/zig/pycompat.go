package zig

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// pySpace and pyWord are the in-package spellings of the base package's
// Python-equivalent character classes. Go's own \s and \w are ASCII-only,
// where Python's are Unicode-aware for text patterns, so every regex ported
// from the Python extractor is built from these instead.
const (
	pySpace = extractors.PySpaceClass
	pyWord  = extractors.PyWordClass
)

// pyNonWord is the complement of pyWord, which is what a word boundary is
// made of. It is built from the same members pyWord is, so the two cannot
// drift apart.
const pyNonWord = "[^" + util.PythonWordChars + "]"

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return util.PythonStrip(s) }

// baseName is the last element of path, reproducing Python's
// posixpath.basename.
func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// wordPattern matches word, whole, the way Python's \b does.
//
// Go's own \b is ASCII-only, so a doc comment written in a non-Latin script
// would break a ported \b pattern. The boundary is spelled out as a non-word
// character or the end of the text instead, which answers the same question
// for the one use these patterns have: does this text name this word at all.
func wordPattern(word string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?:^|` + pyNonWord + `)` + regexp.QuoteMeta(word) + `(?:` + pyNonWord + `|$)`)
}
