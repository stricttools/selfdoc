// Package excludes is the single authority for which source paths a
// project's docs cover.
//
// Every source walk in selfdoc -- page generation, coverage counting, and the
// list-modules directive -- decides the same way which files and directories
// are part of the documented project. Keeping the patterns and the matcher
// here means a path excluded from the generated pages cannot be advertised by
// a listing directive, and vice versa.
//
// The inputs are two: [DefaultExcludes], applied everywhere, and the
// gen.exclude list from selfdoc.json, which a project adds to it. [SkipDirs]
// is separate and unconditional: environment and build directories that are
// never source, pruned from a walk before any pattern is tested. [ScratchDirs]
// are pruned the same way, at the project root only.
package excludes

import (
	"path/filepath"
	"strings"
)

// DefaultExcludes are the exclusion patterns always applied in addition to
// the project-configured ones. They are matched against both the full
// relative path and the basename, so "test_*" matches test_core.py at any
// depth.
var DefaultExcludes = []string{
	"test_*",
	"*_test.*",
	"__pycache__",
	"tests",
}

// SkipDirs are the directory names always pruned during a source walk. A
// walker drops them before descending, so nothing inside them is ever read.
var SkipDirs = map[string]bool{
	".venv": true, "venv": true, "node_modules": true, "__pycache__": true,
	".git": true, ".hg": true, ".svn": true, "dist": true, "build": true,
	"_build": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	".ruff_cache": true, ".zig-cache": true, "zig-cache": true,
}

// ShouldSkipDir reports whether a directory name should be pruned during a
// source walk.
func ShouldSkipDir(dirname string) bool {
	if SkipDirs[dirname] {
		return true
	}
	// Also skip directories ending in .egg-info (mylib.egg-info, say)
	return strings.HasSuffix(dirname, ".egg-info")
}

// ScratchDirs are the scratch directories a project keeps at its root:
// experiments/ for throwaway probes and screenshots/ for images taken while
// verifying work. Nothing in them is the project's source, so every walk of
// a project's tree prunes them -- but only at the project root, because a
// directory of the same name deeper down is ordinary source.
var ScratchDirs = map[string]bool{"experiments": true, "screenshots": true}

