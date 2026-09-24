package preview

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The server half is asserted on the wire, against a real server on an
// ephemeral loopback port, because the properties worth having are wire
// properties: a directory address serves its index, a content type is right,
// and an address the tree does not carry answers 404 with the tree's own page.

// chromeAssetName is the content-hashed stylesheet the synthesized tree
// carries under the assembly's chrome directory.
const chromeAssetName = "minimal-abc123.css"

// notFoundPage is the tree's own 404, the body every unknown address answers
// with.
const notFoundPage = "<!DOCTYPE html>\n<html lang=\"en\"><head><title>404 " +
	"- Not found</title></head><body><h1>404</h1>" +
	"<p><a href=\"/\">Back to the front page</a></p></body></html>\n"

// synthesizeTree writes a tree shaped the way an assembled site is shaped, and
// returns its root.
func synthesizeTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "site")
	write := func(rel, content string) {
		testproject.WriteText(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	write("index.html", fixtureHTML("Front page", canonicalBase+"/", ""))
	write("alpha/index.html", fixtureHTML("Alpha", canonicalBase+"/alpha/", "1.0.0"))
	write("alpha/guide/index.html", fixtureHTML("Alpha Guide", canonicalBase+"/alpha/guide/", "1.0.0"))
	write("nav.json", `{"projects": []}`)
	write("feed.xml", "<?xml version=\"1.0\"?><feed></feed>\n")
	write("sitemap.xml", "<?xml version=\"1.0\"?><urlset></urlset>\n")
	write("robots.txt", "User-agent: *\nSitemap: "+canonicalBase+"/sitemap.xml\n")
	write("404.html", notFoundPage)
	write(site.ChromeDir+"/"+chromeAssetName, ":root{--x:1}\n")
	return root
}

func TestTheServer(t *testing.T) {
	hygiene.Isolate(t)
	root := synthesizeTree(t)
	port := serveTree(t, root)

	t.Run("the root serves the home project's front page", func(t *testing.T) {
		answer := request(t, port, "GET", "/")
		if answer.Status != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", answer.Status)
		}
		if got := answer.Headers.Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
		}
		if !contains(answer.Body, "Front page") {
			t.Errorf("the body does not carry the front page: %s", answer.Body)
		}
	})

	t.Run("a project page is served under its slug", func(t *testing.T) {
		answer := request(t, port, "GET", "/alpha/guide/")
		if answer.Status != http.StatusOK {
			t.Fatalf("GET /alpha/guide/ = %d, want 200", answer.Status)
		}
		if !contains(answer.Body, "Alpha Guide") {
			t.Errorf("the body does not carry the page: %s", answer.Body)
		}
	})

	t.Run("a directory without a trailing slash redirects", func(t *testing.T) {
		answer := request(t, port, "GET", "/alpha")
		if answer.Status != http.StatusMovedPermanently {
			t.Fatalf("GET /alpha = %d, want 301", answer.Status)
		}
		if got := answer.Headers.Get("Location"); got != "/alpha/" {
			t.Errorf("Location = %q, want /alpha/", got)
		}
		followed := requestFollowing(t, port, "GET", "/alpha")
		if followed.Status != http.StatusOK {
			t.Fatalf("following the redirect = %d, want 200", followed.Status)
		}
		if !contains(followed.Body, "Alpha") {
			t.Errorf("the followed body does not carry the page: %s", followed.Body)
		}
	})

	t.Run("content types come from the extension", func(t *testing.T) {
		for _, expected := range []struct{ path, contentType string }{
			{"/nav.json", "application/json; charset=utf-8"},
			{"/feed.xml", "application/xml; charset=utf-8"},
			{"/robots.txt", "text/plain; charset=utf-8"},
			{"/sitemap.xml", "application/xml; charset=utf-8"},
		} {
			answer := request(t, port, "GET", expected.path)
			if answer.Status != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", expected.path, answer.Status)
				continue
			}
			if got := answer.Headers.Get("Content-Type"); got != expected.contentType {
				t.Errorf("GET %s Content-Type = %q, want %q", expected.path, got, expected.contentType)
			}
		}
	})

	t.Run("the chrome asset is served as css", func(t *testing.T) {
		answer := request(t, port, "GET", "/"+site.ChromeDir+"/"+chromeAssetName)
		if answer.Status != http.StatusOK {
			t.Fatalf("the chrome asset = %d, want 200", answer.Status)
		}
		if got := answer.Headers.Get("Content-Type"); got != "text/css; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/css; charset=utf-8", got)
		}
		if len(answer.Body) == 0 {
			t.Error("the chrome asset answered an empty body")
		}
	})

	t.Run("an unknown address answers the tree's 404 page", func(t *testing.T) {
		answer := request(t, port, "GET", "/nothing/here/")
		if answer.Status != http.StatusNotFound {
			t.Fatalf("GET /nothing/here/ = %d, want 404", answer.Status)
		}
		if got := answer.Headers.Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
		}
		if !contains(answer.Body, "404") {
			t.Errorf("the body does not say 404: %s", answer.Body)
		}
	})

	t.Run("the 404 body is the site's own page", func(t *testing.T) {
		answer := request(t, port, "GET", "/nothing/here/")
		want, err := os.ReadFile(filepath.Join(root, "404.html"))
		if err != nil {
			t.Fatalf("reading the tree's 404: %v", err)
		}
		if string(answer.Body) != string(want) {
			t.Errorf("the 404 body is not the tree's own page:\n%s", answer.Body)
		}
	})

	t.Run("a path escaping the root is not served", func(t *testing.T) {
		answer := request(t, port, "GET", "/../../etc/passwd")
		if answer.Status != http.StatusNotFound {
			t.Fatalf("GET /../../etc/passwd = %d, want 404", answer.Status)
		}
	})

	t.Run("HEAD answers the headers without a body", func(t *testing.T) {
		answer := request(t, port, "HEAD", "/")
		if answer.Status != http.StatusOK {
			t.Fatalf("HEAD / = %d, want 200", answer.Status)
		}
		length, err := strconv.Atoi(answer.Headers.Get("Content-Length"))
		if err != nil || length <= 0 {
			t.Errorf("Content-Length = %q, want a positive length", answer.Headers.Get("Content-Length"))
		}
		if len(answer.Body) != 0 {
			t.Errorf("HEAD answered a body: %s", answer.Body)
		}
	})

	t.Run("a tree with no 404 page answers the bare one", func(t *testing.T) {
		bare := filepath.Join(t.TempDir(), "bare")
		testproject.WriteText(t, filepath.Join(bare, "index.html"), "<html></html>\n")
		barePort := serveTree(t, bare)
		answer := request(t, barePort, "GET", "/missing/")
		if answer.Status != http.StatusNotFound {
			t.Fatalf("GET /missing/ = %d, want 404", answer.Status)
		}
		if string(answer.Body) != BareNotFound {
			t.Errorf("the body is not the bare 404:\n%s", answer.Body)
		}
	})

	t.Run("a method other than GET or HEAD is refused", func(t *testing.T) {
		answer := request(t, port, "POST", "/")
		if answer.Status != http.StatusNotImplemented {
			t.Errorf("POST / = %d, want 501", answer.Status)
		}
	})

	t.Run("the bound port is a real one", func(t *testing.T) {
		if port <= 0 {
			t.Errorf("port = %d, want an ephemeral port", port)
		}
	})

	t.Run("every response forbids caching", func(t *testing.T) {
		answer := request(t, port, "GET", "/")
		if got := answer.Headers.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("one line per request names what was served", func(t *testing.T) {
		logged := &strings.Builder{}
		handler := NewPreviewHandler(root)
		handler.Log = logged
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		want := "GET / \"GET / HTTP/1.1\" 200 -\n"
		if logged.String() != want {
			t.Errorf("the log line is %q, want %q", logged.String(), want)
		}
	})
}
