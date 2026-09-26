package cli

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// `selfdoc serve`, on the wire.
//
// The server is the whole point of the command, so the tests below run it as a
// real process on a real loopback port and assert what a browser would see:
// the live-reload script spliced into a served page, the event stream held
// open, and a clean stop on an interrupt.

// freePort returns a port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return port
}

// servedProject is a project whose output tree carries one page.
func servedProject(t *testing.T) string {
	t.Helper()
	dir := postProject(t, map[string]any{"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/"})
	writeText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html"),
		"<!DOCTYPE html>\n<html><head><title>Home</title></head>"+
			"<body><h1>Home</h1></body></html>\n")
	return dir
}

// startServer starts the command as a subprocess and waits for it to report the
// address it bound. It returns the port and a stop function.
func startServer(t *testing.T, dir string, argv ...string) (int, func() string) {
	t.Helper()
	port := freePort(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	command := exec.Command(binary,
		append([]string{"serve", "--port", strconv.Itoa(port)}, argv...)...)
	command.Dir = dir
	command.Env = append(os.Environ(), runAsCLIEnv+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the server's stdout: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("opening the server's stderr: %v", err)
	}
	command.Stderr = stderrFile
	if err := command.Start(); err != nil {
		t.Fatalf("starting the server: %v", err)
	}

	lines := make(chan string, 16)
	var collected strings.Builder
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				collected.WriteString(line)
				select {
				case lines <- line:
				default:
				}
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()

	deadline := time.After(30 * time.Second)
	ready := false
	for !ready {
		select {
		case line, open := <-lines:
			if !open {
				_ = command.Wait()
				report, _ := os.ReadFile(stderrFile.Name())
				t.Fatalf("the server stopped before it bound:\n%s\n%s",
					collected.String(), report)
			}
			if strings.Contains(line, "Serving docs at") {
				ready = true
			}
		case <-deadline:
			_ = command.Process.Kill()
			t.Fatal("the server did not report an address within 30s")
		}
	}

	stopped := false
	stop := func() string {
		if stopped {
			return collected.String()
		}
		stopped = true
		_ = command.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = command.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
		return collected.String()
	}
	t.Cleanup(func() { stop() })
	return port, stop
}

// fetch performs one GET against the running server.
func fetch(t *testing.T, port int, path string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return response.StatusCode, string(body)
}

func TestServeRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "serve")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

func TestServeRefusesWithoutAnOutputTree(t *testing.T) {
	isolate(t)
	dir := postProject(t, map[string]any{"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/"})
	result := run(t, dir, "serve")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "Output directory 'stricttools/.docs-cache/build' not found") {
		t.Errorf("the refusal is not the missing output's: %s", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "Run 'selfdoc build' first.") {
		t.Errorf("the refusal does not name the remedy: %s", result.Stderr)
	}
}

func TestServeInjectsTheLiveReloadScript(t *testing.T) {
	isolate(t)
	dir := servedProject(t)
	port, stop := startServer(t, dir)

	status, body := fetch(t, port, "/")
	if status != http.StatusOK {
		t.Fatalf("the root answered %d", status)
	}
	if !strings.Contains(body, "<h1>Home</h1>") {
		t.Errorf("the page is not the one on disk:\n%s", body)
	}
	if !strings.Contains(body, "new EventSource('/__reload')") {
		t.Errorf("the live-reload script was not injected:\n%s", body)
	}
	// Spliced in before the closing tag rather than appended after it.
	scriptAt := strings.Index(body, "<script>")
	bodyEndAt := strings.LastIndex(body, "</body>")
	if scriptAt < 0 || bodyEndAt < 0 || scriptAt > bodyEndAt {
		t.Errorf("the script is not spliced before </body>:\n%s", body)
	}

	report := stop()
	for _, want := range []string{"Live reload enabled", "Press Ctrl+C to stop.", "Stopped."} {
		if !strings.Contains(report, want) {
			t.Errorf("the server did not print %q:\n%s", want, report)
		}
	}
}

func TestServeHoldsTheEventStreamOpen(t *testing.T) {
	isolate(t)
	dir := servedProject(t)
	port, _ := startServer(t, dir)

	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequest(http.MethodGet,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/__reload", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("opening the event stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the event stream answered %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("the stream's content type is %q", got)
	}

	// Touching the output tree makes the watcher push one event.
	time.Sleep(200 * time.Millisecond)
	writeText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html"),
		"<!DOCTYPE html>\n<html><head><title>Home</title></head>"+
			"<body><h1>Rebuilt</h1></body></html>\n")

	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the event: %v", err)
	}
	if strings.TrimSpace(line) != "data: reload" {
		t.Errorf("the event is %q", line)
	}
}

func TestServeWithDraftsRebuildsFirst(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := postProject(t, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	writeText(t, filepath.Join(dir, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \""+longDescription+"\"\n+++\n\n# Home\n")
	writePost(t, filepath.Join(dir, "stricttools", "posts"), "draft.md",
		[]string{"title = \"Draft Post\"", "date = 2024-01-16", "slug = \"draft-post\"", "draft = true"},
		"Draft content here.\n")
	// The server refuses an absent output tree, so the rebuild has to be what
	// produces the draft page below.
	writeText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "placeholder.txt"), "placeholder\n")

	draftPage := filepath.Join(dir, "stricttools", ".docs-cache", "build", "blog", "draft-post", "index.html")
	if exists(draftPage) {
		t.Fatal("the fixture already carries the draft page")
	}

	port, _ := startServer(t, dir, "--drafts")
	if !exists(draftPage) {
		t.Fatal("--drafts did not rebuild the site with the draft in it")
	}
	if status, _ := fetch(t, port, "/blog/draft-post/"); status != http.StatusOK {
		t.Errorf("the draft page answered %d", status)
	}
}

func TestServeWithoutDraftsDoesNotRebuild(t *testing.T) {
	isolate(t)
	dir := servedProject(t)
	sentinel := filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html")
	before := readText(t, sentinel)

	port, _ := startServer(t, dir)
	if status, _ := fetch(t, port, "/"); status != http.StatusOK {
		t.Fatalf("the root answered %d", status)
	}
	if readText(t, sentinel) != before {
		t.Error("the server rebuilt the site without --drafts")
	}
}
