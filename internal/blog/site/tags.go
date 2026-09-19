package site

import (
	"regexp"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// tagVersionPattern splits a version tag into its family prefix and its
// version. The prefix is lazy under the end anchor, so the optional "v" is
// taken by the version half wherever one is there: "demo@v0.3.1" is the
// family "demo@" at version "0.3.1", not the family "demo@v".
var tagVersionPattern = regexp.MustCompile(
	`^(.*?)v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-+]+)?)$`,
)

// ParseVersionTag splits tag into its family prefix and its version,
// reporting false when it is not a version tag at all.
//
//	"v1.2.3"            -> "", "1.2.3"
//	"demo@v0.3.1"       -> "demo@", "0.3.1"
//	"mypkg/v2.0.0-rc.1" -> "mypkg/", "2.0.0-rc.1"
func ParseVersionTag(tag string) (family, version string, ok bool) {
	match := tagVersionPattern.FindStringSubmatch(util.PythonStrip(tag))
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

// ResolveProjectTag returns the tag that names version for this project.
//
// Tag resolution used to be "the repository's newest tag by creation date",
// which is wrong in any repo that releases more than one thing: a sibling
// package released an hour later owns the newest tag, and the assembly then
// builds that sibling's ref under this project's slug. That shipped a 404 stub
// to the live site once.
//
// The version being dispatched decides instead: the tag has to carry that
// version, whatever family prefix it wears. Two families at the same version
// is a hard error rather than a coin flip, and a version with no tag is a hard
// error rather than a fallback onto something newer.
func ResolveProjectTag(tags []string, version string) (string, error) {
	if version == "" {
		return "", errorf(
			"cannot resolve a release tag without a version; set 'version' in " +
				"selfdoc.json or run this from a project with a detectable version",
		)
	}
	families := make([]string, 0, len(tags))
	matches := make([]string, 0)
	for _, tag := range tags {
		family, parsed, ok := ParseVersionTag(tag)
		if !ok {
			continue
		}
		families = append(families, family)
		if parsed == version {
			matches = append(matches, tag)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", errorf(
			"ambiguous release tag for version %s: %s. Two tag families carry "+
				"the same version, so the dispatch cannot tell which one is "+
				"this project's.",
			version, strings.Join(sortedStrings(matches), ", "),
		)
	}

	known := ""
	if distinct := sortedUnique(families); len(distinct) > 0 {
		known = " Tag families in this repo: " + joinReprs(distinct) + "."
	}
	return "", errorf(
		"no git tag names version %s.%s Release this project before "+
			"dispatching an assembly rebuild -- the assembly builds the tag, "+
			"so an untagged version would publish the wrong docs.",
		version, known,
	)
}

// ListRepoTags returns the repository's tags, newest creation date first.
func ListRepoTags(cwd string, handle *effects.Handle) ([]string, error) {
	result, err := handle.Run(
		[]string{
			"git", "for-each-ref", "--sort=-creatordate",
			"--format=%(refname:short)", "refs/tags",
		},
		effects.Cwd(cwd), effects.CaptureOutput(),
		effects.Timeout(30*time.Second), effects.Read(),
	)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errorf("failed to list git tags: %s", util.PythonStrip(string(result.Stderr)))
	}
	tags := make([]string, 0)
	for _, line := range util.PythonSplitLines(string(result.Stdout)) {
		if trimmed := util.PythonStrip(line); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags, nil
}

// CheckVersionIsDeclared returns an error unless version is the version the
// build will actually produce.
//
// Membership in the versions array was never the question. The assembly builds
// the last entry and records the dispatch under the version the payload
// carries, so dispatching a version that is merely present in the array
// publishes the newest version's docs under the dispatched version's name --
// silently, and for as long as nobody compares the two. The two have to be the
// same version, and [BuildTargetVersion] is what says which one the build
// produces.
func CheckVersionIsDeclared(cfg map[string]any, version string) error {
	// A project that declares it has no public version is dispatched under
	// the literal [config.UnversionedVersion]. Its loaded config carries one
	// anonymous 'versions' entry, which the rewrite derives from the
	// declaration rather than from anything an author wrote, so checking the
	// dispatch against that array would refuse the only version such a
	// project can be dispatched under.
	if config.IsUnversioned(cfg) {
		if version != config.UnversionedVersion {
			return errorf(
				"selfdoc.json declares 'unversioned': true, so this project "+
					"has no public version and is dispatched as %s. Version "+
					"%s would record docs under a version nobody released. "+
					"Remove the declaration and declare 'versions' if the "+
					"project does have one.",
				util.PythonRepr(config.UnversionedVersion), version,
			)
		}
		return nil
	}
	entries, _ := asList(cfg["versions"])
	declared := make([]string, 0, len(entries))
	for _, entryAny := range entries {
		entry, ok := asTable(entryAny)
		if !ok {
			continue
		}
		if !pythonTruthy(entry["version"]) {
			continue
		}
		declared = append(declared, util.PythonStr(entry["version"]))
	}
	if len(declared) == 0 {
		return errorf(
			"selfdoc.json declares no versions; the assembly has nothing to " +
				"build. Add the released version to 'versions'.",
		)
	}
	target, err := BuildTargetVersion(cfg, "")
	if err != nil {
		return err
	}
	if version != target {
		return errorf(
			"version %s is not the version the assembly would build. "+
				"selfdoc.json's newest declared version is %s (declared: %s), "+
				"and the build takes the newest one -- so this dispatch would "+
				"publish %s's docs recorded under the name %s. Make %s the "+
				"last entry of 'versions'.",
			version, target, strings.Join(declared, ", "), target, version, version,
		)
	}
	return nil
}

// VersionLabel renders a version the way a summary line names it.
//
// A released version reads as "v1.2.3"; the unversioned literal reads as
// "(unversioned)", because "vunversioned" names nothing and a summary that
// prints it invites the reader to look for a release under that name.
func VersionLabel(version string) string {
	if version == config.UnversionedVersion {
		return "(" + config.UnversionedVersion + ")"
	}
	return "v" + version
}
