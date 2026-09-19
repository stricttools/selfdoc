package typescript

import (
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

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return util.PythonStrip(s) }

// pyLStrip is Python's str.lstrip() with no argument.
func pyLStrip(s string) string { return util.PythonLStrip(s) }

// pyRStrip is Python's str.rstrip() with no argument.
func pyRStrip(s string) string { return util.PythonRStrip(s) }

// splitExt splits path into its stem and its extension, reproducing Python's
// posixpath.splitext: the extension starts at the last dot of the last path
// element, and a leading run of dots belongs to the stem, so a dotfile has no
// extension.
func splitExt(path string) (string, string) {
	sepIndex := strings.LastIndex(path, "/")
	dotIndex := strings.LastIndex(path, ".")
	if dotIndex > sepIndex {
		filenameIndex := sepIndex + 1
		for filenameIndex < dotIndex {
			if path[filenameIndex] != '.' {
				return path[:dotIndex], path[dotIndex:]
			}
			filenameIndex++
		}
	}
	return path, ""
}
