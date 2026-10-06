package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/excludes"
	"github.com/stricttools/selfdoc/internal/layout"
)

// The layout strictcli's files had before they moved under [layout.Root]: one
// .strictcli/ directory beside the module it served, holding the program's
// schema, its committed test-coverage manifest, the per-process coverage
// shards, and in a Go module a stub go.mod.
const (
	previousCLIDir          = ".strictcli"
	previousSchemaName      = "schema.json"
	previousCoverageName    = "test-coverage.json"
	previousShardsDir       = "coverage"
	previousStubModule      = "go.mod"
	previousShardSuffix     = ".jsonl"
	rootGitignore           = ".gitignore"
	previousCoverageIgnore  = "coverage/"
	managedFilesRel         = ".rlsbl/managed-files.json"
	previousStubModuleBases = ".rlsbl/bases/" + previousCLIDir
)

// Where strictcli's files sit now, under [layout.Root] beside the module they
// serve. strictcli owns both directories; their manifests name it.
const (
	cliSchemaDir          = ".cli-schema"
	cliTestCoverageDir    = ".cli-test-coverage"
	cliSchemaName         = "schema.json"
	cliCoverageName       = "manifest.json"
	cliShardsDir          = "shards"
	cliTestCoverageIgnore = cliShardsDir + "/\n"
)

// ShardMove is a set of uncommitted coverage shard files moving from one
// directory to another, as paths relative to the repository root.
type ShardMove struct {
	From  string
	To    string
	Files []string
}

// Delete is one file the move deletes, and why.
type Delete struct {
	Path string
	Why  string
}

// joinRel joins slash-form relative path parts, "." standing for the
// repository root.
func joinRel(parts ...string) string {
	return path.Clean(path.Join(parts...))
}

// UnplaceableError is a file under a .strictcli/ directory the move does not
// know the new place of: it is not the schema, the coverage manifest, the stub
// module, or a coverage shard, or it is one of those that git does not track.
type UnplaceableError struct {
	// Paths are the files, relative to the repository root.
	Paths []string
}

func (e *UnplaceableError) Error() string {
	return fmt.Sprintf(
		"selfdoc layout migrate moves a .strictcli/ directory's committed schema (%s), committed coverage manifest (%s), stub %s, and %s/*%s shards under %s/, and does not know where these belong: %s. A file git does not track is not moved either, since the move would carry it into a directory the repository commits. Move each one where it belongs, or delete it, and run this again.",
		previousSchemaName, previousCoverageName, previousStubModule, previousShardsDir, previousShardSuffix,
		layout.Root, strings.Join(e.Paths, ", "))
}

// PartialCLIMoveError is a .strictcli/ file whose new place is already taken:
// a move that was begun and not finished, which this command will not guess
// the rest of.
type PartialCLIMoveError struct {
	From string
	To   string
}

func (e *PartialCLIMoveError) Error() string {
	return fmt.Sprintf(
		"%s is still in place and %s, where it moves, exists as well. selfdoc layout migrate will not guess which of the two is current. Remove the one that is not, commit, and run it again.",
		e.From, e.To)
}

// cliDirSkipped reports whether the search for .strictcli/ directories passes
// over a directory: hidden ones, vendored and fixture trees, and the
// repository's scratch directories.
func cliDirSkipped(baseDir, full, name string) bool {
	switch name {
	case "node_modules", "vendor", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".") || excludes.IsScratchDir(baseDir, full)
}

