package assembly

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/testisolation/go/hygiene"
)

// siteLinksBase is the site the relativizing pass recognises as its own.
const siteLinksBase = "https://docs.example.com"

// siteLinksTree writes every named file into a fresh tree and returns its root.
//
// The keys are site-relative paths, as the pass takes them.
func siteLinksTree(t *testing.T, files map[string]string) string {
	t.Helper()
	hygiene.Isolate(t)
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.Join(strings.Split(rel, "/")...))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

// siteLinksModTimes is every named file's modification time.
func siteLinksModTimes(t *testing.T, root string, rels []string) map[string]time.Time {
	t.Helper()
	times := make(map[string]time.Time, len(rels))
	for _, rel := range rels {
		info, err := os.Stat(filepath.Join(root, filepath.Join(strings.Split(rel, "/")...)))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		times[rel] = info.ModTime()
	}
	return times
}

// siteLinksRead is the text of a site-relative file.
func siteLinksRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.Join(strings.Split(rel, "/")...)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// siteLinksPage wraps link markup in the smallest document the pass reads.
func siteLinksPage(body string) string {
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<body>\n" + body + "\n</body>\n</html>\n"
}

func TestRelativizeSiteLinksRewritesEveryLinkThatNamesThisSite(t *testing.T) {
	for _, test := range []struct {
		name string
		// files is the whole tree, site-relative path to content.
		files map[string]string
		// pages are the HTML pages handed to the pass.
		pages []string
		// want is the content each named page carries afterwards; a page
		// absent from it must be byte-identical to what the tree was given.
		want map[string]string
		// wantChanged is the site-relative page list the pass reports.
		wantChanged []string
		// wantErr, when set, is text the refusal has to carry.
		wantErr []string
	}{
		{
			name: "a page at the site root addresses the root and a directory",
			files: map[string]string{
				"index.html": siteLinksPage(
					`<a href="` + siteLinksBase + `/">Home</a>` + "\n" +
						`<a href="` + siteLinksBase + `">Root</a>` + "\n" +
						`<a href="` + siteLinksBase + `/blog/">Blog</a>`),
				"blog/index.html": siteLinksPage(`<a href="../">Up</a>`),
			},
			pages: []string{"index.html", "blog/index.html"},
			want: map[string]string{
				"index.html": siteLinksPage(
					`<a href="./">Home</a>` + "\n" +
						`<a href="./">Root</a>` + "\n" +
						`<a href="blog/">Blog</a>`),
			},
			wantChanged: []string{"index.html"},
		},
		{
			name: "a page one level in climbs out once",
			files: map[string]string{
				"alpha/index.html": siteLinksPage(
					`<a href="` + siteLinksBase + `/">Home</a>` + "\n" +
						`<a href="` + siteLinksBase + `/blog/">Blog</a>`),
			},
			pages: []string{"alpha/index.html"},
			want: map[string]string{
				"alpha/index.html": siteLinksPage(
					`<a href="../">Home</a>` + "\n" +
						`<a href="../blog/">Blog</a>`),
			},
			wantChanged: []string{"alpha/index.html"},
		},
		{
			name: "a page two levels in climbs out twice",
			files: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="` + siteLinksBase + `/">Home</a>` + "\n" +
						`<a href="` + siteLinksBase + `/blog/hello/">A post</a>`),
			},
			pages: []string{"alpha/guide/index.html"},
			want: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="../../">Home</a>` + "\n" +
						`<a href="../../blog/hello/">A post</a>`),
			},
			wantChanged: []string{"alpha/guide/index.html"},
		},
		{
			name: "a link to another host is somebody else's address",
			files: map[string]string{
				"alpha/index.html": siteLinksPage(
					`<a href="https://example.org/blog/">Elsewhere</a>` + "\n" +
						`<a href="https://docs.example.com.evil.test/blog/">Not us</a>`),
			},
			pages:       []string{"alpha/index.html"},
			wantChanged: []string{},
		},
		{
			name: "a page that is already relative is not written",
			files: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="../../blog/">Blog</a>` + "\n" +
						`<a href="#top">Top</a>`),
			},
			pages:       []string{"alpha/guide/index.html"},
			wantChanged: []string{},
		},
		{
			name: "a query string keeps the escaping the attribute was written with",
			files: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="` + siteLinksBase + `/search/?q=a&amp;p=2#hit">Search</a>`),
			},
			pages: []string{"alpha/guide/index.html"},
			want: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="../../search/?q=a&amp;p=2#hit">Search</a>`),
			},
			wantChanged: []string{"alpha/guide/index.html"},
		},
		{
			name: "a base spelled without its trailing slash names the emitted page",
			files: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="` + siteLinksBase + `/blog">Blog</a>` + "\n" +
						`<a href="` + siteLinksBase + `/missing">Missing</a>` + "\n" +
						`<a href="` + siteLinksBase + `/llms.txt">Machines</a>`),
				"blog/index.html": siteLinksPage(`<a href="../">Up</a>`),
				"llms.txt":        "# machines\n",
			},
			pages: []string{"alpha/guide/index.html", "blog/index.html"},
			want: map[string]string{
				"alpha/guide/index.html": siteLinksPage(
					`<a href="../../blog/">Blog</a>` + "\n" +
						`<a href="../../missing">Missing</a>` + "\n" +
						`<a href="../../llms.txt">Machines</a>`),
			},
			wantChanged: []string{"alpha/guide/index.html"},
		},
		{
			name: "a base spelled with entities is refused rather than left behind",
			files: map[string]string{
				"alpha/index.html": siteLinksPage(
					`<a href="https://docs.example.com&#47;blog/">Blog</a>`),
			},
			pages:   []string{"alpha/index.html"},
			wantErr: []string{"alpha/index.html", "https://docs.example.com&#47;blog/"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := siteLinksTree(t, test.files)
			rels := make([]string, 0, len(test.files))
			for rel := range test.files {
				rels = append(rels, rel)
			}
			slices.Sort(rels)
			before := siteLinksModTimes(t, root, rels)
			time.Sleep(10 * time.Millisecond)

			changed, err := RelativizeSiteLinks(root, siteLinksBase, test.pages, effects.Unbound())
			if len(test.wantErr) > 0 {
				if err == nil {
					t.Fatalf("the pass accepted the tree, want a refusal naming %v", test.wantErr)
				}
				for _, want := range test.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not name %q: %v", want, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("relativizing: %v", err)
			}
			if !reflect.DeepEqual(changed, test.wantChanged) {
				t.Errorf("changed = %v, want %v", changed, test.wantChanged)
			}

			after := siteLinksModTimes(t, root, rels)
			for _, rel := range rels {
				want, rewritten := test.want[rel]
				if !rewritten {
					want = test.files[rel]
				}
				if got := siteLinksRead(t, root, rel); got != want {
					t.Errorf("%s =\n%s\nwant\n%s", rel, got, want)
				}
				if rewritten {
					continue
				}
				if !after[rel].Equal(before[rel]) {
					t.Errorf("%s was rewritten with the content it already had", rel)
				}
			}
		})
	}
}

