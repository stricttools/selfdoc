package cli

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/unified"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
)

// reloadScript is the snippet injected into every HTML response so an open
// page reloads itself when the build writes over it.
var reloadScript = []byte(
	"\n<script>" +
		"const es = new EventSource('/__reload');" +
		"es.onmessage = () => location.reload();" +
		"</script>\n",
)

// reloadPollInterval is how often the watcher compares the output tree's
// modification times against its previous snapshot.
const reloadPollInterval = 500 * time.Millisecond

func (c *cli) registerServe() {
	c.app.Command("serve", "Serve the documentation site locally with live reload",
		c.cmdServe,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.IntFlag("port", "HTTP port number to serve on (e.g., 3000). Omitted, the server binds port 8000", strictcli.Short("p"), strictcli.Optional()),
			strictcli.BoolFlag("drafts", "Rebuild the site with draft posts included before starting the local server. Omitted, drafts are left out; pass --drafts to include them", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdServe(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	port := absentMeans(kwargs, "port", 8000)
	drafts := absentMeans(kwargs, "drafts", false)
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	if drafts {
		// The cross-CLI refusal is gone: a unified project rebuilds through
		// the unified builder here rather than naming another package.
		var err error
		if cfg["unified"] != nil {
			_, err = unified.BuildUnified(c.dir(), cfg, "", true, handle)
		} else {
			_, err = build.Build(build.Options{
				DirPath:       c.dir(),
				Config:        cfg,
				IncludeDrafts: true,
				Stdout:        c.out(),
			}, handle)
		}
		if err != nil {
			return c.fail(err)
		}
	}

	outputDir := filepath.Join(c.dir(), strings.TrimRight(outputDirOf(cfg), "/"))
	if info, err := os.Stat(outputDir); err != nil || !info.IsDir() {
		return c.failf("Error: Output directory '%s' not found. Run 'selfdoc build' first.",
			strings.TrimRight(outputDirOf(cfg), "/"))
	}

	server := newReloadServer(outputDir)
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return c.fail(err)
	}

	c.printf("Serving docs at http://localhost:%d/\n", port)
	c.println("Live reload enabled")
	c.println("Press Ctrl+C to stop.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	go server.watch(outputDir)
	go func() {
		_ = http.Serve(listener, server)
	}()

	<-stop
	c.printf("\nStopped.\n")
	server.shutdown()
	_ = listener.Close()
	return strictcli.Exit(0)
}

// reloadServer serves a built site and holds the event streams the injected
// script listens on.
type reloadServer struct {
	files http.Handler

	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	done    chan struct{}
	once    sync.Once
}

func newReloadServer(root string) *reloadServer {
	return &reloadServer{
		files:   http.FileServer(http.Dir(root)),
		clients: map[chan struct{}]struct{}{},
		done:    make(chan struct{}),
	}
}

func (s *reloadServer) shutdown() {
	s.once.Do(func() { close(s.done) })
}

func (s *reloadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__reload" {
		s.serveEvents(w)
		return
	}
	// The injection keys on the request path, exactly as the Python's
	// copyfile override did: a directory address and an .html/.htm file are
	// the responses a page is served from.
	path := r.URL.Path
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm") {
		injector := &injectingWriter{rw: w, status: http.StatusOK}
		s.files.ServeHTTP(injector, r)
		injector.finish()
		return
	}
	s.files.ServeHTTP(w, r)
}

// serveEvents holds one connection open as an SSE stream until the server
// stops or the client goes away.
func (s *reloadServer) serveEvents(w http.ResponseWriter) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	notify := make(chan struct{}, 1)
	s.mu.Lock()
	s.clients[notify] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, notify)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-s.done:
			return
		case <-notify:
			if _, err := fmt.Fprint(w, "data: reload\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// watch polls the output tree's modification times and notifies every held
// stream when the snapshot changes.
func (s *reloadServer) watch(root string) {
	previous := snapshotMtimes(root)
	ticker := time.NewTicker(reloadPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			current := snapshotMtimes(root)
			if sameSnapshot(previous, current) {
				continue
			}
			previous = current
			s.mu.Lock()
			for notify := range s.clients {
				select {
				case notify <- struct{}{}:
				default:
				}
			}
			s.mu.Unlock()
		}
	}
}

func snapshotMtimes(root string) map[string]time.Time {
	mtimes := map[string]time.Time{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		mtimes[path] = info.ModTime()
		return nil
	})
	return mtimes
}

func sameSnapshot(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for path, mtime := range a {
		other, ok := b[path]
		if !ok || !other.Equal(mtime) {
			return false
		}
	}
	return true
}

// injectingWriter buffers a response so the live-reload script can be spliced
// in before </body>, and the content length corrected, before anything is
// written to the wire.
type injectingWriter struct {
	rw     http.ResponseWriter
	buf    bytes.Buffer
	status int
	sent   bool
}

func (w *injectingWriter) Header() http.Header { return w.rw.Header() }

func (w *injectingWriter) WriteHeader(status int) { w.status = status }

func (w *injectingWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *injectingWriter) finish() {
	if w.sent {
		return
	}
	w.sent = true
	data := w.buf.Bytes()
	// Only a served page is injected into. An error body or a redirect is
	// the server's own answer, not a page a reader has open.
	if w.status != http.StatusOK {
		w.rw.WriteHeader(w.status)
		_, _ = w.rw.Write(data)
		return
	}
	if position := bytes.LastIndex(bytes.ToLower(data), []byte("</body>")); position != -1 {
		spliced := make([]byte, 0, len(data)+len(reloadScript))
		spliced = append(spliced, data[:position]...)
		spliced = append(spliced, reloadScript...)
		spliced = append(spliced, data[position:]...)
		data = spliced
	} else {
		data = append(data, reloadScript...)
	}
	w.rw.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.rw.WriteHeader(w.status)
	_, _ = w.rw.Write(data)
}
