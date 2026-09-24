package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The site a mounted fixture is served from, and the slug it is served under.
const (
	mountDocsBase = "https://docs.example.com"
	mountSlug     = "alpha"
)

// mountPost is the post the mount fixtures publish: it declares a term of its
// own and mentions the term the guide declares, so the build writes a
// cross-page term link in each direction.
const mountPost = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
	"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\n" +
	"This is the post content.\n\n" +
	"## Chained revision\n\n" +
	"<dfn>Chained revision</dfn> is a recorded edge between two schema states.\n\n" +
	"The widget catalog is described at length in the guide.\n"

// mountGuide declares a term of its own and mentions the post's, so the two
// cross-page term links cross the mount boundary in opposite directions.
const mountGuide = "# Guide\n\nHow to.\n\n" +
	"## Widget catalog\n\n" +
	"<dfn>Widget catalog</dfn> is a list of every widget this project ships.\n\n" +
	"See the notes on chained revision for the history model.\n"

// mountFixture is the project both mount cases build, with or without a
// declared site mount.
func mountFixture(mounted bool) fixture {
	f := fixture{
		Docs: map[string]string{
			"index.md": "# Test Project\n\nWelcome.\n",
			"guide.md": mountGuide,
		},
		Files:  postFiles(map[string]string{"hello.md": mountPost}),
		Config: map[string]any{},
	}
	if mounted {
		f.Config["topology"] = map[string]any{
			"docs_base": mountDocsBase, "slug": mountSlug,
		}
	}
	return f
}

// canonicalOf is the canonical URL a page declares, "" when it declares none.
func canonicalOf(html string) string {
	match := regexp.MustCompile(`<link rel="canonical" href="([^"]*)"`).FindStringSubmatch(html)
	if match == nil {
		return ""
	}
	return match[1]
}

// hrefsOf lists every href a page carries, the metadata links included.
func hrefsOf(html string) []string {
	var hrefs []string
	for _, match := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		hrefs = append(hrefs, match[1])
	}
	return hrefs
}

