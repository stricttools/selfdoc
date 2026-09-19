// Package staleness detects descriptions that no longer describe what they
// sit on, by hashing what a description is about and comparing that hash
// against the one recorded the last time the description was written.
//
// Three inputs are tracked per page: the page's own raw body, the module
// docstrings of the sources its directives reference, and -- for a CLI page --
// the schema slice of the command it documents. When one of those changes and
// the page's frontmatter description does not, the description is reported
// stale (the body) or drifted (the docstrings, the schema).
//
// # The baseline hold
//
// A page reported stale or drifted keeps its ENTIRE previous entry, so the
// error persists until the description is actually rewritten. Advancing the
// baseline on the first report would make the second run pass with nothing
// fixed, which is how a stale description used to become permanent.
//
// # Two writers, one store
//
// The store is written by two independent passes: gen owns each page's
// seed_hash, while build and check own content, description, source_docstring
// and schema_hash. UpdateHashes therefore MERGES into an existing entry
// instead of replacing it, so neither writer erases the other's fields.
//
// # The empty string stands for an absent hash
//
// Every hash this package stores is a 64-character hex digest, so no real
// value is ever the empty string and the Python surface's `None` is carried
// as "" throughout: an absent source-docstring hash, an absent schema hash,
// and an absent stored field are all the empty string. The distinctions the
// Python drew with `is None` and `not in` are the same distinctions here.
package staleness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// HashVersion is the version of the on-disk hash store's schema. It is bumped
// when the meaning of a stored hash changes, so an older store is discarded
// and re-baselined on load rather than compared against.
//
//	v1 -> v2: the content hash switched from the resolved output to the raw
//	          template body.
//	v2 -> v3: added the per-page seed_hash (written by gen) and canonicalized
//	          directive-marker attribute values before content hashing.
const HashVersion = 3

// BaselineAcceptHintTemplate is the remediation hint appended to every
// DRIFT001 message, with "{source}" and "{page}" standing for what changed
// and which page reports it. BaselineAcceptHint renders it.
//
// A drift error means the source moved while the description did not -- which
// is sometimes correct: the description can still be accurate. Without this
// hint the operator is told a problem exists but not how to close it
// honestly, and the only discoverable way out is to invent a description
// edit.
//
// The hint names BOTH ways out and says that they are alternatives. Naming
// only the accept made it read as a step to take after any review, including a
// review that ended in a description edit -- and an edited description clears
// the finding by itself, so the accept that followed refused with nothing to
// accept.
const BaselineAcceptHintTemplate = "review the page against the changed {source}, then take " +
	"ONE of these: edit the page's frontmatter description, which clears " +
	"this on its own and needs no further command; or, when the description " +
	"is still accurate, run `selfdoc baseline accept {page}` to accept the " +
	"current content and description as the new baseline"

// hashVersionKey is the top-level key the store's version is recorded under.
// It sorts before every page key, because "_" precedes every letter and
// digit a docs path starts with.
const hashVersionKey = "_hash_version"

// directiveMarkerPrefixes identify the directive marker lines whose attribute
// VALUES are canonicalized before content hashing: one-liner (:-:), block
// open (:<:) and attribute (:@:) markers. The body-separator, content and
// close markers carry no attributes and are left untouched.
var directiveMarkerPrefixes = []string{":-: ", ":<: ", ":@: "}

// pySpaceCutset is the character set Python's str.lstrip() removes -- every
// code point str.isspace() reports true for. Go's unicode.IsSpace omits the
// four ASCII separator controls, which a directive marker line could in
// principle be indented with.
const pySpaceCutset = "\t\n\v\f\r \x1c\x1d\x1e\x1f" +
	"\u0085\u00a0\u1680" +
	"\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a" +
	"\u2028\u2029\u202f\u205f\u3000"

// attrKVRe matches one key="value" attribute pair. It is the directives
// package's own pattern, restated because that package keeps it unexported;
// the two must stay identical, since a pair this pattern does not see keeps
// its value in the content hash and a mechanical rename then reads as an
// edit. The key class is Python's [\w-] spelled for Go.
var attrKVRe = regexp.MustCompile(`([\p{L}\p{N}_-]+)="([^"]*)"`)

// Entry is one page's recorded hashes. Every field is a hex digest, and the
// empty string means the field was never recorded -- which the drift checks
// read as "nothing to compare against yet" rather than as a mismatch.
type Entry struct {
	// Content is the hash of the page's raw body.
	Content string
	// Description is the hash of the page's frontmatter description.
	Description string
	// SourceDocstring is the hash of the module docstrings of the sources
	// the page's directives reference.
	SourceDocstring string
	// SchemaHash is the hash of the CLI schema slice a CLI page documents.
	SchemaHash string
	// SeedHash is the hash of the machine-emitted description text gen last
	// wrote onto the page. gen owns this field; build and check own the
	// other four.
	SeedHash string
}