func TestRelativizeSiteLinksChangesNothingOnASecondRun(t *testing.T) {
	// The pass runs on every deploy, over pages earlier deploys already
	// repaired, so a second run has to be a no-op or the tree never settles.
	files := map[string]string{
		"index.html": siteLinksPage(
			`<a href="` + siteLinksBase + `/">Home</a>` + "\n" +
				`<a href="` + siteLinksBase + `/blog/">Blog</a>`),
		"alpha/guide/index.html": siteLinksPage(
			`<a href="` + siteLinksBase + `/blog">Blog</a>` + "\n" +
				`<a href="https://example.org/">Elsewhere</a>`),
		"blog/index.html": siteLinksPage(`<a href="../">Up</a>`),
	}
	root := siteLinksTree(t, files)
	pages := []string{"alpha/guide/index.html", "blog/index.html", "index.html"}

	if _, err := RelativizeSiteLinks(root, siteLinksBase, pages, effects.Unbound()); err != nil {
		t.Fatalf("the first run: %v", err)
	}
	first := make(map[string]string, len(files))
	for rel := range files {
		first[rel] = siteLinksRead(t, root, rel)
	}

	changed, err := RelativizeSiteLinks(root, siteLinksBase, pages, effects.Unbound())
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("the second run reports %v, want nothing", changed)
	}
	for rel := range files {
		if got := siteLinksRead(t, root, rel); got != first[rel] {
			t.Errorf("%s moved on the second run:\n%s\nwas\n%s", rel, got, first[rel])
		}
	}
}