// anchorsOf lists only the hrefs a click follows -- not the canonical, which
// is metadata.
func anchorsOf(html string) []string {
	var hrefs []string
	for _, match := range regexp.MustCompile(
		`<a\b[^>]*?\bhref="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		hrefs = append(hrefs, match[1])
	}
	return hrefs
}

func TestBuildUnderASiteMount(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	mounted := buildFixture(t, mountFixture(true))
	standalone := buildFixture(t, mountFixture(false))

	t.Run("a mounted build writes no 404 page", func(t *testing.T) {
		// It would be unreachable: only the served root's 404 is ever
		// asked for.
		if mounted.exists("404.html") {
			t.Error("a mounted project wrote a 404 page nothing can reach")
		}
		if !standalone.exists("404.html") {
			t.Error("a standalone project, whose output root IS the served root, wrote none")
		}
	})

	t.Run("a post's canonical is the site's blog address", func(t *testing.T) {
		got := canonicalOf(mounted.page(t, "blog/hello-world/index.html"))
		if want := mountDocsBase + "/blog/hello-world/"; got != want {
			t.Errorf("the mounted post canonicalizes to %q, want %q", got, want)
		}
		got = canonicalOf(standalone.page(t, "blog/hello-world/index.html"))
		if want := "https://example.com/blog/hello-world/"; got != want {
			t.Errorf("the standalone post canonicalizes to %q, want %q", got, want)
		}
	})

	t.Run("a mounted project page reaches a post by climbing the mount", func(t *testing.T) {
		// A hop back to the project's own output root resolves inside
		// site/<slug>/, where the assembly does not put posts. One level
		// further out is the site root, where it does. guide/index.html is
		// one deep, so the site root is "../../" and the post is
		// "../../blog/hello-world/".
		guide := mounted.page(t, "guide/index.html")
		if !contains(hrefsOf(guide), "../../blog/hello-world/") {
			t.Error("the mounted guide does not reach the post by climbing the mount")
		}
		if !contains(hrefsOf(guide), "../../blog/") {
			t.Error("the mounted guide does not reach the listing by climbing the mount")
		}
		for _, href := range anchorsOf(guide) {
			if strings.HasPrefix(href, mountDocsBase) {
				t.Errorf("a click follows the absolute link %q instead of a relative hop", href)
			}
		}
	})

	t.Run("a standalone project page reaches a post relatively", func(t *testing.T) {
		guide := standalone.page(t, "guide/index.html")
		if !contains(hrefsOf(guide), "../blog/hello-world/") {
			t.Error("the standalone guide does not reach the post at its own output root")
		}
	})

	t.Run("a cross-page term link crosses the same boundary", func(t *testing.T) {
		guide := mounted.page(t, "guide/index.html")
		crossed := false
		for _, href := range hrefsOf(guide) {
			if strings.HasPrefix(href, "../../blog/hello-world/#") {
				crossed = true
			}
			if strings.HasPrefix(href, "../blog/") {
				t.Errorf("the term link %q stops at the project's output root", href)
			}
		}
		if !crossed {
			t.Error("the mounted guide carries no term link into the post")
		}
	})

	t.Run("a glossary source link crosses it too", func(t *testing.T) {
		glossary := mounted.page(t, "glossary/index.html")
		if !contains(hrefsOf(glossary), "../../blog/hello-world/") {
			t.Error("the mounted glossary does not reach the post by climbing the mount")
		}
		for _, href := range hrefsOf(glossary) {
			if strings.HasPrefix(href, "../blog/") {
				t.Errorf("the glossary link %q stops at the project's output root", href)
			}
		}
		if !contains(hrefsOf(standalone.page(t, "glossary/index.html")), "../blog/hello-world/") {
			t.Error("the standalone glossary does not reach the post relatively")
		}
	})

	t.Run("a post's sidebar descends back into the project's mount", func(t *testing.T) {
		// A relative hop out of a post reaches the site root, not the
		// subtree, so a project page it links has to go back down through
		// the slug.
		post := mounted.page(t, "blog/hello-world/index.html")
		hrefs := hrefsOf(post)
		if !contains(hrefs, "../../"+mountSlug+"/glossary/") {
			t.Error("the mounted post's sidebar does not descend into the project's mount")
		}
		if contains(hrefs, "../../glossary/") {
			t.Error("the mounted post's sidebar points at the site root's own glossary")
		}
		for _, href := range anchorsOf(post) {
			if strings.HasPrefix(href, mountDocsBase) {
				t.Errorf("a click follows the absolute link %q", href)
			}
		}
		if !contains(hrefsOf(standalone.page(t, "blog/hello-world/index.html")), "../../glossary/") {
			t.Error("the standalone post's sidebar does not reach the glossary relatively")
		}
	})

	t.Run("a post's home link descends into the project's mount", func(t *testing.T) {
		// A bare hop out of a post lands on the assembled site's front
		// page, which belongs to whichever project is served at the root:
		// a link that resolves, to somebody else's page.
		post := mounted.page(t, "blog/hello-world/index.html")
		hrefs := hrefsOf(post)
		if !contains(hrefs, "../../"+mountSlug+"/index.html") {
			t.Error("the mounted post's home link does not name the project's own index")
		}
		if contains(hrefs, "../../index.html") {
			t.Error("the mounted post's home link names the site's front page")
		}
	})

	t.Run("a post's breadcrumbs stay at the site level", func(t *testing.T) {
		post := mounted.page(t, "blog/hello-world/index.html")
		hrefs := hrefsOf(post)
		if !contains(hrefs, "../../blog/") {
			t.Error("the mounted post's breadcrumb does not name the site-level listing")
		}
		if contains(hrefs, "../../"+mountSlug+"/blog/") {
			t.Error("the mounted post's breadcrumb descends into the project's mount")
		}
	})

	t.Run("the site-level files name the post at the site address", func(t *testing.T) {
		sitemap := mounted.page(t, "sitemap.xml")
		assertCarries(t, "sitemap.xml", sitemap,
			"<loc>"+mountDocsBase+"/blog/hello-world/</loc>")
		assertLacks(t, "sitemap.xml", sitemap, mountDocsBase+"/"+mountSlug+"/blog/")
		feed := mounted.page(t, "feed.xml")
		assertCarries(t, "feed.xml", feed, mountDocsBase+"/blog/hello-world/")
		assertLacks(t, "feed.xml", feed, mountDocsBase+"/"+mountSlug+"/blog/")
	})

	t.Run("the documentation pages keep the slug", func(t *testing.T) {
		// Only the site-level pages are mountless.
		got := canonicalOf(mounted.page(t, "guide/index.html"))
		if want := mountDocsBase + "/" + mountSlug + "/guide/"; got != want {
			t.Errorf("the mounted guide canonicalizes to %q, want %q", got, want)
		}
		assertCarries(t, "sitemap.xml", mounted.page(t, "sitemap.xml"),
			"<loc>"+mountDocsBase+"/"+mountSlug+"/guide/</loc>")
	})
}

func TestPostsTargetAgreesWithTheFullBuild(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A post is one page, so it has one address whichever build wrote it.
	// The assembly rebuilds posts on their own when only a post changed and
	// grafts that output beside the full build's; if the two targets
	// disagreed, whichever ran last would decide the site's addresses.
	for _, mounted := range []bool{true, false} {
		name := "standalone"
		if mounted {
			name = "mounted"
		}
		t.Run(name, func(t *testing.T) {
			built := buildFixture(t, mountFixture(mounted))
			fromFull := built.page(t, "blog/hello-world/index.html")

			if err := os.RemoveAll(built.output); err != nil {
				t.Fatalf("clearing the output: %v", err)
			}
			if _, err := Build(Options{
				DirPath: built.dir, Target: "posts", Stdout: &discard{},
			}, effects.Unbound()); err != nil {
				t.Fatalf("the posts-only build: %v", err)
			}
			fromPosts := readFile(t, filepath.Join(
				built.output, "blog", "hello-world", "index.html"))

			if fromPosts != fromFull {
				t.Error("the posts-only build wrote a different page than the full build")
			}
		})
	}
}
