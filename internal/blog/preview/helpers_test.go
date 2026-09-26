package preview

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// canonicalBase is the deployed base every fixture page carries its canonical
// against. It is not the loopback address the preview is served from: the
// pages carry the canonicals they would ship with, and verification asserts
// against those.
const canonicalBase = "https://docs.example.com"

// fixturePage is one documentation page a fixture checkout declares: the
// source path its manifest names, and the title its built page carries.
type fixturePage struct {
	Path  string
	Title string
}

// fixturePost is one post a fixture checkout declares.
type fixturePost struct {
	Slug  string
	Title string
	Date  string
	Path  string
}

// listingTOML is the curated project listing the home fixture publishes.
const listingTOML = `[[category]]
name = "Projects"

[[category.project]]
slug = "alpha"
blurb = "Does the alpha thing."

[[category.project]]
slug = "beta"
blurb = "Does the beta thing."
`

// fixtureHTML is a page shaped the way a real build's pages are shaped.
//
// The stylesheet names the project-local "style.css" a build writes at its own
// output root, which is what the graft really delivers; the shared generator
// re-points it at the site-level chrome asset.
func fixtureHTML(title, canonical, version string) string {
	versionAttr := ""
	if version != "" {
		versionAttr = fmt.Sprintf(" data-default-version=%q", version)
	}
	return "<!DOCTYPE html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		fmt.Sprintf("  <title>%s</title>\n", title) +
		fmt.Sprintf("  <link rel=\"canonical\" href=%q>\n", canonical) +
		"  <link rel=\"stylesheet\" href=\"style.css\">\n" +
		"</head>\n" +
		"<body>\n" +
		fmt.Sprintf("  <dialog class=\"search-dialog\" data-search-base=\"./\"%s></dialog>\n", versionAttr) +
		fmt.Sprintf("  <main><p>%s is a page with enough prose in it that the "+
			"search index has words to index and a fragment to render.</p></main>\n", title) +
		"</body>\n" +
		"</html>\n"
}