func TestRelativizeSiteLinksRefusesWithoutACanonicalBase(t *testing.T) {
	// With no base there is no way to tell this site's addresses from anybody
	// else's, so the pass has nothing to recognise and says so.
	root := siteLinksTree(t, map[string]string{
		"index.html": siteLinksPage(`<a href="` + siteLinksBase + `/blog/">Blog</a>`),
	})
	for _, base := range []string{"", "/"} {
		changed, err := RelativizeSiteLinks(root, base, []string{"index.html"}, effects.Unbound())
		if err == nil {
			t.Fatalf("base %q was accepted, reporting %v", base, changed)
		}
		if !strings.Contains(err.Error(), "canonical base") {
			t.Errorf("base %q: the refusal does not name the canonical base: %v", base, err)
		}
	}
}

func TestRelativeSiteHrefCutsTheAttributeAsWritten(t *testing.T) {
	// The tree behind these: a directory the build emitted a page into, and a
	// file beside it.
	root := siteLinksTree(t, map[string]string{
		"blog/index.html": siteLinksPage(`<a href="../">Up</a>`),
		"llms.txt":        "# machines\n",
	})
	for _, test := range []struct {
		name string
		ref  string
		hop  string
		want string
		// ours is whether the reference named this site at all.
		ours bool
		// wantErr, when set, is text the refusal has to carry.
		wantErr string
	}{
		{name: "another host", ref: "https://example.org/blog/", hop: "../"},
		{name: "a relative reference", ref: "../blog/", hop: "../"},
		{
			name: "the root from the root", ref: siteLinksBase, hop: "",
			want: "./", ours: true,
		},
		{
			name: "the root with its slash from the root",
			ref:  siteLinksBase + "/", hop: "", want: "./", ours: true,
		},
		{
			name: "the root from one level in", ref: siteLinksBase, hop: "../",
			want: "../", ours: true,
		},
		{
			name: "a directory from two levels in",
			ref:  siteLinksBase + "/blog/", hop: "../../",
			want: "../../blog/", ours: true,
		},
		{
			name: "a directory named without its slash",
			ref:  siteLinksBase + "/blog", hop: "../../",
			want: "../../blog/", ours: true,
		},
		{
			name: "a path naming nothing in the tree",
			ref:  siteLinksBase + "/missing", hop: "../../",
			want: "../../missing", ours: true,
		},
		{
			name: "a file in the tree", ref: siteLinksBase + "/llms.txt",
			hop: "../../", want: "../../llms.txt", ours: true,
		},
		{
			name: "a slashless directory keeps its query after the slash",
			ref:  siteLinksBase + "/blog?x=1", hop: "../../",
			want: "../../blog/?x=1", ours: true,
		},
		{
			// The lookup that decides on the slash must stay inside the
			// tree: a remainder climbing out of it is looked up as the root
			// path it cleans to, never as a path beside the tree.
			name: "a remainder that climbs out is looked up inside the tree",
			ref:  siteLinksBase + "/../blog", hop: "../../",
			want: "../../../blog/", ours: true,
		},
		{
			name: "a query string, escaping and all",
			ref:  siteLinksBase + "/search/?q=a&amp;p=2#hit", hop: "../",
			want: "../search/?q=a&amp;p=2#hit", ours: true,
		},
		{
			name:    "a base spelled with an entity",
			ref:     "https://docs.example.com&#47;blog/",
			hop:     "../",
			wantErr: "https://docs.example.com&#47;blog/",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ours, err := relativeSiteHref(test.ref, siteLinksBase, test.hop, root)
			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("relativeSiteHref(%q) = %q, %v, want a refusal", test.ref, got, ours)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("the refusal does not name %q: %v", test.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("relativeSiteHref(%q): %v", test.ref, err)
			}
			if ours != test.ours {
				t.Fatalf("relativeSiteHref(%q) claimed ours = %v, want %v", test.ref, ours, test.ours)
			}
			if got != test.want {
				t.Errorf("relativeSiteHref(%q) = %q, want %q", test.ref, got, test.want)
			}
		})
	}
}
