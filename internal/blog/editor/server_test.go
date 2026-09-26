package editor

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/serving"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The authoring server: registry -> posts -> document read/write -> preview.
//
// Everything here runs against a real server bound to an ephemeral loopback
// port, because the properties worth asserting are the wire ones: what the
// shell can ask for, what it gets back, what a refusal looks like, and -- the
// one the whole preview design exists for -- that asking for a preview leaves
// the working tree untouched.

// workspace is a registry naming one local project and one (unserved)
// remote, with the state a server is built from.
type workspace struct {
	project string
	state   *State
}

// newWorkspace builds the fixture every test here starts from.
func newWorkspace(t *testing.T) workspace {
	t.Helper()
	project := makeProject(t, map[string]string{
		postHelloName: postHello, postDraftName: postDraft,
	})
	reg := writeRegistry(t, local("proj", project), remote(t, "afar"))
	return workspace{project: project, state: newState(t, reg, nil)}
}

// live serves the fixture and returns the project root and the bound port.
func live(t *testing.T) (workspace, int) {
	t.Helper()
	space := newWorkspace(t)
	return space, serveEditor(t, space.state)
}

func TestBinding(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("the server binds loopback only", func(t *testing.T) {
		_, port := live(t)
		if port <= 0 {
			t.Fatalf("port = %d, want a bound port", port)
		}
		if status, _ := request(t, port, "GET", "/api/repos", ""); status != 200 {
			t.Errorf("GET /api/repos = %d, want 200", status)
		}
	})

	t.Run("the host is hard coded", func(t *testing.T) {
		if serving.Host != "127.0.0.1" {
			t.Errorf("Host = %q, want 127.0.0.1", serving.Host)
		}
	})
}

func TestTheShell(t *testing.T) {
	hygiene.Isolate(t)
	_, port := live(t)

	t.Run("the root serves the shell", func(t *testing.T) {
		status, text := request(t, port, "GET", "/", "")
		if status != 200 {
			t.Fatalf("GET / = %d, want 200", status)
		}
		for _, want := range []string{"tinymoon", "/ui/app.js"} {
			if !strings.Contains(text, want) {
				t.Errorf("the shell does not name %q", want)
			}
		}
	})

	t.Run("the app module is served", func(t *testing.T) {
		status, text := request(t, port, "GET", "/ui/app.js", "")
		if status != 200 {
			t.Fatalf("GET /ui/app.js = %d, want 200", status)
		}
		if !strings.Contains(text, "createEditor") {
			t.Error("the app module does not mount an editor")
		}
	})

	t.Run("the tinymoon editor tier is served", func(t *testing.T) {
		for _, rel := range []string{"js/editor.js", "js/completion.js", "css/editor.css"} {
			status, text := request(t, port, "GET", "/tinymoon/"+rel, "")
			if status != 200 {
				t.Errorf("GET /tinymoon/%s = %d, want 200", rel, status)
				continue
			}
			if !strings.Contains(text, rel) {
				t.Errorf("/tinymoon/%s served something else: %q", rel, text)
			}
		}
	})

	t.Run("an asset outside the tree is refused", func(t *testing.T) {
		status, _ := request(t, port, "GET", "/tinymoon/../../etc/passwd", "")
		if status != 400 && status != 404 {
			t.Errorf("status = %d, want 400 or 404", status)
		}
	})

	t.Run("an unknown route is a 404", func(t *testing.T) {
		if status, _ := request(t, port, "GET", "/nope", ""); status != 404 {
			t.Errorf("GET /nope = %d, want 404", status)
		}
	})

	t.Run("a method the editor answers nothing for is a 501", func(t *testing.T) {
		status, body := requestJSON(t, port, "DELETE", "/api/repos", "")
		if status != 501 {
			t.Errorf("DELETE /api/repos = %d, want 501", status)
		}
		if !strings.Contains(errorText(t, body), "Unsupported method") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("every response forbids caching", func(t *testing.T) {
		response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/repos")
		if err != nil {
			t.Fatalf("GET /api/repos: %v", err)
		}
		defer response.Body.Close()
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})
}

func TestRepoListing(t *testing.T) {
	hygiene.Isolate(t)
	_, port := live(t)

	t.Run("every registry entry is listed", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/repos", "")
		if status != 200 {
			t.Fatalf("GET /api/repos = %d, want 200", status)
		}
		repos, _ := body["repos"].([]any)
		if len(repos) != 2 {
			t.Fatalf("repos = %v, want two entries", repos)
		}
		first, _ := repos[0].(map[string]any)
		second, _ := repos[1].(map[string]any)
		if first["name"] != "proj" || second["name"] != "afar" {
			t.Errorf("names = %v, %v, want proj, afar", first["name"], second["name"])
		}
		if first["kind"] != "local" || first["served"] != true {
			t.Errorf("the local entry reads %v", first)
		}
		if second["kind"] != "remote" || second["served"] != false {
			t.Errorf("the remote entry reads %v", second)
		}
	})

	t.Run("a local entry lists its posts", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/repos/proj/posts", "")
		if status != 200 {
			t.Fatalf("GET /api/repos/proj/posts = %d, want 200", status)
		}
		byPath := map[string]map[string]any{}
		for _, raw := range body["posts"].([]any) {
			post, _ := raw.(map[string]any)
			byPath[post["path"].(string)] = post
		}
		if len(byPath) != 2 || byPath[postHelloName] == nil || byPath[postDraftName] == nil {
			t.Fatalf("posts = %v, want hello.md and later.md", byPath)
		}
		if byPath[postHelloName]["title"] != "Hello World" {
			t.Errorf("title = %v", byPath[postHelloName]["title"])
		}
		if byPath[postHelloName]["draft"] != false {
			t.Errorf("hello.md reads draft %v", byPath[postHelloName]["draft"])
		}
		if byPath[postDraftName]["draft"] != true {
			t.Errorf("later.md reads draft %v", byPath[postDraftName]["draft"])
		}
	})

	t.Run("an unknown repo is a 404", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/repos/ghost/posts", "")
		if status != 404 {
			t.Fatalf("status = %d, want 404", status)
		}
		if !strings.Contains(errorText(t, body), "ghost") {
			t.Errorf("error = %q, want it to name the repository", errorText(t, body))
		}
	})
}

