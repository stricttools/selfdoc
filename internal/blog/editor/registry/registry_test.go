package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// isolate binds the environment-isolation floor. The registry expands "~" out
// of the environment and every path it reports is absolute, so a test that
// read the developer's real home would answer differently on every machine.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// write puts a registry document in dir and returns its path.
func write(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, "selfdoc-registry.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	return path
}

// tree makes a working tree inside dir and returns its path.
func tree(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("writing the tree: %v", err)
	}
	return path
}

func TestTheDefaultIsTheMachineLocalFile(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, "Projects", "ark", "selfdoc-registry.toml")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestReadsAHandWrittenFile(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	first := tree(t, dir, "first")
	second := tree(t, dir, "second")
	path := write(t, dir, `
[[repo]]
name = "first"
kind = "local"
path = "`+first+`"

[[repo]]
name = "second"
kind = "local"
path = "`+second+`"
`)

	parsed, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(parsed.Names(), ","); got != "first,second" {
		t.Errorf("Names() = %q, want \"first,second\"", got)
	}
	if parsed.Len() != 2 {
		t.Errorf("Len() = %d, want 2", parsed.Len())
	}
	for name, want := range map[string]string{"first": first, "second": second} {
		entry, err := parsed.Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		local, ok := entry.(*LocalRepo)
		if !ok {
			t.Fatalf("Get(%q) = %T, want *LocalRepo", name, entry)
		}
		if local.Path() != want {
			t.Errorf("Get(%q).Path() = %q, want %q", name, local.Path(), want)
		}
		if local.Kind() != "local" {
			t.Errorf("Get(%q).Kind() = %q, want \"local\"", name, local.Kind())
		}
	}
}