// findCLIDirs returns the directories under baseDir holding a .strictcli/
// directory, relative to baseDir in slash form, "." for the root, sorted.
func findCLIDirs(baseDir string) ([]string, error) {
	root, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	var found []string
	walkErr := filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if full != root && cliDirSkipped(root, full, entry.Name()) {
			return fs.SkipDir
		}
		if info, statErr := os.Lstat(filepath.Join(full, previousCLIDir)); statErr == nil && info.IsDir() {
			rel, relErr := filepath.Rel(root, full)
			if relErr != nil {
				return relErr
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(found)
	return found, nil
}

// planCLIFiles moves every .strictcli/ directory's files under the
// .strictmetadata/ beside it: the schema to .cli-schema/schema.json, the
// coverage manifest to .cli-test-coverage/manifest.json, and the shards to
// .cli-test-coverage/shards/. It writes the manifest naming strictcli in each
// of those directories, and the ignore file that keeps the shards out of the
// repository; deletes the stub go.mod, which the root's own go.mod covers;
// and removes each emptied .strictcli/. For the repository root's
// .strictcli/ it also deletes rlsbl's scaffold base of the stub, drops the
// stub from rlsbl's managed-files record, and removes the root .gitignore's
// coverage/ line, which ignored the shard directory.
func (p *Plan) planCLIFiles(baseDir string, h *effects.Handle) error {
	dirs, err := findCLIDirs(baseDir)
	if err != nil {
		return err
	}
	var unplaceable []string
	for _, dir := range dirs {
		problems, err := p.planCLIDir(baseDir, dir, h)
		if err != nil {
			return err
		}
		unplaceable = append(unplaceable, problems...)
	}
	if len(unplaceable) > 0 {
		return &UnplaceableError{Paths: unplaceable}
	}
	for _, dir := range dirs {
		if dir != "." {
			continue
		}
		if err := p.planRootCLIRecords(baseDir, h); err != nil {
			return err
		}
	}
	return nil
}

// planCLIDir plans one .strictcli/ directory's move, returning the files it
// does not know the place of.
func (p *Plan) planCLIDir(baseDir, dir string, h *effects.Handle) ([]string, error) {
	fromDir := joinRel(dir, previousCLIDir)
	newRoot := joinRel(dir, layout.Root)
	schemaDir := joinRel(newRoot, cliSchemaDir)
	coverageDir := joinRel(newRoot, cliTestCoverageDir)
	tracked, err := trackedFiles(baseDir, fromDir, h)
	if err != nil {
		return nil, err
	}
	isTracked := map[string]bool{}
	for _, file := range tracked {
		isTracked[file] = true
	}
	entries, err := os.ReadDir(filepath.Join(baseDir, filepath.FromSlash(fromDir)))
	if err != nil {
		return nil, err
	}
	var unplaceable []string
	schemaMoves, coverageMoves := false, false
	removeShardsDir := false
	for _, entry := range entries {
		rel := joinRel(fromDir, entry.Name())
		switch {
		case !entry.IsDir() && (entry.Name() == previousSchemaName || entry.Name() == previousCoverageName):
			if !isTracked[rel] {
				unplaceable = append(unplaceable, rel)
				continue
			}
			to := joinRel(schemaDir, cliSchemaName)
			if entry.Name() == previousCoverageName {
				to = joinRel(coverageDir, cliCoverageName)
				coverageMoves = true
			} else {
				schemaMoves = true
			}
			if _, err := os.Lstat(filepath.Join(baseDir, filepath.FromSlash(to))); err == nil {
				return nil, &PartialCLIMoveError{From: rel, To: to}
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			p.CLIMoves = append(p.CLIMoves, Move{From: rel, To: to})
			p.Commit = append(p.Commit, rel, to)
		case !entry.IsDir() && entry.Name() == previousStubModule && isTracked[rel]:
			p.Deletes = append(p.Deletes, Delete{Path: rel, Why: "the stub module " + joinRel(newRoot, previousStubModule) + " covers the new place"})
			p.Commit = append(p.Commit, rel)
		case entry.IsDir() && entry.Name() == previousShardsDir:
			shards, others, err := shardFiles(baseDir, rel, isTracked)
			if err != nil {
				return nil, err
			}
			unplaceable = append(unplaceable, others...)
			if len(shards) > 0 {
				to := joinRel(coverageDir, cliShardsDir)
				for _, shard := range shards {
					target := joinRel(to, shard)
					if _, err := os.Lstat(filepath.Join(baseDir, filepath.FromSlash(target))); err == nil {
						return nil, &PartialCLIMoveError{From: joinRel(rel, shard), To: target}
					} else if !errors.Is(err, os.ErrNotExist) {
						return nil, err
					}
				}
				p.ShardMoves = append(p.ShardMoves, ShardMove{From: rel, To: to, Files: shards})
				coverageMoves = true
			}
			removeShardsDir = len(others) == 0
		default:
			unplaceable = append(unplaceable, rel)
		}
	}
	if len(unplaceable) > 0 {
		return unplaceable, nil
	}
	if err := p.planCLIGrant(baseDir, schemaDir, schemaMoves, ""); err != nil {
		return nil, err
	}
	if err := p.planCLIGrant(baseDir, coverageDir, coverageMoves, cliTestCoverageIgnore); err != nil {
		return nil, err
	}
	if removeShardsDir {
		p.RemoveDirs = append(p.RemoveDirs, joinRel(fromDir, previousShardsDir))
	}
	p.RemoveDirs = append(p.RemoveDirs, fromDir)
	p.CLIDirs = append(p.CLIDirs, fromDir)
	return nil, nil
}

// shardFiles lists the shard files in a .strictcli/coverage/ directory by
// name, and every other entry, or a tracked shard, as a file the move does not
// place.
func shardFiles(baseDir, rel string, isTracked map[string]bool) ([]string, []string, error) {
	entries, err := os.ReadDir(filepath.Join(baseDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, err
	}
	var shards, others []string
	for _, entry := range entries {
		file := joinRel(rel, entry.Name())
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), previousShardSuffix) || isTracked[file] {
			others = append(others, file)
			continue
		}
		shards = append(shards, entry.Name())
	}
	return shards, others, nil
}

// planCLIGrant writes the manifest naming strictcli in one of its directories
// when the directory receives a file in this move or already exists without
// one, and the directory's ignore file when it needs one and lacks it.
func (p *Plan) planCLIGrant(baseDir, dir string, receives bool, ignore string) error {
	full := filepath.Join(baseDir, filepath.FromSlash(dir))
	info, err := os.Stat(full)
	exists := err == nil && info.IsDir()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !receives && !exists {
		return nil
	}
	manifestRel := joinRel(dir, layout.ManifestFileName)
	if _, err := os.Lstat(filepath.Join(baseDir, filepath.FromSlash(manifestRel))); errors.Is(err, os.ErrNotExist) {
		p.Writes = append(p.Writes, Write{
			Path: manifestRel, Why: "the grant naming strictcli as the owner",
			Content: []byte(layout.DirectoryManifestContent(layout.CLIOwner)),
		})
		p.Commit = append(p.Commit, manifestRel)
	} else if err != nil {
		return err
	}
	if ignore == "" {
		return nil
	}
	ignoreRel := joinRel(dir, layout.IgnoreFileName)
	existing, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(ignoreRel)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if string(existing) != ignore {
		p.Writes = append(p.Writes, Write{
			Path: ignoreRel, Why: "keeps the shards out of the repository",
			Content: []byte(ignore),
		})
		p.Commit = append(p.Commit, ignoreRel)
	}
	return nil
}

// planRootCLIRecords plans what a repository root's .strictcli/ leaves behind
// outside itself: rlsbl's scaffold base of the stub module, the stub's entry
// in rlsbl's managed-files record, and the root .gitignore's coverage/ line.
func (p *Plan) planRootCLIRecords(baseDir string, h *effects.Handle) error {
	baseStub := previousStubModuleBases + "/" + previousStubModule
	if _, err := os.Lstat(filepath.Join(baseDir, filepath.FromSlash(baseStub))); err == nil {
		p.Deletes = append(p.Deletes, Delete{Path: baseStub, Why: "rlsbl's scaffold base of the stub module, which no longer exists"})
		p.Commit = append(p.Commit, baseStub)
		p.RemoveDirs = append(p.RemoveDirs, previousStubModuleBases)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := p.planManagedFiles(baseDir); err != nil {
		return err
	}
	return p.planRootGitignore(baseDir)
}

// planManagedFiles drops the stub module's entry from rlsbl's managed-files
// record, line-scoped so every other byte of the file stays.
func (p *Plan) planManagedFiles(baseDir string) error {
	full := filepath.Join(baseDir, filepath.FromSlash(managedFilesRel))
	raw, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	key := jsonString(previousCLIDir + "/" + previousStubModule)
	lines := strings.Split(string(raw), "\n")
	index := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+":") {
			if index >= 0 {
				return fmt.Errorf("%s names %s on more than one line; remove the entry by hand and run this again", managedFilesRel, key)
			}
			index = i
		}
	}
	if index < 0 {
		return nil
	}
	removedHadComma := strings.HasSuffix(strings.TrimRight(lines[index], " \t\r"), ",")
	lines = append(lines[:index], lines[index+1:]...)
	if !removedHadComma && index > 0 {
		previous := strings.TrimRight(lines[index-1], " \t\r")
		lines[index-1] = strings.TrimSuffix(previous, ",")
	}
	content := strings.Join(lines, "\n")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return fmt.Errorf("%s is not valid JSON once the %s entry is removed line by line (%v); remove the entry by hand and run this again", managedFilesRel, key, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	p.Rewrites = append(p.Rewrites, Rewrite{
		Path: managedFilesRel, Changes: []string{"the " + key + " entry removed"},
		Content: []byte(content), Mode: info.Mode().Perm(),
	})
	p.Commit = append(p.Commit, managedFilesRel)
	return nil
}

// planRootGitignore removes the root .gitignore's coverage/ line, which kept
// the .strictcli/coverage/ shards out of the repository; the shards' new
// directory carries its own ignore file.
func (p *Plan) planRootGitignore(baseDir string) error {
	full := filepath.Join(baseDir, rootGitignore)
	raw, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := lines[:0:0]
	removed := false
	for _, line := range lines {
		if strings.TrimRight(line, " \t\r") == previousCoverageIgnore {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	p.Rewrites = append(p.Rewrites, Rewrite{
		Path: rootGitignore, Changes: []string{"the " + previousCoverageIgnore + " line removed"},
		Content: []byte(strings.Join(kept, "\n")), Mode: info.Mode().Perm(),
	})
	p.Commit = append(p.Commit, rootGitignore)
	return nil
}

// RootIgnoreError is an ignore file at the top of .strictmetadata/ holding
// lines that are not selfdoc's: another tool's rules, which the move does not
// know the directory of.
type RootIgnoreError struct {
	// Lines are what the file holds once selfdoc's block is removed.
	Lines string
}

func (e *RootIgnoreError) Error() string {
	return fmt.Sprintf(
		"%s holds lines that are not selfdoc's, and %s/ holds directories and %s only, each directory carrying its own %s. Move these lines into the %s of the directory each is for, delete them from %s, and run this again:\n%s",
		layout.RootIgnoreRel, layout.Root, strings.Join(layout.RootFiles, ", "), layout.IgnoreFileName,
		layout.IgnoreFileName, layout.RootIgnoreRel, strings.TrimRight(e.Lines, "\n"))
}

// planIgnores converts the ignore file an earlier selfdoc derived at the top
// of .strictmetadata/ into one per directory: it deletes that file when
// selfdoc's block was all it held, and writes the ignore file of every
// uncommitted directory selfdoc owns that exists, or arrives in this move,
// and lacks it.
func (p *Plan) planIgnores(baseDir string, arriving map[string]bool) error {
	raw, err := os.ReadFile(layout.Path(baseDir, layout.RootIgnoreRel))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if remaining := layout.WithoutIgnoreBlock(string(raw)); strings.TrimSpace(remaining) != "" {
			return &RootIgnoreError{Lines: remaining}
		}
		p.Deletes = append(p.Deletes, Delete{Path: layout.RootIgnoreRel, Why: "each directory carries its own ignore file now"})
		p.Commit = append(p.Commit, layout.RootIgnoreRel)
	}
	for _, dir := range layout.IgnoredDirectories() {
		if !arriving[dir.Name] {
			info, err := os.Stat(layout.Path(baseDir, dir.Rel()))
			if err != nil || !info.IsDir() {
				continue
			}
			manifest, err := layout.ReadDirectoryManifest(baseDir, dir.Name)
			if err != nil || manifest.Owner != layout.Owner {
				continue
			}
		}
		rel := layout.DirectoryIgnoreRel(dir.Name)
		existing, err := os.ReadFile(layout.Path(baseDir, rel))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if string(existing) == layout.DirectoryIgnoreContent() {
			continue
		}
		p.Writes = append(p.Writes, Write{
			Path: rel, Why: "the directory's own ignore file",
			Content: []byte(layout.DirectoryIgnoreContent()),
		})
		p.Commit = append(p.Commit, rel)
	}
	return nil
}
