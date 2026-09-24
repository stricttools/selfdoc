package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// reference reads a rendering recorded from the Python surface, which every
// renderer here is asserted byte-identical to.
func reference(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return string(content)
}

func TestRenderRosterIsByteIdenticalToThePython(t *testing.T) {
	t.Parallel()
	got := RenderRoster([]RosterEntry{
		fixtureRoster["home"], fixtureRoster["alpha"], fixtureRoster["beta"],
	}, "home")
	if want := reference(t, "roster-rendered.toml"); got != want {
		t.Errorf("rendered roster:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderRosterSortsByDeclaredSlug(t *testing.T) {
	t.Parallel()
	got := RenderRoster([]RosterEntry{
		{Slug: "beta", Repo: "owner/beta"}, {Slug: "alpha", Repo: "owner/alpha"},
	}, "alpha")
	alpha := strings.Index(got, `slug = "alpha"`)
	beta := strings.Index(got, `slug = "beta"`)
	if alpha < 0 || beta < 0 || alpha > beta {
		t.Errorf("blocks are not in slug order: alpha at %d, beta at %d", alpha, beta)
	}
}

// An empty home leaves a commented placeholder: a scaffolded roster that
// silently named some project home would be choosing the front page on the
// author's behalf.
func TestRenderRosterWithNoHomeLeavesThePlaceholder(t *testing.T) {
	t.Parallel()
	got := RenderRoster(nil, "")
	if want := reference(t, "roster-scaffold.toml"); got != want {
		t.Errorf("scaffolded roster:\n%q\nwant:\n%q", got, want)
	}
	if !strings.Contains(got, `home = "<slug>"`) {
		t.Error("the scaffold names no home and must say so with a placeholder")
	}
}

func TestARenderedRosterRoundTrips(t *testing.T) {
	t.Parallel()
	entries := []RosterEntry{
		{Slug: "alpha", Repo: "owner/alpha"}, {Slug: "beta", Repo: "owner/beta"},
	}
	roster, err := ParseRoster(RenderRoster(entries, "alpha"), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if roster.Home != "alpha" {
		t.Errorf("home = %q, want alpha", roster.Home)
	}
	if got := roster.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	for _, want := range entries {
		got, ok := roster.Get(want.Slug)
		if !ok || got != want {
			t.Errorf("Get(%q) = %v, %v; want %v", want.Slug, got, ok, want)
		}
	}
}

func TestTheScaffoldedRosterIsRefusedForNamingNoHome(t *testing.T) {
	t.Parallel()
	_, err := ParseRoster(RenderRoster(nil, ""), "")
	if err == nil || !strings.Contains(err.Error(), "carries no top-level 'home' key") {
		t.Fatalf("err = %v, want the no-home refusal", err)
	}
}

func TestRosterStringRendersTheDeclaredSlugsAndHome(t *testing.T) {
	t.Parallel()
	roster := NewRoster(fixtureRoster, "home")
	want := `Roster(projects=['alpha', 'beta', 'home'], home='home')`
	if got := roster.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// Every refusal, byte for byte as the Python raised it. The roster is a
// declaration a deploy reconciles a live site to, so a message that stops
// naming what is wrong stops being a fix anyone can act on.
func TestParseRosterRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "unknown top-level key",
			text: "projects = []\n",
			want: "roster.toml declares unknown top-level key(s) 'projects'. " +
				"The roster holds nothing but [[project]] blocks and the 'home' key.",
		},
		{
			name: "project is not a list",
			text: "home = \"a\"\nproject = \"x\"\n",
			want: "roster.toml: 'project' must be a list of [[project]] blocks.",
		},
		{
			name: "a block that is not a table",
			text: "home = \"a\"\nproject = [1]\n",
			want: "roster.toml: [[project]] #1 is not a table.",
		},
		{
			name: "unknown key on a block",
			text: "home = \"a\"\n[[project]]\nslug = \"a\"\nrepo = \"o/a\"\nrfe = \"typo\"\n",
			want: "roster.toml: [[project]] #1 declares unknown key(s) 'rfe'. " +
				"A [[project]] block carries exactly slug, repo.",
		},
		{
			name: "a missing required key",
			text: "home = \"a\"\n[[project]]\nslug = \"a\"\n",
			want: "roster.toml: [[project]] #1 is missing repo. Every declared " +
				"project names slug, repo.",
		},
		{
			name: "an empty required value",
			text: "home = \"a\"\n[[project]]\nslug = \"a\"\nrepo = \"\"\n",
			want: "roster.toml: [[project]] #1 is missing repo. Every declared " +
				"project names slug, repo.",
		},
		{
			name: "both required keys missing",
			text: "home = \"a\"\n[[project]]\n",
			want: "roster.toml: [[project]] #1 is missing slug, repo. Every " +
				"declared project names slug, repo.",
		},
		{
			name: "a slug colliding with an assembly directory",
			text: "home = \"blog\"\n[[project]]\nslug = \"blog\"\nrepo = \"o/blog\"\n",
			want: "roster.toml: [[project]] #1 claims the slug 'blog', which is " +
				"one of the assembly's own directories (blog, projects, " +
				"pagefind, _chrome). Give the project a different slug.",
		},
		{
			name: "a duplicate slug",
			text: "home = \"a\"\n[[project]]\nslug = \"a\"\nrepo = \"o/a\"\n" +
				"[[project]]\nslug = \"a\"\nrepo = \"o/b\"\n",
			want: "roster.toml: [[project]] #2 repeats the slug 'a', which an " +
				"earlier block already declares.",
		},
		{
			name: "no home key",
			text: "[[project]]\nslug = \"a\"\nrepo = \"o/a\"\n",
			want: "roster.toml carries no top-level 'home' key. Exactly one " +
				"declared project is the site's front page: its pages are " +
				"emitted at the site root instead of under site/<slug>/. There " +
				"is no default -- add home = \"<slug>\" naming one of the " +
				"declared projects. Declared projects: a.",
		},
		{
			// An assembly with no projects cannot name one of them home.
			name: "an empty roster is therefore homeless",
			text: "",
			want: "roster.toml carries no top-level 'home' key. Exactly one " +
				"declared project is the site's front page: its pages are " +
				"emitted at the site root instead of under site/<slug>/. There " +
				"is no default -- add home = \"<slug>\" naming one of the " +
				"declared projects. Declared projects: (none).",
		},
		{
			name: "home is not a string",
			text: "home = 7\n[[project]]\nslug = \"a\"\nrepo = \"o/a\"\n",
			want: "roster.toml: home must be a slug string, got 7.",
		},
		{
			name: "home is blank",
			text: "home = \"  \"\n[[project]]\nslug = \"a\"\nrepo = \"o/a\"\n",
			want: "roster.toml declares an empty home ('  '). The key is there, " +
				"so this is not a roster that forgot one -- it is a roster that " +
				"names no project as the site's front page, and a site needs " +
				"one. Give home the slug of a declared project. Declared " +
				"projects: a.",
		},
		{
			name: "home names an undeclared slug",
			text: "home = \"ghost\"\n[[project]]\nslug = \"a\"\nrepo = \"o/a\"\n",
			want: "roster.toml names 'ghost' as the home project, but no " +
				"[[project]] block declares it. The home project is an ordinary " +
				"declared project that happens to be served at the site root, " +
				"never a slug the roster does not carry. Declared projects: a.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseRoster(test.text, "")
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if err.Error() != test.want {
				t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), test.want)
			}
		})
	}
}