func TestAnEmptyRegistryIsLegal(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	parsed, err := Load(write(t, dir, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if parsed.Len() != 0 {
		t.Errorf("Len() = %d, want 0", parsed.Len())
	}
}

func TestATildePathIsExpanded(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	home := tree(t, dir, "home")
	tree(t, home, "proj")
	t.Setenv("HOME", home)

	parsed, err := Load(write(t, dir, "[[repo]]\nname = \"proj\"\nkind = \"local\"\npath = \"~/proj\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, err := parsed.Get("proj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := filepath.Join(home, "proj"); entry.(*LocalRepo).Path() != want {
		t.Errorf("Path() = %q, want %q", entry.(*LocalRepo).Path(), want)
	}
}

func TestGetNamesTheOfferForAnUnknownEntry(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := write(t, dir, "")
	parsed, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err = parsed.Get("nosuch")
	want := "No repository named 'nosuch' in " + path + ". Known repositories: (none)."
	if err == nil || err.Error() != want {
		t.Fatalf("Get error = %v, want %q", err, want)
	}

	known := tree(t, dir, "known")
	parsed, err = Load(write(t, dir, "[[repo]]\nname = \"known\"\nkind = \"local\"\npath = \""+known+"\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = parsed.Get("nosuch")
	if err == nil || !strings.HasSuffix(err.Error(), "Known repositories: known.") {
		t.Fatalf("Get error = %v, want it to name the one entry on offer", err)
	}
}

func TestARemoteEntryValidates(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	parsed, err := Load(write(t, dir, `
[[repo]]
name = "afar"
kind = "remote"
repo = "smm-h/afar"
ref = "v1.2.3"
cache = "/var/cache/afar"
render = true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, err := parsed.Get("afar")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	remote, ok := entry.(*RemoteRepo)
	if !ok {
		t.Fatalf("Get = %T, want *RemoteRepo", entry)
	}
	if remote.Repo() != "smm-h/afar" || remote.Ref() != "v1.2.3" {
		t.Errorf("repo/ref = %q/%q, want smm-h/afar and v1.2.3", remote.Repo(), remote.Ref())
	}
	if remote.Cache() != "/var/cache/afar" {
		t.Errorf("Cache() = %q, want /var/cache/afar", remote.Cache())
	}
	if !remote.Render() {
		t.Error("Render() = false, want true")
	}
	if remote.Kind() != "remote" {
		t.Errorf("Kind() = %q, want \"remote\"", remote.Kind())
	}
}

func TestARemoteCacheIsExpandedAndMadeAbsolute(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	home := tree(t, dir, "home")
	t.Setenv("HOME", home)
	parsed, err := Load(write(t, dir, `
[[repo]]
name = "afar"
kind = "remote"
repo = "smm-h/afar"
ref = "main"
cache = "~/.cache/selfdoc/afar"
render = false
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, err := parsed.Get("afar")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := filepath.Join(home, ".cache", "selfdoc", "afar")
	if got := entry.(*RemoteRepo).Cache(); got != want {
		t.Errorf("Cache() = %q, want %q", got, want)
	}
}

// refusals pairs a malformed document with the whole message the Python reader
// this package replaces produced for it, captured verbatim. The registry path
// is spelled <PATH> and substituted per run.
func TestMalformedShapesRefuseLoudly(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   string
		prefix bool
	}{
		{
			name: "an unknown top-level key",
			text: "port = 8080\n\n[[repo]]\nname = \"a\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: unknown top-level key(s) port. The registry holds [[repo]] entries and nothing else.",
		},
		{
			name: "repo declared as something other than an array",
			text: "repo = \"selfdoc\"\n",
			want: "<PATH>: 'repo' must be an array of tables ([[repo]]), got str.",
		},
		{
			name: "an entry that is not a table",
			text: "repo = [\"selfdoc\"]\n",
			want: "<PATH>: entry #1: each 'repo' element must be a table ([[repo]]), got str.",
		},
		{
			name: "a missing name",
			text: "[[repo]]\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' is required.",
		},
		{
			name: "a name with a slash",
			text: "[[repo]]\nname = \"with/slash\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' must be a URL-addressable identifier (letters, digits, dot, dash, underscore; no slashes, no spaces), got 'with/slash'.",
		},
		{
			name: "a name that climbs out of the tree",
			text: "[[repo]]\nname = \"../escape\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' must be a URL-addressable identifier (letters, digits, dot, dash, underscore; no slashes, no spaces), got '../escape'.",
		},
		{
			name: "a name with a space",
			text: "[[repo]]\nname = \"sp ace\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' must be a URL-addressable identifier (letters, digits, dot, dash, underscore; no slashes, no spaces), got 'sp ace'.",
		},
		{
			name: "an empty name",
			text: "[[repo]]\nname = \"\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' must be a URL-addressable identifier (letters, digits, dot, dash, underscore; no slashes, no spaces), got ''.",
		},
		{
			name: "a name that is not a string",
			text: "[[repo]]\nname = 7\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: entry #1: 'name' must be a URL-addressable identifier (letters, digits, dot, dash, underscore; no slashes, no spaces), got 7.",
		},
		{
			name: "a missing kind",
			text: "[[repo]]\nname = \"kindless\"\npath = \"<TREE>\"\n",
			want: "<PATH>: repository 'kindless': 'kind' is required and has no default. Declare kind = \"local\" for a working tree on this machine, or kind = \"remote\" for a repository elsewhere.",
		},
		{
			name: "an unknown kind",
			text: "[[repo]]\nname = \"weird\"\nkind = \"submodule\"\npath = \".\"\n",
			want: "<PATH>: repository 'weird': unknown kind 'submodule'. Valid kinds are \"local\" and \"remote\".",
		},
		{
			name: "an unknown key on a local entry",
			text: "[[repo]]\nname = \"typo\"\nkind = \"local\"\npath = \"<TREE>\"\nbrnach = \"main\"\n",
			want: "<PATH>: repository 'typo': unknown key(s) brnach on a local entry. A local entry takes: kind, name, path.",
		},
		{
			name: "a remote key on a local entry",
			text: "[[repo]]\nname = \"mixed\"\nkind = \"local\"\npath = \"<TREE>\"\nref = \"main\"\n",
			want: "<PATH>: repository 'mixed': unknown key(s) ref on a local entry. A local entry takes: kind, name, path.",
		},
		{
			name: "a missing path",
			text: "[[repo]]\nname = \"orphan\"\nkind = \"local\"\n",
			want: "<PATH>: repository 'orphan': 'path' is required.",
		},
		{
			name: "a path that is not a string",
			text: "[[repo]]\nname = \"orphan\"\nkind = \"local\"\npath = 3\n",
			want: "<PATH>: repository 'orphan': 'path' must be a non-empty string, got 3.",
		},
		{
			name: "a path that is not a directory",
			text: "[[repo]]\nname = \"gone\"\nkind = \"local\"\npath = \"<DIR>/nowhere\"\n",
			want: "<PATH>: repository 'gone': path <DIR>/nowhere is not a directory. A local entry names a working tree on this machine.",
		},
		{
			name: "a missing render",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\nref = \"main\"\ncache = \"/var/cache/afar\"\n",
			want: "<PATH>: repository 'afar': 'render' is required and has no default. Declare render = true if rendering runs against a checkout of this repository (directive resolution needs a source tree), or render = false if it does not.",
		},
		{
			name: "a render that is not a boolean",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\nref = \"main\"\ncache = \"/var/cache/afar\"\nrender = \"yes\"\n",
			want: "<PATH>: repository 'afar': 'render' must be true or false, got 'yes'.",
		},
		{
			name: "a missing repo",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nref = \"main\"\ncache = \"/var/cache/afar\"\nrender = false\n",
			want: "<PATH>: repository 'afar': 'repo' is required.",
		},
		{
			name: "a missing ref",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\ncache = \"/var/cache/afar\"\nrender = false\n",
			want: "<PATH>: repository 'afar': 'ref' is required.",
		},
		{
			name: "a missing cache",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\nref = \"main\"\nrender = false\n",
			want: "<PATH>: repository 'afar': 'cache' is required.",
		},
		{
			name: "an unknown key on a remote entry",
			text: "[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\nref = \"main\"\ncache = \"/c\"\nrender = false\nbranch = \"x\"\n",
			want: "<PATH>: repository 'afar': unknown key(s) branch on a remote entry. A remote entry takes: cache, kind, name, ref, render, repo.",
		},
		{
			name: "duplicate names",
			text: "[[repo]]\nname = \"twice\"\nkind = \"local\"\npath = \"<TREE>\"\n\n[[repo]]\nname = \"twice\"\nkind = \"local\"\npath = \"<TREE>\"\n",
			want: "<PATH>: duplicate repository name 'twice', declared by entry #1 and entry #2.",
		},
		{
			name:   "a document that is not TOML",
			text:   "[[repo]\nname = ",
			want:   "<PATH> is not valid TOML: ",
			prefix: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			dir := t.TempDir()
			treePath := tree(t, dir, "t")
			text := strings.ReplaceAll(testCase.text, "<TREE>", treePath)
			text = strings.ReplaceAll(text, "<DIR>", dir)
			path := write(t, dir, text)
			want := strings.ReplaceAll(testCase.want, "<PATH>", path)
			want = strings.ReplaceAll(want, "<TREE>", treePath)
			want = strings.ReplaceAll(want, "<DIR>", dir)

			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load(%q) returned no error", text)
			}
			var registryError *Error
			if !errors.As(err, &registryError) {
				t.Fatalf("error type = %T, want *registry.Error", err)
			}
			if testCase.prefix {
				if !strings.HasPrefix(err.Error(), want) {
					t.Errorf("error = %q, want a message starting %q", err, want)
				}
				return
			}
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

func TestAMissingFileNamesThePathAndTheRemedy(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope.toml")
	_, err := Load(missing)
	want := "No editor registry at " + missing + ". Create it with one [[repo]] " +
		"block per repository: name, kind, and (for kind = \"local\") path."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestAnEmptyPathReadsTheMachineLocalRegistry(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := Load("")
	if err == nil {
		t.Fatal("Load(\"\") found a registry in an empty home")
	}
	if !strings.Contains(err.Error(), DefaultPath()) {
		t.Errorf("error %q does not name the default path %q", err, DefaultPath())
	}
}

func TestTheListingRendersOneLinePerEntry(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	local := tree(t, dir, "work")
	path := write(t, dir, `
[[repo]]
name = "here"
kind = "local"
path = "`+local+`"

[[repo]]
name = "afar"
kind = "remote"
repo = "smm-h/afar"
ref = "v1.2.3"
cache = "/var/cache/afar"
render = true

[[repo]]
name = "prose"
kind = "remote"
repo = "smm-h/prose"
ref = "main"
cache = "/var/cache/prose"
render = false
`)
	parsed, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{
		"here  local   " + local,
		"afar  remote  smm-h/afar@v1.2.3 [render, not served yet]",
		"prose  remote  smm-h/prose@main [no-render, not served yet]",
		"",
		"3 repository(ies) in " + path + ".",
	}
	got := parsed.RenderList()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("RenderList() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTheListingOfAnEmptyRegistrySaysSo(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := write(t, dir, "")
	parsed, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"No repositories in " + path + "."}
	if got := parsed.RenderList(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("RenderList() = %q, want %q", got, want)
	}
}