// IsScratchDir reports whether dir is one of [ScratchDirs] directly inside
// projectRoot. Both paths are compared absolute and cleaned, so a walk rooted
// at a relative path answers the same as one rooted at an absolute one.
func IsScratchDir(projectRoot, dir string) bool {
	if !ScratchDirs[filepath.Base(dir)] {
		return false
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return false
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return filepath.Dir(absolute) == root
}

// GoToolchainIgnoresDir reports whether the Go toolchain ignores a directory
// of this name, so nothing under it is part of any Go package.
//
// cmd/go never builds a directory named "testdata" or "vendor", nor one whose
// name begins with "." or "_". Documenting such a directory as a package
// advertises code that "go build ./..." does not compile.
func GoToolchainIgnoresDir(name string) bool {
	if name == "testdata" || name == "vendor" {
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// GoToolchainIgnoresPath reports whether any component of a slash-separated
// relative directory path is ignored by the Go toolchain.
//
// A path of "." names the walk's own root, which no component test applies to.
func GoToolchainIgnoresPath(relDir string) bool {
	if relDir == "" || relDir == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(relDir), "/") {
		if part == "" || part == "." {
			continue
		}
		if GoToolchainIgnoresDir(part) {
			return true
		}
	}
	return false
}

// IsExcluded reports whether a relative path matches any exclusion glob.
//
// A "**/" prefix means "match at any depth": the prefix is stripped and the
// rest is tested against the whole path, the basename, and every directory
// component. A plain pattern is tested the same three ways, and then -- when
// stripping changed it -- once more in its original spelling against the
// whole path.
func IsExcluded(relPath string, excludePatterns []string) bool {
	// Normalise to forward slashes for consistent matching
	normalized := strings.ReplaceAll(relPath, string(filepath.Separator), "/")
	parts := strings.Split(normalized, "/")
	basename := parts[len(parts)-1]
	dirParts := parts[:len(parts)-1]
	for _, pattern := range excludePatterns {
		// Strip leading **/ for "any depth" semantics
		stripped := pattern
		for strings.HasPrefix(stripped, "**/") {
			stripped = stripped[3:]
		}

		if Match(normalized, stripped) {
			return true
		}
		if Match(basename, stripped) {
			return true
		}
		// Check each directory component
		for _, part := range dirParts {
			if Match(part, stripped) {
				return true
			}
		}
		// Also try the original pattern against the full path
		if pattern != stripped && Match(normalized, pattern) {
			return true
		}
	}
	return false
}

// PatternsFor returns the full exclusion pattern list for a project: the
// defaults plus its gen.exclude entries. config is a loaded selfdoc.json.
func PatternsFor(config map[string]any) []string {
	patterns := make([]string, 0, len(DefaultExcludes))
	patterns = append(patterns, DefaultExcludes...)
	gen, _ := config["gen"].(map[string]any)
	exclude, _ := gen["exclude"].([]any)
	for _, entry := range exclude {
		if text, ok := entry.(string); ok {
			patterns = append(patterns, text)
		}
	}
	return patterns
}

// Match reports whether name matches the shell pattern pat, reproducing
// Python's fnmatch.fnmatch on a POSIX filesystem -- which is what every
// exclusion pattern in the fleet was written against, and which
// path/filepath.Match does NOT reproduce.
//
// The whole name must match. The syntax:
//
//   - "*" matches any run of characters, INCLUDING "/" -- the one difference
//     from filepath.Match that changes real answers, since "*_test.*" has to
//     reach a file at any depth of a relative path.
//   - "?" matches one character, again including "/".
//   - "[seq]" matches one character in seq and "[!seq]" one not in seq. A
//     "]" first in the set is that character; a "-" first or last is that
//     character; "x-y" is the inclusive range, and a reversed range matches
//     nothing. An unterminated "[" is the literal character.
//   - Nothing quotes a metacharacter, exactly as Python documents.
//
// Matching is case-sensitive: Python case-normalizes through
// os.path.normcase, which is the identity on POSIX.
func Match(name, pat string) bool {
	return matchRunes([]rune(name), []rune(pat))
}

// matchRunes walks the pattern with one backtrack point per "*", which is
// the whole of glob matching: on a mismatch the most recent star consumes
// one more character and the walk resumes after it.
func matchRunes(name, pat []rune) bool {
	var (
		n, p     int
		starPat  = -1
		starName int
	)
	for n < len(name) {
		if p < len(pat) {
			switch pat[p] {
			case '*':
				// Consecutive stars say what one says.
				for p < len(pat) && pat[p] == '*' {
					p++
				}
				starPat, starName = p, n
				continue
			case '?':
				p++
				n++
				continue
			case '[':
				if matched, next, ok := matchClass(pat, p, name[n]); ok {
					if matched {
						p = next
						n++
						continue
					}
					// A terminated class that does not match this
					// character: fall through to the backtrack below.
					if starPat >= 0 {
						break
					}
					return false
				}
				// An unterminated "[" is the literal character.
				if name[n] == '[' {
					p++
					n++
					continue
				}
				if starPat < 0 {
					return false
				}
			default:
				if pat[p] == name[n] {
					p++
					n++
					continue
				}
				if starPat < 0 {
					return false
				}
			}
		}
		if starPat < 0 {
			return false
		}
		// Backtrack: the last star takes one more character.
		starName++
		n, p = starName, starPat
	}
	for p < len(pat) && pat[p] == '*' {
		p++
	}
	return p == len(pat)
}

// matchClass matches one character against the bracket expression starting
// at pat[start] (which is "["). It returns whether the character matched,
// the pattern index just past the expression, and whether the expression is
// terminated at all -- an unterminated "[" is not a class, and the caller
// treats it as a literal.
func matchClass(pat []rune, start int, c rune) (matched bool, next int, terminated bool) {
	// Python's scan for the closing bracket: a leading "!" and then a
	// leading "]" are both part of the set rather than terminators.
	i := start + 1
	j := i
	if j < len(pat) && pat[j] == '!' {
		j++
	}
	if j < len(pat) && pat[j] == ']' {
		j++
	}
	for j < len(pat) && pat[j] != ']' {
		j++
	}
	if j >= len(pat) {
		return false, start, false
	}
	body := pat[i:j]
	next = j + 1

	negated := false
	if len(body) > 0 && body[0] == '!' {
		negated = true
		body = body[1:]
	}
	// A negated empty set matches any character; the positive empty set
	// cannot occur, because the scan above folds "[]" into an
	// unterminated bracket.
	if len(body) == 0 {
		return negated, next, true
	}

	found := false
	for k := 0; k < len(body); {
		// A "-" forms a range only with a character on each side; first
		// or last in the set it is the literal character. After a range
		// the scan resumes past it, so "a-c-e" is the range a-c plus a
		// literal "-" plus a literal "e", as Python reads it.
		if k+2 < len(body) && body[k+1] == '-' {
			if body[k] <= c && c <= body[k+2] {
				found = true
			}
			k += 3
			continue
		}
		if body[k] == c {
			found = true
		}
		k++
	}
	if negated {
		return !found, next, true
	}
	return found, next, true
}