// The TOML parser's own wording is its own; only the sentence the roster puts
// around it is this package's to promise.
func TestInvalidTOMLIsARefusalNamingTheSource(t *testing.T) {
	t.Parallel()
	_, err := ParseRoster("[[project]\n", "")
	if err == nil || !strings.HasPrefix(err.Error(), "roster.toml is not valid TOML: ") {
		t.Fatalf("err = %v, want the invalid-TOML refusal", err)
	}
}

// A roster with no [[project]] block at all is legal and means an empty
// assembly -- which is then refused only for naming no home.
func TestAnAbsentProjectKeyIsAnEmptyAssembly(t *testing.T) {
	t.Parallel()
	_, err := ParseRoster("home = \"a\"\n", "")
	if err == nil || !strings.Contains(err.Error(), "no [[project]] block declares it") {
		t.Fatalf("err = %v, want the undeclared-home refusal", err)
	}
}

func TestParseRosterNamesTheSourceItWasGiven(t *testing.T) {
	t.Parallel()
	_, err := ParseRoster("projects = []\n", "owner/assembly:roster.toml")
	if err == nil || !strings.HasPrefix(err.Error(), "owner/assembly:roster.toml declares unknown") {
		t.Fatalf("err = %v, want the source named", err)
	}
}

func TestAMissingRosterFileIsARefusalCarryingAScaffold(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	_, err := LoadRoster(dir)
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	want := strings.Replace(
		reference(t, "missing-roster-error.txt"),
		"/nonexistent-dir-xyz/roster.toml",
		filepath.Join(dir, RosterPath), 1,
	)
	if err.Error() != want {
		t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), want)
	}
}

func TestLoadRosterReadsTheDeclaredMembership(t *testing.T) {
	root := assemblyTree(t)
	roster, err := LoadRoster(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := roster.Slugs(); strings.Join(got, ",") != "alpha,beta,home" {
		t.Errorf("Slugs() = %v", got)
	}
	if roster.Home != "home" {
		t.Errorf("home = %q, want home", roster.Home)
	}
}

func TestLoadRosterNamesTheFileInItsRefusals(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, RosterPath)
	write(t, path, "projects = []\n")
	_, err := LoadRoster(dir)
	if err == nil || !strings.HasPrefix(err.Error(), path+" declares unknown") {
		t.Fatalf("err = %v, want the refusal to name %s", err, path)
	}
}