// asDocument renders the entry as the JSON object the store file carries,
// omitting every field that was never recorded -- which is what keeps the
// written bytes identical to the Python's, where an unrecorded field is an
// absent key rather than an empty string.
func (e Entry) asDocument() map[string]any {
	document := map[string]any{}
	for key, value := range map[string]string{
		"content":          e.Content,
		"description":      e.Description,
		"source_docstring": e.SourceDocstring,
		"schema_hash":      e.SchemaHash,
		"seed_hash":        e.SeedHash,
	} {
		if value != "" {
			document[key] = value
		}
	}
	return document
}

// Store is the hash store: one Entry per page, keyed by the page's path
// relative to the docs directory and prefixed with its locale when the
// project declares more than one.
type Store map[string]Entry

// Doc is one resolved docs page in the shape this package reads it: the
// parsed frontmatter and the RAW template body.
//
// The raw body is what the content hash covers, so a directive whose output
// changed -- a version bump, a regenerated table -- does not read as a page
// edit. It is the same tuple the docs resolution produces, narrowed to the
// two members the hash computation uses.
type Doc struct {
	// Frontmatter is the page's parsed metadata block.
	Frontmatter util.Frontmatter
	// Raw is the page's pre-resolution template content, frontmatter
	// included.
	Raw string
}

// PageDirective is one resolved directive as the drift measurement reads it:
// the path its "path" attribute named, and the source entry that answered it.
//
// It is the narrowed form of the check pass's resolved-directive record --
// the two members the source-docstring hash needs -- so this package does
// not depend on the command layer that produces them.
type PageDirective struct {
	// PathArg is the directive's "path" attribute, "" when it declared none.
	PathArg string
	// SourceEntry is the declared source path that answered the directive,
	// nil when none did.
	SourceEntry *extractors.SourceEntry
}

// SourceFile is one source path paired with the extractor that reads it.
type SourceFile struct {
	// Path is the resolved filesystem path.
	Path string
	// Extractor reads this path's module documentation.
	Extractor extractors.Extractor
}

// Warning is one staleness or drift report: the page it is about and the
// message the lint carries.
type Warning struct {
	// Page is the page's store key.
	Page string
	// Message is the full diagnostic, hint included.
	Message string
}

// BaselineAcceptHint renders the DRIFT001 remediation hint for pagePath.
//
// source names what changed ("docstrings", "CLI schema") so the operator
// knows what to review the description against.
func BaselineAcceptHint(pagePath, source string) string {
	hint := strings.ReplaceAll(BaselineAcceptHintTemplate, "{source}", source)
	return strings.ReplaceAll(hint, "{page}", pagePath)
}

