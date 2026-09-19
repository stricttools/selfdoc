package site

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// FilesManifestPath returns the path of slug's published-file record.
func FilesManifestPath(manifestsDir, slug string) string {
	return filepath.Join(manifestsDir, fmt.Sprintf("%s-files.json", slug))
}

// ParseFilesManifest returns owner -> published paths from the record text
// raw, naming source in every diagnostic.
//
// Empty text is an empty mapping: nothing has published anything yet, so there
// is nothing anybody is entitled to remove. Everything else is strict, because
// a record read wrong hands paths to the wrong publisher: malformed JSON, an
// unknown publisher and a path list that is not a list of strings are each a
// hard error, and so is a record written in the version-1 format, whose paths
// meant something else.
func ParseFilesManifest(raw string, source string) (map[string][]string, error) {
	if util.PythonStrip(raw) == "" {
		return map[string][]string{}, nil
	}
	decoded, err := config.DecodeDocument([]byte(raw))
	if err != nil {
		return nil, errorf("%s is not valid JSON: %v", source, err)
	}
	data, ok := decoded.(map[string]any)
	if !ok {
		return nil, errorf("%s must contain a JSON object", source)
	}
	version := data["schema_version"]
	if !isRecordVersion(version, FilesRecordVersion) {
		return nil, errorf(
			"%s is a version %s published-file record; this selfdoc writes "+
				"and reads version %d. Version 1 addressed every path from the "+
				"project's own subtree and put posts at '<slug>/posts/...'; "+
				"version 2 addresses every path from site/ and posts are "+
				"site-level, at 'blog/<post-slug>/...'. The two cannot be told "+
				"apart by reading them, so this one is refused rather than "+
				"reinterpreted. Either rewrite it -- prefix every documentation "+
				"path with '<slug>/' and re-address every post as "+
				"'blog/<post-slug>/...' -- or delete it together with the stale "+
				"site/<slug>/posts/ tree it describes, in which case the next "+
				"publish records what it produces and until then no publisher "+
				"is entitled to remove anything.",
			source, util.PythonRepr(version), FilesRecordVersion,
		)
	}
	// Python reads `data.get("owners") or {}`, so a falsy value -- absent,
	// null, an empty object and an empty array alike -- is an empty mapping and
	// never reaches the type check.
	ownersValue := data["owners"]
	if !pythonTruthy(ownersValue) {
		return map[string][]string{}, nil
	}
	owners, ok := ownersValue.(map[string]any)
	if !ok {
		return nil, errorf("%s: 'owners' must be a JSON object", source)
	}
	ownerNames := make([]string, 0, len(owners))
	for owner := range owners {
		ownerNames = append(ownerNames, owner)
	}
	sort.Strings(ownerNames)
	unknown := make([]string, 0)
	for _, owner := range ownerNames {
		if !containsString(PublishOwners, owner) {
			unknown = append(unknown, owner)
		}
	}
	if len(unknown) > 0 {
		return nil, errorf(
			"%s records paths under unknown publisher(s) %s; known publishers "+
				"are %s.",
			source, joinReprs(unknown), strings.Join(PublishOwners, ", "),
		)
	}
	result := map[string][]string{}
	for _, owner := range ownerNames {
		items, ok := owners[owner].([]any)
		if !ok {
			return nil, errorf(
				"%s: the paths recorded for %s must be a list of strings.",
				source, util.PythonRepr(owner),
			)
		}
		paths := make([]string, 0, len(items))
		for _, item := range items {
			path, ok := item.(string)
			if !ok {
				return nil, errorf(
					"%s: the paths recorded for %s must be a list of strings.",
					source, util.PythonRepr(owner),
				)
			}
			paths = append(paths, path)
		}
		sort.Strings(paths)
		result[owner] = paths
	}
	return result, nil
}

// isRecordVersion reports whether a decoded schema_version equals want. The
// decoder answers a JSON integer as int64, and Python compared the decoded
// value against the int 2.
func isRecordVersion(value any, want int) bool {
	switch typed := value.(type) {
	case int64:
		return typed == int64(want)
	case float64:
		return typed == float64(want)
	default:
		return false
	}
}