func TestRemoteEntriesAreNotServedYet(t *testing.T) {
	hygiene.Isolate(t)
	space, port := live(t)

	t.Run("listing a remote entry's posts hard errors", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/repos/afar/posts", "")
		if status != 501 {
			t.Fatalf("status = %d, want 501", status)
		}
		if !strings.Contains(errorText(t, body), "remote entries not yet served") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("reading a remote document hard errors", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET",
			"/api/repos/afar/document?path=a.md", "")
		if status != 501 {
			t.Fatalf("status = %d, want 501", status)
		}
		if !strings.Contains(errorText(t, body), "remote entries not yet served") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("the helper refuses directly", func(t *testing.T) {
		entry, err := space.state.Registry().Get("afar")
		if err != nil {
			t.Fatalf("Get(afar): %v", err)
		}
		_, err = RepoPosts(entry, effects.Unbound())
		editorError := wantEditorError(t, err)
		if editorError.Status != 501 {
			t.Errorf("status = %d, want 501", editorError.Status)
		}
		if !strings.Contains(editorError.Message, "remote entries not yet served") {
			t.Errorf("message = %q", editorError.Message)
		}
	})
}

func TestDocumentRead(t *testing.T) {
	hygiene.Isolate(t)
	space, port := live(t)

	t.Run("reading a post returns its source", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET",
			"/api/repos/proj/document?path=hello.md", "")
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		if body["content"] != postHello {
			t.Errorf("content = %q", body["content"])
		}
		if body["path"] != postHelloName {
			t.Errorf("path = %v", body["path"])
		}
	})

	t.Run("a missing document is a 404", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET",
			"/api/repos/proj/document?path=ghost.md", "")
		if status != 404 {
			t.Fatalf("status = %d, want 404", status)
		}
		if !strings.Contains(errorText(t, body), "ghost.md") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("a path outside the posts directory is refused", func(t *testing.T) {
		for _, bad := range []string{"..%2Fsecret.md", "%2Fetc%2Fpasswd", ""} {
			status, _ := request(t, port, "GET",
				"/api/repos/proj/document?path="+bad, "")
			if status != 400 {
				t.Errorf("path=%q answered %d, want 400", bad, status)
			}
		}
	})

	t.Run("the helper refuses an escaping path", func(t *testing.T) {
		entry, err := space.state.Registry().Get("proj")
		if err != nil {
			t.Fatalf("Get(proj): %v", err)
		}
		_, err = ReadPost(entry, "../../etc/passwd")
		editorError := wantEditorError(t, err)
		if !strings.Contains(editorError.Message, "..") {
			t.Errorf("message = %q, want it to name the offending segment", editorError.Message)
		}
	})
}