// canonicalizeDirectiveMarkers blanks the attribute VALUES on directive
// marker lines.
//
// A marker line such as `:-: ref path="mylib.old"` is rewritten to
// `:-: ref path=""`, so a pure path="x" -> path="y" rename does not change
// the content hash -- the resolved output the reader sees is unchanged --
// while any prose change or directive NAME change still does. Only
// :-:/:<:/:@: marker lines are affected; a key="value" pair in ordinary
// prose is left intact.
func canonicalizeDirectiveMarkers(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if hasDirectiveMarkerPrefix(strings.TrimLeft(line, pySpaceCutset)) {
			out = append(out, attrKVRe.ReplaceAllString(line, `${1}=""`))
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// hasDirectiveMarkerPrefix reports whether line opens with one of the marker
// prefixes whose attributes are canonicalized.
func hasDirectiveMarkerPrefix(line string) bool {
	for _, prefix := range directiveMarkerPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// ComputeContentHash computes the SHA-256 hash of a page's raw body, with the
// directive marker lines canonicalized.
//
// It receives the page's BODY -- what the frontmatter reader returned behind
// the block, pre-resolution -- so a change in what a directive emits (a
// version bump, a regenerated table) is not a content change. This pass used
// to strip a frontmatter block of its own before hashing; the reader is the
// one place that knows what a block looks like, and a body is what it hands
// out. See canonicalizeDirectiveMarkers for what a marker line contributes.
func ComputeContentHash(body string) string {
	return sha256Hex(canonicalizeDirectiveMarkers(body))
}

// ComputeDescriptionHash computes the SHA-256 hash of a frontmatter
// description string.
func ComputeDescriptionHash(description string) string {
	return sha256Hex(description)
}

// ExtractModuleDocstring extracts the module-level documentation at path
// through its extractor.
func ExtractModuleDocstring(path string, extractor extractors.Extractor) (string, error) {
	return extractor.ModuleDocstring(path)
}

// ComputeSourceDocstringHash computes the SHA-256 hash of the module
// docstrings of sourceFiles, joined with newlines in path order.
//
// It returns "" when not one of the files carries any documentation: there is
// nothing to compare a description against, so drift is not measurable for
// that page.
func ComputeSourceDocstringHash(sourceFiles []SourceFile) (string, error) {
	sorted := make([]SourceFile, len(sourceFiles))
	copy(sorted, sourceFiles)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})
	docstrings := make([]string, 0, len(sorted))
	any := false
	for _, file := range sorted {
		docstring, err := ExtractModuleDocstring(file.Path, file.Extractor)
		if err != nil {
			return "", err
		}
		if docstring != "" {
			any = true
		}
		docstrings = append(docstrings, docstring)
	}
	if !any {
		return "", nil
	}
	return sha256Hex(strings.Join(docstrings, "\n")), nil
}

// ComputeSchemaHash computes the SHA-256 hash of a CLI command's schema
// slice.
//
// The slice is serialized as canonical JSON with sorted keys, so the hash
// covers every field the schema carries for that one command or group --
// its help text, its flags, its arguments and anything a later strictcli
// adds -- and is stable across runs.
func ComputeSchemaHash(schemaSlice any) (string, error) {
	serialized, err := util.PythonJSON(schemaSlice)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(serialized)), nil
}

// CheckStaleness reports whether a page's description is stale: its content
// changed while its description stayed the same.
//
// It returns "" for a new page, for unchanged content, and for a description
// that was rewritten alongside the content.
func CheckStaleness(pagePath, contentHash, descriptionHash string, stored Store) string {
	entry, ok := stored[pagePath]
	if !ok {
		// New page -- no staleness.
		return ""
	}
	if contentHash == entry.Content {
		// Content unchanged -- no staleness.
		return ""
	}
	if descriptionHash != entry.Description {
		// The description was updated too -- no staleness.
		return ""
	}
	return fmt.Sprintf(
		"%s: content changed but frontmatter description "+
			"was not updated (possible stale description)", pagePath)
}

// CheckDrift reports whether a page's description drifted from the module
// docstrings of the sources it documents.
//
// It returns "" when the page has no source docstrings, is new, has no
// recorded docstring hash yet, has unchanged sources, or had its description
// rewritten alongside them.
func CheckDrift(pagePath, sourceDocstringHash, descriptionHash string, stored Store) string {
	if sourceDocstringHash == "" {
		return ""
	}
	entry, ok := stored[pagePath]
	if !ok {
		return ""
	}
	if entry.SourceDocstring == "" {
		return ""
	}
	if sourceDocstringHash == entry.SourceDocstring {
		return ""
	}
	if descriptionHash != entry.Description {
		return ""
	}
	return fmt.Sprintf(
		"%s: source docstrings changed but page description "+
			"was not updated (possible documentation drift) -- ", pagePath) +
		BaselineAcceptHint(pagePath, "docstrings")
}

// CheckSchemaDrift reports whether a CLI page's description drifted from the
// schema slice of the command it documents.
//
// It returns "" when the page has no schema hash, is new, has no recorded
// schema hash yet, has an unchanged schema, or had its description rewritten
// alongside the schema.
func CheckSchemaDrift(pagePath, schemaHash, descriptionHash string, stored Store) string {
	if schemaHash == "" {
		return ""
	}
	entry, ok := stored[pagePath]
	if !ok {
		return ""
	}
	if entry.SchemaHash == "" {
		return ""
	}
	if schemaHash == entry.SchemaHash {
		return ""
	}
	if descriptionHash != entry.Description {
		return ""
	}
	return fmt.Sprintf(
		"%s: CLI schema changed but page description "+
			"was not updated (possible stale CLI description) -- ", pagePath) +
		BaselineAcceptHint(pagePath, "CLI schema")
}

// HashesPath is where the store file sits under a project root.
func HashesPath(baseDir string) string {
	return layout.Path(baseDir, layout.HashesRel)
}

