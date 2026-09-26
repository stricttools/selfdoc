package vocabulary

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
)

// Published is one project's vocabulary as its manifest carries it onto a
// site: the words it accepts with their aliases, and the patterns it rejects
// with their kinds. Meanings and reasons stay in the project.
type Published struct {
	// Project is the project's slug on the site.
	Project string
	// Accepted are the words the project accepts.
	Accepted []Accepted
	// Rejected are the patterns the project rejects.
	Rejected []Rejected
}

// CrossConflict is one rejected pattern of one project covering a word that
// another project, or the baseline, accepts: two vocabularies one site cannot
// hold at once.
type CrossConflict struct {
	// Rejecting is the slug of the project whose pattern covers the word.
	Rejecting string
	// Rejected is the covering pattern.
	Rejected Rejected
	// Accepting is the slug of the project accepting the word, or
	// [BaselineSource] when the baseline accepts it.
	Accepting string
	// Accepted is the accepting entry.
	Accepted Accepted
	// Spelling is the covered spelling: the entry's word or one of its
	// aliases.
	Spelling string
}

// Message states the conflict, naming both sides, the word, the pattern and
// the fixes each side owns.
func (c CrossConflict) Message() string {
	narrow := fmt.Sprintf(
		"narrow the pattern in %s with 'selfdoc vocabulary remove %s', then reject the specific words meant, one at a time, with 'selfdoc vocabulary reject <word> --kind word --reason <text>'",
		c.Rejecting, c.Rejected.Pattern)
	if c.Accepting == BaselineSource {
		return fmt.Sprintf(
			"%s rejects the %s %q, which covers %q, a word %s accepts. %s, so %s.",
			c.Rejecting, c.Rejected.Kind, c.Rejected.Pattern, c.Spelling, BaselineSource,
			baselineIsSelfdocs, narrow)
	}
	return fmt.Sprintf(
		"%s rejects the %s %q, which covers %q, a word %s accepts, so one site would both reject and accept it. Either %s; or remove the word in %s with 'selfdoc vocabulary remove %s'.",
		c.Rejecting, c.Rejected.Kind, c.Rejected.Pattern, c.Spelling, c.Accepting,
		narrow, c.Accepting, c.Accepted.Word)
}

// Involves reports whether slug is either side of the conflict.
func (c CrossConflict) Involves(slug string) bool {
	return c.Rejecting == slug || c.Accepting == slug
}

// SiteConflicts checks every project's rejected patterns against every other
// project's accepted words and against the baseline's, in one pass, and
// returns every conflict, ordered by the rejecting project, then the
// accepting one.
//
// A project's own entries are not compared with each other or with the
// baseline's rejections here: those disagreements are the project's lints
// and load errors, reported where the project can see them.
func SiteConflicts(projects []Published, baseline List) []CrossConflict {
	ordered := append([]Published(nil), projects...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Project < ordered[j].Project })
	var conflicts []CrossConflict
	for _, rejecting := range ordered {
		for _, rejected := range rejecting.Rejected {
			matcher := NewMatcher(rejected)
			for _, accepting := range ordered {
				if accepting.Project == rejecting.Project {
					continue
				}
				conflicts = append(conflicts, covered(matcher, rejecting.Project, accepting.Project, accepting.Accepted)...)
			}
			conflicts = append(conflicts, covered(matcher, rejecting.Project, BaselineSource, baseline.Accepted)...)
		}
	}
	return conflicts
}

// covered is every accepted entry the matcher covers, one conflict per entry.
func covered(matcher Matcher, rejecting, accepting string, accepted []Accepted) []CrossConflict {
	var conflicts []CrossConflict
	for _, entry := range accepted {
		for _, written := range entry.Spellings() {
			if matcher.Covers(written) {
				conflicts = append(conflicts, CrossConflict{
					Rejecting: rejecting, Rejected: matcher.Rejected,
					Accepting: accepting, Accepted: entry, Spelling: written,
				})
				break
			}
		}
	}
	return conflicts
}

// ConflictsError is a set of cross-project conflicts refusing a publish.
type ConflictsError struct {
	// Action names what was refused, for the first line.
	Action string
	// Conflicts are every conflict found.
	Conflicts []CrossConflict
}

func (e *ConflictsError) Error() string {
	lines := []string{fmt.Sprintf(
		"Refusing %s: the vocabularies of projects on one site disagree about %d word(s), and nothing was written.",
		e.Action, len(e.Conflicts))}
	for _, conflict := range e.Conflicts {
		lines = append(lines, "  - "+conflict.Message())
	}
	return strings.Join(lines, "\n")
}

// CheckIncoming refuses a project arriving on a site whose vocabulary
// disagrees with another project's there or with the baseline: its pattern
// covering their word, or their pattern covering its word. others are the
// site's other projects; an entry naming incoming's own slug is ignored,
// since incoming replaces it.
func CheckIncoming(incoming Published, others []Published, action string) error {
	baseline, err := LoadBaseline()
	if err != nil {
		return err
	}
	projects := []Published{incoming}
	for _, other := range others {
		if other.Project != incoming.Project {
			projects = append(projects, other)
		}
	}
	var involved []CrossConflict
	for _, conflict := range SiteConflicts(projects, baseline) {
		if conflict.Involves(incoming.Project) {
			involved = append(involved, conflict)
		}
	}
	if len(involved) > 0 {
		return &ConflictsError{Action: action, Conflicts: involved}
	}
	return nil
}

// LoadTermsAt reads one terms file at path, naming it source in every
// diagnostic. A missing file is an empty list.
func LoadTermsAt(path, source string) (List, error) {
	return loadTermsFile(path, source)
}

// LoadProject reads a project's own terms file, and nothing else: no baseline,
// no review list, no conflict check. It is what a manifest publishes. A
// project with no terms file has an empty list.
func LoadProject(baseDir string) (List, error) {
	return loadTermsFile(layout.Path(baseDir, layout.TermsRel), layout.TermsRel)
}