func TestDocumentWrite(t *testing.T) {
	hygiene.Isolate(t)
	space, port := live(t)

	t.Run("a put saves to the working tree", func(t *testing.T) {
		edited := strings.Replace(postHello, "post content", "SAVED content", 1)
		status, body := requestJSON(t, port, "PUT",
			"/api/repos/proj/document?path=hello.md", edited)
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		if body["saved"] != true {
			t.Errorf("saved = %v", body["saved"])
		}
		onDisk := filepath.Join(space.project, "stricttools", "posts", postHelloName)
		if got := testproject.ReadText(t, onDisk); got != edited {
			t.Errorf("the saved file reads %q", got)
		}
	})

	t.Run("a put can create a new post", func(t *testing.T) {
		source := "+++\ntitle = \"Brand New\"\ndate = 2024-06-01\nslug = \"brand-new\"\n" +
			"tags = []\ndraft = true\ndirectives = false\n+++\nFresh.\n"
		status, _ := requestJSON(t, port, "PUT",
			"/api/repos/proj/document?path=brand-new.md", source)
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		created := filepath.Join(space.project, "stricttools", "posts", "brand-new.md")
		if got := testproject.ReadText(t, created); got != source {
			t.Errorf("the created file reads %q", got)
		}
	})

	t.Run("a put to a remote entry hard errors", func(t *testing.T) {
		status, body := requestJSON(t, port, "PUT",
			"/api/repos/afar/document?path=a.md", "x")
		if status != 501 {
			t.Fatalf("status = %d, want 501", status)
		}
		if !strings.Contains(errorText(t, body), "remote entries not yet served") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("the helper refuses an escaping path", func(t *testing.T) {
		entry, err := space.state.Registry().Get("proj")
		if err != nil {
			t.Fatalf("Get(proj): %v", err)
		}
		if _, err := SavePost(entry, "../escape.md", "nope", effects.Unbound()); err == nil {
			t.Fatal("want a refusal")
		} else {
			wantEditorError(t, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(space.project), "escape.md")); err == nil {
			t.Error("the refused write reached the disk")
		}
	})
}

func TestPreviewWritesNothing(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	space, port := live(t)
	entry, err := space.state.Registry().Get("proj")
	if err != nil {
		t.Fatalf("Get(proj): %v", err)
	}

	t.Run("the preview helper leaves the tree alone", func(t *testing.T) {
		edited := strings.Replace(postHello, "post content", "buffer content", 1)
		before := treeFingerprint(t, space.project)
		html, err := RenderPreview(entry, postHelloName, edited, effects.Unbound())
		if err != nil {
			t.Fatalf("RenderPreview: %v", err)
		}
		if !strings.Contains(html, "buffer content") {
			t.Error("the rendered page does not carry the buffer")
		}
		if after := treeFingerprint(t, space.project); after != before {
			t.Error("the preview helper mutated the working tree")
		}
	})

	t.Run("the preview endpoint leaves the tree alone", func(t *testing.T) {
		edited := strings.Replace(postHello, "post content", "over the wire", 1)
		before := treeFingerprint(t, space.project)
		status, body := requestJSON(t, port, "POST",
			"/api/repos/proj/preview?path=hello.md", edited)
		after := treeFingerprint(t, space.project)
		if status != 200 {
			t.Fatalf("status = %d, want 200: %v", status, body)
		}
		html, _ := body["html"].(string)
		if !strings.Contains(html, "over the wire") {
			t.Error("the answer does not carry the buffer")
		}
		if after != before {
			t.Error("the preview endpoint mutated the working tree")
		}
	})

	t.Run("a draft buffer previews", func(t *testing.T) {
		html, err := RenderPreview(entry, postDraftName, postDraft, effects.Unbound())
		if err != nil {
			t.Fatalf("RenderPreview: %v", err)
		}
		if !strings.Contains(html, "Not yet.") {
			t.Error("the draft did not render")
		}
	})

	t.Run("a broken buffer reports the defect", func(t *testing.T) {
		broken := "+++\ntitle = \"No Date\"\ndirectives = false\n+++\nbody\n"
		status, body := requestJSON(t, port, "POST",
			"/api/repos/proj/preview?path=hello.md", broken)
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(strings.ToLower(errorText(t, body)), "date") {
			t.Errorf("error = %q, want it to name the missing field", errorText(t, body))
		}
	})

	t.Run("previewing a remote entry hard errors", func(t *testing.T) {
		status, body := requestJSON(t, port, "POST",
			"/api/repos/afar/preview?path=a.md", "x")
		if status != 501 {
			t.Fatalf("status = %d, want 501", status)
		}
		if !strings.Contains(errorText(t, body), "remote entries not yet served") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("a rendered preview is served at the address it publishes to", func(t *testing.T) {
		status, _ := requestJSON(t, port, "POST",
			"/api/repos/proj/preview?path=hello.md", postHello)
		if status != 200 {
			t.Fatalf("the preview answered %d", status)
		}
		served, text := request(t, port, "GET", "/preview/proj/blog/hello-world/", "")
		if served != 200 {
			t.Fatalf("the held preview answered %d, want 200", served)
		}
		if !strings.Contains(text, "Hello World") {
			t.Error("the served page is not the previewed document")
		}
	})
}

func TestTheEventStream(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	space, port := live(t)
	_ = space

	t.Run("a preview is streamed to a connected client", func(t *testing.T) {
		// No client timeout: the stream is held open on purpose, and a
		// deadline on the whole body read would end it.
		client := &http.Client{}
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/events")
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("GET /events = %d, want 200", response.StatusCode)
		}
		if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want text/event-stream", got)
		}

		edited := strings.Replace(postHello, "post content", "streamed content", 1)
		go postPreview(port, edited)

		frames := make(chan map[string]any, 1)
		go func() {
			reader := bufio.NewReader(response.Body)
			event := ""
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					close(frames)
					return
				}
				line = strings.TrimRight(line, "\n")
				switch {
				case strings.HasPrefix(line, "event:"):
					event = strings.TrimSpace(line[len("event:"):])
				case strings.HasPrefix(line, "data:") && event == "preview":
					var payload map[string]any
					if json.Unmarshal([]byte(strings.TrimSpace(line[len("data:"):])), &payload) == nil {
						frames <- payload
						return
					}
				}
			}
		}()

		select {
		case payload, ok := <-frames:
			if !ok {
				t.Fatal("no preview event arrived on the stream")
			}
			if payload["repo"] != "proj" || payload["path"] != postHelloName {
				t.Errorf("the frame reads %v", payload)
			}
			html, _ := payload["html"].(string)
			if !strings.Contains(html, "streamed content") {
				t.Error("the streamed document is not the previewed buffer")
			}
		case <-time.After(60 * time.Second):
			t.Fatal("no preview event arrived on the stream")
		}
	})

	t.Run("an idle stream is held with a comment", func(t *testing.T) {
		state := newWorkspace(t).state
		state.heartbeat = 20 * time.Millisecond
		idlePort := serveEditor(t, state)

		client := &http.Client{}
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(idlePort) + "/events")
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer response.Body.Close()

		reader := bufio.NewReader(response.Body)
		first, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		if strings.TrimRight(first, "\n") != ": connected" {
			t.Errorf("the stream opens with %q", first)
		}
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				t.Fatalf("reading the stream: %v", readErr)
			}
			if strings.TrimRight(line, "\n") == ": ping" {
				return
			}
		}
	})

	t.Run("stopping the server releases a held stream", func(t *testing.T) {
		state := newWorkspace(t).state
		server, err := NewServer(state, 0)
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		go server.Serve()

		client := &http.Client{}
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(server.Port()) + "/events")
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer response.Body.Close()

		released := make(chan error, 1)
		go func() {
			_, readErr := bufio.NewReader(response.Body).ReadString('\x00')
			released <- readErr
		}()

		if stopErr := server.Stop(); stopErr != nil {
			t.Fatalf("Stop: %v", stopErr)
		}
		select {
		case <-released:
		case <-time.After(30 * time.Second):
			t.Fatal("the stop did not release the held stream")
		}
		if count := state.Channel().Count(); count != 0 {
			t.Errorf("the channel still holds %d client(s)", count)
		}
	})
}

// postPreview posts one preview off the test's own goroutine, where a
// failure cannot be reported through t.
func postPreview(port int, content string) {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/repos/proj/preview?path=hello.md"
	response, err := http.Post(url, "text/markdown", strings.NewReader(content))
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

// wantEditorError fails the test unless err is one of the editor's own
// refusals, and returns it.
func wantEditorError(t *testing.T, err error) *Error {
	t.Helper()
	if err == nil {
		t.Fatal("want a refusal")
	}
	editorError, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %T (%v), want *Error", err, err)
	}
	return editorError
}