// LoadHashes loads the hash store from selfdoc's hash directory.
//
// It returns an empty store when the file does not exist, and discards an
// older store wholesale -- nothing a v1 or v2 file holds is reusable under
// v3's rules, so the project re-baselines rather than comparing against
// hashes that meant something else.
func LoadHashes(baseDir string) (Store, error) {
	path := HashesPath(baseDir)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return Store{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	version, ok := document[hashVersionKey]
	if !ok {
		return Store{}, nil
	}
	var recorded int
	if err := json.Unmarshal(version, &recorded); err != nil || recorded != HashVersion {
		return Store{}, nil
	}
	store := Store{}
	for key, raw := range document {
		if key == hashVersionKey {
			continue
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("%s: entry %q: %w", path, key, err)
		}
		store[key] = Entry{
			Content:         fields["content"],
			Description:     fields["description"],
			SourceDocstring: fields["source_docstring"],
			SchemaHash:      fields["schema_hash"],
			SeedHash:        fields["seed_hash"],
		}
	}
	return store, nil
}

// SaveHashes writes the hash store to selfdoc's hash directory, creating
// the directory when it is missing and stamping the current HashVersion.
//
// The write is atomic, so an interrupted run can never leave a truncated
// store -- which would re-baseline every page on the next run.
func SaveHashes(hashes Store, baseDir string, handle *effects.Handle) error {
	hashesDir := layout.Path(baseDir, layout.HashesDirRel)
	if err := layout.EnsureDir(handle, baseDir, layout.HashesDirRel); err != nil {
		return err
	}
	document := map[string]any{hashVersionKey: HashVersion}
	for key, entry := range hashes {
		document[key] = entry.asDocument()
	}
	serialized, err := util.PythonJSONIndent2(document)
	if err != nil {
		return err
	}
	target := filepath.Join(hashesDir, "hashes.json")
	return handle.AtomicWrite(target, append(serialized, '\n'), effects.ModeDefault)
}

// ComputeCurrentHashes computes the full current hash entry for every
// documentation page.
//
// Every page whose frontmatter declares a description gets a content and a
// description hash; a page that declares none is skipped entirely, because
// there is no description to report stale. A page whose directives resolve to
// readable sources also gets a source-docstring hash, and a page named in
// schemaHashes gets its schema hash.
//
// This is the baseline a page WOULD receive if it had no outstanding errors:
// nothing is read from or written to the store, and no comparison is made.
// Pass nil for pageDirectives or schemaHashes to leave that hash out.
func ComputeCurrentHashes(
	allDocs map[string]Doc,
	baseDir string,
	pageDirectives map[string][]PageDirective,
	schemaHashes map[string]string,
) (Store, error) {
	current := Store{}

	for _, relPath := range sortedKeys(allDocs) {
		doc := allDocs[relPath]
		description, ok := doc.Frontmatter["description"]
		if !ok {
			continue
		}
		current[relPath] = Entry{
			Content:     ComputeContentHash(doc.Raw),
			Description: ComputeDescriptionHash(util.PythonStr(description)),
		}
	}

	for _, relPath := range sortedKeys(allDocs) {
		directives, ok := pageDirectives[relPath]
		if !ok {
			continue
		}
		var sourceFiles []SourceFile
		for _, directive := range directives {
			if directive.SourceEntry == nil {
				continue
			}
			if directive.PathArg == "" {
				continue
			}
			resolved := directive.SourceEntry.Extractor.ResolvePath(
				directive.PathArg,
				[]string{directive.SourceEntry.Path},
				baseDir,
			)
			if resolved == "" {
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				continue
			}
			sourceFiles = append(sourceFiles, SourceFile{
				Path:      resolved,
				Extractor: directive.SourceEntry.Extractor,
			})
		}
		if len(sourceFiles) == 0 {
			continue
		}
		docstringHash, err := ComputeSourceDocstringHash(sourceFiles)
		if err != nil {
			return nil, err
		}
		entry, tracked := current[relPath]
		if docstringHash != "" && tracked {
			entry.SourceDocstring = docstringHash
			current[relPath] = entry
		}
	}

	for relPath, schemaHash := range schemaHashes {
		entry, tracked := current[relPath]
		if !tracked {
			continue
		}
		entry.SchemaHash = schemaHash
		current[relPath] = entry
	}

	return current, nil
}

// UpdateHashes computes the current hashes for every page, reports the pages
// whose descriptions are stale or drifted, and advances the baseline of every
// page that is not.
//
// dryRun computes everything and writes nothing. Pass nil for pageDirectives
// to leave source-docstring drift unmeasured, and nil for schemaHashes to
// leave CLI schema drift unmeasured.
//
// skeletonPages names the pages that are both generated and machine-seeded,
// keyed as allDocs is. Those pages cannot be hand-fixed, so a persistent
// staleness or drift report would hold their baseline in error forever and
// never advance: they are exempt, their baselines always advance, and neither
// warning list ever mentions them. Pass an empty set where no exemption
// applies -- the build and gen paths, which regenerate content and
// description together. There is deliberately no default, because a caller
// that has not decided has not thought about the deadlock.
//
// A page reported stale or drifted keeps its ENTIRE previous entry, so the
// error persists until the description is rewritten. Every other page's
// freshly computed fields are MERGED into its existing entry, so the
// gen-owned seed_hash survives a check pass and vice versa.
func UpdateHashes(
	allDocs map[string]Doc,
	baseDir string,
	dryRun bool,
	pageDirectives map[string][]PageDirective,
	schemaHashes map[string]string,
	skeletonPages map[string]bool,
	handle *effects.Handle,
) (staleWarnings, driftWarnings []Warning, err error) {
	stored, err := LoadHashes(baseDir)
	if err != nil {
		return nil, nil, err
	}
	current, err := ComputeCurrentHashes(allDocs, baseDir, pageDirectives, schemaHashes)
	if err != nil {
		return nil, nil, err
	}

	for _, relPath := range sortedKeys(current) {
		entry := current[relPath]
		if message := CheckStaleness(relPath, entry.Content, entry.Description, stored); message != "" {
			staleWarnings = append(staleWarnings, Warning{Page: relPath, Message: message})
		}
	}

	if pageDirectives != nil {
		for _, relPath := range sortedKeys(current) {
			entry := current[relPath]
			if entry.SourceDocstring == "" {
				continue
			}
			message := CheckDrift(relPath, entry.SourceDocstring, entry.Description, stored)
			if message != "" {
				driftWarnings = append(driftWarnings, Warning{Page: relPath, Message: message})
			}
		}
	}

	// The CLI schema check: for a CLI page, whether the command's schema
	// slice changed with no corresponding description update.
	for _, relPath := range sortedKeys(schemaHashes) {
		entry, tracked := current[relPath]
		if !tracked || entry.Description == "" {
			continue
		}
		message := CheckSchemaDrift(relPath, schemaHashes[relPath], entry.Description, stored)
		if message != "" {
			driftWarnings = append(driftWarnings, Warning{Page: relPath, Message: message})
		}
	}

	// Skeleton pages are exempt from the hold: they cannot be hand-edited to
	// clear the error, so holding their baseline would deadlock them in a
	// perpetual re-error. Their warnings are dropped, so no lint is emitted
	// and their baselines advance with everyone else's below.
	if len(skeletonPages) > 0 {
		staleWarnings = withoutPages(staleWarnings, skeletonPages)
		driftWarnings = withoutPages(driftWarnings, skeletonPages)
	}

	errorPages := map[string]bool{}
	for _, warning := range staleWarnings {
		errorPages[warning.Page] = true
	}
	for _, warning := range driftWarnings {
		errorPages[warning.Page] = true
	}
	for relPath, entry := range current {
		if errorPages[relPath] {
			// Do not advance the baseline of a page with errors.
			continue
		}
		stored[relPath] = merge(stored[relPath], entry)
	}
	if !dryRun {
		if err := SaveHashes(stored, baseDir, handle); err != nil {
			return nil, nil, err
		}
	}
	return staleWarnings, driftWarnings, nil
}

// merge writes the freshly computed build/check-owned fields over an existing
// entry, keeping the gen-owned seed_hash and keeping any field the fresh
// computation did not produce.
func merge(existing, fresh Entry) Entry {
	if fresh.Content != "" {
		existing.Content = fresh.Content
	}
	if fresh.Description != "" {
		existing.Description = fresh.Description
	}
	if fresh.SourceDocstring != "" {
		existing.SourceDocstring = fresh.SourceDocstring
	}
	if fresh.SchemaHash != "" {
		existing.SchemaHash = fresh.SchemaHash
	}
	if fresh.SeedHash != "" {
		existing.SeedHash = fresh.SeedHash
	}
	return existing
}

// withoutPages drops every warning whose page is in exempt.
func withoutPages(warnings []Warning, exempt map[string]bool) []Warning {
	kept := make([]Warning, 0, len(warnings))
	for _, warning := range warnings {
		if exempt[warning.Page] {
			continue
		}
		kept = append(kept, warning)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// sha256Hex is the hex-encoded SHA-256 digest of text's UTF-8 bytes.
func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// sortedKeys returns the keys of m in ascending order, which is the order
// every pass over the pages walks them in so a report reads the same twice.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
