package editor

import (
	"bytes"
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
	"testing/fstest"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/editor/assets"
	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// The posts the fixtures publish. Each is a whole Markdown source as it would
// be saved under the posts directory.
const (
	postHelloName = "hello.md"
	postHello     = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\n" +
		"# Hello World\n\nThis is the post content.\n"
	postDraftName = "later.md"
	postDraft     = "+++\ntitle = \"Later\"\ndate = 2024-05-01\nslug = \"later\"\n" +
		"tags = []\ndraft = true\ndirectives = false\n+++\nNot yet.\n"
)

// makeProject writes a selfdoc project carrying the named posts and returns
// its root.
func makeProject(t *testing.T, posts map[string]string) string {
	t.Helper()
	return makeProjectWithConfig(t, posts, nil)
}

// makeProjectWithConfig writes a selfdoc project with extra config keys
// applied over the fixture defaults.
func makeProjectWithConfig(t *testing.T, posts map[string]string, overrides map[string]any) string {
	t.Helper()
	settings := map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"}
	for key, value := range overrides {
		settings[key] = value
	}
	dir := testproject.Make(t, settings)
	testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"),
		"# Test Project\n\nWelcome.\n")
	for name, content := range posts {
		testproject.WriteText(t, filepath.Join(dir, ".stricttools", "posts", name), content)
	}
	return dir
}

// repoEntry is one registry entry a fixture declares: a local working tree
// when Path is set, a remote repository otherwise.
type repoEntry struct {
	Name  string
	Path  string
	Repo  string
	Ref   string
	Cache string
}

// local names a local working tree.
func local(name, path string) repoEntry { return repoEntry{Name: name, Path: path} }

// remote names a repository elsewhere: validated by the registry, refused by
// every path that would have to serve it.
func remote(t *testing.T, name string) repoEntry {
	t.Helper()
	return repoEntry{
		Name: name, Repo: "smm-h/" + name, Ref: "main",
		Cache: filepath.Join(t.TempDir(), "cache"),
	}
}

// writeRegistry writes a registry file declaring the given entries and loads
// it.
func writeRegistry(t *testing.T, entries ...repoEntry) *registry.Registry {
	t.Helper()
	var document strings.Builder
	for _, entry := range entries {
		if entry.Path != "" {
			fmt.Fprintf(&document,
				"[[repo]]\nname = %q\nkind = \"local\"\npath = %q\n\n",
				entry.Name, entry.Path)
			continue
		}
		fmt.Fprintf(&document,
			"[[repo]]\nname = %q\nkind = \"remote\"\nrepo = %q\nref = %q\n"+
				"cache = %q\nrender = true\n\n",
			entry.Name, entry.Repo, entry.Ref, entry.Cache)
	}
	path := filepath.Join(t.TempDir(), "registry.toml")
	if err := os.WriteFile(path, []byte(document.String()), 0o644); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	loaded, err := registry.Load(path)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return loaded
}

// fakeTinymoon is an asset tree carrying every file the shell loads, each
// naming itself so a served file can be told from another.
func fakeTinymoon() fstest.MapFS {
	tree := fstest.MapFS{}
	for _, rel := range assets.TinymoonRequired {
		tree[rel] = &fstest.MapFile{Data: []byte("/* " + rel + " */\n")}
	}
	return tree
}

// newState builds the state one running editor holds over a registry.
func newState(t *testing.T, reg *registry.Registry, publisher Publisher) *State {
	t.Helper()
	if publisher == nil {
		publisher = &fakePublisher{}
	}
	state, err := NewState(StateOptions{
		Registry:  reg,
		Tinymoon:  fakeTinymoon(),
		Publisher: publisher,
		Handle:    effects.Unbound(),
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	return state
}

// serveEditor binds the editor on an ephemeral port, serves it for the test's
// duration, and returns the port.
func serveEditor(t *testing.T, state *State) int {
	t.Helper()
	server, err := NewServer(state, 0)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
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

// request performs one request against a served editor and returns its
// status and body.
func request(t *testing.T, port int, method, path, body string) (int, string) {
	t.Helper()
	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + path
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return response.StatusCode, string(content)
}

// requestJSON performs one request and decodes its body.
func requestJSON(t *testing.T, port int, method, path, body string) (int, map[string]any) {
	t.Helper()
	status, text := request(t, port, method, path, body)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s %s answered %d with a body that is not a JSON object: %v\n%s",
			method, path, status, err, text)
	}
	return status, decoded
}

// errorText returns the "error" member of a refusal body.
func errorText(t *testing.T, body map[string]any) string {
	t.Helper()
	message, ok := body["error"].(string)
	if !ok {
		t.Fatalf("the body carries no error member: %v", body)
	}
	return message
}

// entryOf loads a registry naming one local project and returns its entry.
func entryOf(t *testing.T, name, project string) registry.Entry {
	t.Helper()
	entry, err := writeRegistry(t, local(name, project)).Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	return entry
}

// treeFingerprint digests every file under root: its path, its size, its
// modification time and the digest of its bytes.
//
// The modification time is included on purpose: a rewrite with identical
// bytes is still a write, and this notices it.
func treeFingerprint(t *testing.T, root string) string {
	t.Helper()
	var digest bytes.Buffer
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		fmt.Fprintf(&digest, "%s|%d|%d|%x\n",
			relative, info.Size(), info.ModTime().UnixNano(), content)
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprinting %s: %v", root, err)
	}
	return digest.String()
}