// fixtureManifest is the manifest a checkout publishes for itself.
func fixtureManifest(slug, name, version string, pages []fixturePage, posts []fixturePost) map[string]any {
	pageEntries := []any{}
	for _, entry := range pages {
		pageEntries = append(pageEntries, map[string]any{
			"path": entry.Path, "title": entry.Title,
		})
	}
	postEntries := []any{}
	for _, entry := range posts {
		postEntries = append(postEntries, map[string]any{
			"slug": entry.Slug, "title": entry.Title, "date": entry.Date,
			"path": entry.Path, "tags": []any{},
		})
	}
	return map[string]any{
		"schema_version": 1,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    name + " docs",
		"language":       "python",
		"base_url":       canonicalBase + "/" + slug,
		"author":         map[string]any{"name": "Test Author", "url": "https://author.example"},
		"pages":          pageEntries,
		"posts":          postEntries,
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
}

// checkoutSpec is one source checkout as the preview reads one.
type checkoutSpec struct {
	Root    string
	Slug    string
	Name    string
	Version string
	Home    bool
	Pages   []fixturePage
	Posts   []fixturePost
	Listing string
}

// writeCheckout writes a source checkout: config, manifest, build output.
//
// the build output directory is pre-populated rather than built, so the tests assert the
// graft and everything downstream of it without running a full documentation
// build per case. Every path below is where a real build puts it, which is the
// only reason the production functions can be pointed at it unchanged.
func writeCheckout(t *testing.T, spec checkoutSpec) string {
	t.Helper()
	testproject.MkdirAll(t, spec.Root)
	cfg := map[string]any{
		"name":          spec.Name,
		"base_url":      canonicalBase + "/" + spec.Slug,
		"docs":          "stricttools/docs/",
		"output":        "stricttools/.docs-cache/build/",
		"search_engine": "pagefind",
		"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
		"topology":      map[string]any{"slug": spec.Slug},
	}
	if spec.Version != "" {
		cfg["versions"] = []any{map[string]any{"version": spec.Version}}
	} else {
		cfg["unversioned"] = true
	}
	testproject.WriteJSON(t, filepath.Join(spec.Root, "selfdoc.json"), cfg)
	testproject.WriteJSON(t, filepath.Join(spec.Root, "stricttools", ".docs-state", "manifest.json"),
		fixtureManifest(spec.Slug, spec.Name, spec.Version, spec.Pages, spec.Posts))

	buildDir := filepath.Join(spec.Root, "stricttools", ".docs-cache", "build")
	for _, entry := range spec.Pages {
		stem := strings.TrimSuffix(entry.Path, ".md")
		address, out := "", "index.html"
		if stem != "index" {
			address, out = stem+"/", stem+"/index.html"
		}
		canonical := canonicalBase + "/" + spec.Slug + "/" + address
		if spec.Home {
			canonical = canonicalBase + "/" + address
		}
		testproject.WriteText(t, filepath.Join(buildDir, filepath.FromSlash(out)),
			fixtureHTML(entry.Title, canonical, spec.Version))
	}
	for _, entry := range spec.Posts {
		testproject.WriteText(t,
			filepath.Join(buildDir, "blog", entry.Slug, "index.html"),
			fixtureHTML(entry.Title, canonicalBase+"/blog/"+entry.Slug+"/", spec.Version))
	}
	// Every selfdoc build writes these for its own standalone hosting; the
	// graft is supposed to leave them behind.
	testproject.WriteText(t, filepath.Join(buildDir, "404.html"),
		fixtureHTML("Not found", "", ""))
	testproject.WriteText(t, filepath.Join(buildDir, "_headers"), "/*\n  X-Test: 1\n")
	if spec.Listing != "" {
		testproject.WriteText(t, filepath.Join(spec.Root, "stricttools", "docs", "projects.toml"), spec.Listing)
	}
	return spec.Root
}

// homeCheckout writes the home fixture: a front page, a CV page, the curated
// listing.
func homeCheckout(t *testing.T, root string, pages ...fixturePage) string {
	t.Helper()
	if len(pages) == 0 {
		pages = []fixturePage{{Path: "index.md", Title: "Front page"}}
	}
	return writeCheckout(t, checkoutSpec{
		Root: root, Slug: "home", Name: "Home", Home: true,
		Listing: listingTOML, Pages: pages,
	})
}

// serveTree serves dir on an ephemeral loopback port and returns the port.
func serveTree(t *testing.T, dir string) int {
	t.Helper()
	server, err := MakePreviewServer(dir, 0)
	if err != nil {
		t.Fatalf("MakePreviewServer: %v", err)
	}
	server.Handler().Log = io.Discard
	served := make(chan error, 1)
	go func() { served <- server.Serve() }()
	t.Cleanup(func() {
		if stopErr := server.Stop(); stopErr != nil {
			t.Errorf("Stop: %v", stopErr)
		}
		select {
		case serveErr := <-served:
			if serveErr != nil {
				t.Errorf("Serve: %v", serveErr)
			}
		case <-time.After(10 * time.Second):
			t.Error("the server did not stop")
		}
	})
	return server.Port()
}

// response is one answer off the wire.
type response struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// request performs one request against a served tree, writing the target
// bytes as given.
//
// The request is written onto a raw connection rather than built with
// http.Client, because the client normalizes a path before it sends it and
// "/../../etc/passwd" -- the address the containment boundary exists for --
// would never reach the server.
func request(t *testing.T, port int, method, target string) response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		t.Fatalf("dialing the preview: %v", err)
	}
	defer conn.Close()
	if deadlineErr := conn.SetDeadline(time.Now().Add(30 * time.Second)); deadlineErr != nil {
		t.Fatalf("setting the deadline: %v", deadlineErr)
	}
	raw := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n", method, target)
	if _, writeErr := conn.Write([]byte(raw)); writeErr != nil {
		t.Fatalf("writing the request: %v", writeErr)
	}
	answer, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer answer.Body.Close()
	body, err := io.ReadAll(answer.Body)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}
	return response{Status: answer.StatusCode, Headers: answer.Header, Body: body}
}

// requestFollowing performs one request and follows a single redirect.
func requestFollowing(t *testing.T, port int, method, target string) response {
	t.Helper()
	answer := request(t, port, method, target)
	if answer.Status == http.StatusMovedPermanently || answer.Status == http.StatusFound {
		return request(t, port, method, answer.Headers.Get("Location"))
	}
	return answer
}

// readJSON decodes a JSON file, failing the test when it is not one.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("%s is not a JSON object: %v", path, err)
	}
	return decoded
}

// isFile reports whether path is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDir reports whether path is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// contains reports whether a response body carries text.
func contains(body []byte, text string) bool {
	return strings.Contains(string(body), text)
}

// itoa renders an integer for a report line assembled by hand.
func itoa(value int) string { return strconv.Itoa(value) }
