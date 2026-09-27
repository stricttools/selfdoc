package check

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// statTrimSet is the wrapping punctuation and Markdown emphasis a prose token
// can carry. A statistic is recognized from the token's core, not from its
// decoration: "**42**", "(0.36.0)" and "1999." all reduce to their bare form.
const statTrimSet = "`*_~\"'“”‘’()[]{}<>,.;:!?…—–-"

// versionShaped matches a version token: either a "v"-prefixed number of any
// component count ("v2", "v1.5", "v0.36.0") or a bare dotted triple
// ("0.36.0"), each optionally followed by pre-release or build metadata
// ("1.0.0-alpha.1", "2.11.3+build.7").
//
// A bare two-component number is NOT version-shaped: "3.5" is far more often a
// measurement than a release, so it keeps counting as a statistic.
var versionShaped = regexp.MustCompile(
	`^(?:[vV][0-9]+(?:\.[0-9]+)*|[0-9]+\.[0-9]+\.[0-9]+)(?:[-+][0-9A-Za-z.]+)?$`,
)

// bareYear matches a four-digit standalone number. "1899" and "2100" are
// outside the calendar range this rule refuses and stay statistics;
// "2026-08-11" is not standalone.
var bareYear = regexp.MustCompile(`^[0-9]{4}$`)

// CountsAsStatistic reports whether a prose token is a concrete numeric data
// point.
//
// low-numeric-data-density measures how many quantities a page offers a citing model. A digit
// alone does not make a quantity: release versions and calendar years appear
// in almost every documentation page and say nothing about magnitude, count or
// proportion. Both are refused here, so a page whose only digits are "0.36.0"
// and "2026" reads as having no statistics -- which is the truth.
//
// word is a whitespace-delimited token from prose content, with any Markdown
// decoration still attached. Genuine quantities ("42", "3.5", "87%", "12ms")
// answer true; a token carrying no digit, a version-shaped token and a bare
// year answer false.
func CountsAsStatistic(word string) bool {
	if !strings.ContainsFunc(word, unicode.IsDigit) {
		return false
	}
	core := strings.Trim(word, statTrimSet)
	if core == "" {
		return false
	}
	if versionShaped.MatchString(core) {
		return false
	}
	if bareYear.MatchString(core) {
		year, err := strconv.Atoi(core)
		if err == nil && year >= 1900 && year <= 2099 {
			return false
		}
	}
	return true
}