// LoadFilesManifest returns owner -> published paths from the record at path.
//
// An absent record is an empty mapping, which is not a fallback but the real
// initial state: nothing has published anything for this project yet.
func LoadFilesManifest(path string) (map[string][]string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return map[string][]string{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFilesManifest(string(content), path)
}

// RenderFilesManifest returns the JSON text of slug's published-file record.
//
// Every path is relative to site/: the project's own pages are under <slug>/
// and its posts are at blog/<post-slug>/, in one namespace so a claim can be
// compared against any other project's.
func RenderFilesManifest(slug string, owners map[string][]string) (string, error) {
	recorded := map[string]any{}
	for _, owner := range PublishOwners {
		paths := owners[owner]
		if len(paths) == 0 {
			continue
		}
		recorded[owner] = sortedStrings(paths)
	}
	encoded, err := util.PythonJSONIndent2(map[string]any{
		"schema_version": FilesRecordVersion,
		"slug":           slug,
		"owners":         recorded,
	})
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// RemoteTextFetcher reads the text of a path on a repository's default branch.
//
// [StagePublishedRecord] takes one rather than calling the GitHub API itself:
// that is the one read in this whole package that would need a remote, and
// keeping it a parameter is what lets the model stay free of the network while
// the publisher that owns the remote supplies its own reader. An absent file
// is the empty string, and every other failure is an error -- a caller that
// read a failure as "nothing published yet" would write a record erasing what
// it could not read.
type RemoteTextFetcher func(repo, path, operation string) (string, error)

// StagePublishedRecord records what owner now publishes for slug and returns
// the repo-relative paths it deletes.
//
// The one path by which a publisher that writes through the Git Data API --
// the documentation publish and the post publish -- keeps the published-file
// record honest. It reads the record on repo through fetch, prunes owner's
// entry against produced (site-relative paths), and stages the rewritten
// record in files alongside whatever else that publisher is pushing, so the
// record and the content it describes reach the same commit.
//
// Without this, a publish leaves its files unclaimed: nothing accounts for
// them, the ownership-prune model cannot protect them from another publisher,
// retirement does not take them along, and the cross-project write refusal
// ([ForeignPostClaims]) cannot see them at all.
func StagePublishedRecord(
	fetch RemoteTextFetcher,
	repo, slug, owner string,
	produced []string,
	files map[string][]byte,
) ([]string, error) {
	recordPath := fmt.Sprintf("manifests/%s-files.json", slug)
	// Absence is the real first-publish state and nothing else is: a failed
	// read here would prune this owner's entry against a record it never saw
	// and drop every other owner's claims from the file it rewrites.
	rawRecord, err := fetch(repo, recordPath, fmt.Sprintf(
		"record what %s publishes for %s on %s",
		util.PythonRepr(owner), util.PythonRepr(slug), repo,
	))
	if err != nil {
		return nil, err
	}
	owners, err := ParseFilesManifest(rawRecord, fmt.Sprintf("%s:%s", repo, recordPath))
	if err != nil {
		return nil, err
	}
	removed, owners, err := PrunePlan(owners, owner, produced)
	if err != nil {
		return nil, err
	}
	rendered, err := RenderFilesManifest(slug, owners)
	if err != nil {
		return nil, err
	}
	files[recordPath] = []byte(rendered)
	deletions := make([]string, 0, len(removed))
	for _, rel := range removed {
		deletions = append(deletions, "site/"+rel)
	}
	return deletions, nil
}

// PrunePlan returns the paths owner must remove, and the updated owners map.
//
// This is the whole of "prune instead of wipe". A publisher removes a path
// only when it published that path before and does not publish it now -- so a
// page a build dropped disappears, while content the build never produced is
// untouched, because nothing entitles this publisher to it. A path another
// publisher currently claims is never removed either: a documentation page or
// a post published between releases outlives a full build that happens not to
// carry it.
func PrunePlan(
	owners map[string][]string,
	owner string,
	produced []string,
) ([]string, map[string][]string, error) {
	if !containsString(PublishOwners, owner) {
		return nil, nil, errorf(
			"unknown publisher %s; expected one of %s",
			util.PythonRepr(owner), strings.Join(PublishOwners, ", "),
		)
	}
	producedSet := make(map[string]bool, len(produced))
	for _, path := range produced {
		producedSet[path] = true
	}
	claimedElsewhere := map[string]bool{}
	for other, paths := range owners {
		if other == owner {
			continue
		}
		for _, path := range paths {
			claimedElsewhere[path] = true
		}
	}
	removed := make([]string, 0)
	for _, path := range owners[owner] {
		if producedSet[path] || claimedElsewhere[path] {
			continue
		}
		removed = append(removed, path)
	}
	removed = sortedUnique(removed)
	updated := make(map[string][]string, len(owners)+1)
	for other, paths := range owners {
		updated[other] = sortedStrings(paths)
	}
	updated[owner] = sortedUnique(produced)
	return removed, updated, nil
}

// GitBlobSHA1 returns git's object id for a blob holding data.
//
// Git hashes "blob <len>\x00<bytes>", so this is computable locally and
// comparable against the sha the Trees API reports for a path -- which is how
// an unchanged file is recognized without uploading it.
func GitBlobSHA1(data []byte) string {
	digest := sha1.New()
	fmt.Fprintf(digest, "blob %d\x00", len(data))
	digest.Write(data)
	return fmt.Sprintf("%x", digest.Sum(nil))
}
