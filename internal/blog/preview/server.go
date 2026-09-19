package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/stricttools/selfdoc/internal/blog/serving"
)

// BareNotFound is the page served for an address the tree does not carry, when
// the tree does not carry a 404 page either. The assembly always writes one, so
// this is the shape of a preview of a tree that was never generated.
const BareNotFound = "<!DOCTYPE html>\n<html lang=\"en\"><head><title>404</title></head>" +
	"<body><h1>404</h1><p>This preview tree carries no 404.html.</p>" +
	"</body></html>\n"

// ServerVersion is the identity the preview server answers under.
const ServerVersion = "selfdoc-preview"

// PreviewHandler answers one request against the preview tree.
//
// What it imitates about a static host is exactly what changes whether a page
// looks right: a directory address serves that directory's "index.html", an
// address missing its trailing slash is redirected to the one that has it,
// everything is served under its real content type, and an address the tree
// does not carry answers with the tree's own "404.html" AND a 404 status. A 404
// page served as 200 is how a broken link survives a preview.
type PreviewHandler struct {
	// Root is the served tree, absolute.
	Root string
	// Log is where the one line per request goes. nil means os.Stderr.
	Log io.Writer
}

// NewPreviewHandler builds a handler serving root.
func NewPreviewHandler(root string) *PreviewHandler {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = filepath.Clean(root)
	}
	return &PreviewHandler{Root: absolute}
}

// ServeHTTP dispatches one request. GET and HEAD share their whole path: the
// head answers the headers a get would answer and stops before the body, so
// the two can never disagree about what is there.
func (h *PreviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.respond(w, r, true)
	case http.MethodHead:
		h.respond(w, r, false)
	default:
		h.send(w, r, http.StatusNotImplemented,
			[]byte(fmt.Sprintf("Unsupported method (%q)\n", r.Method)),
			"text/plain; charset=utf-8", true)
	}
}

// logRequest writes the one compact line a request earns, so a preview says
// what it served.
func (h *PreviewHandler) logRequest(r *http.Request, status int) {
	stream := h.Log
	if stream == nil {
		stream = os.Stderr
	}
	fmt.Fprintf(stream, "%s %s \"%s %s %s\" %d -\n",
		r.Method, r.RequestURI, r.Method, r.RequestURI, r.Proto, status)
}

// send writes one response, with the no-store every preview response carries:
// a preview is looked at while it is being rebuilt underneath.
func (h *PreviewHandler) send(
	w http.ResponseWriter, r *http.Request,
	status int, payload []byte, contentType string, body bool,
) {
	header := w.Header()
	header.Set("Server", ServerVersion)
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(payload)))
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	h.logRequest(r, status)
	if body {
		_, _ = w.Write(payload)
	}
}

// redirect answers the permanent redirect a directory address without its
// trailing slash earns.
func (h *PreviewHandler) redirect(w http.ResponseWriter, r *http.Request, location string) {
	header := w.Header()
	header.Set("Server", ServerVersion)
	header.Set("Location", location)
	header.Set("Content-Length", "0")
	w.WriteHeader(http.StatusMovedPermanently)
	h.logRequest(r, http.StatusMovedPermanently)
}

// notFound answers with the tree's own 404 page, and a 404 status.
func (h *PreviewHandler) notFound(w http.ResponseWriter, r *http.Request, body bool) {
	page := filepath.Join(h.Root, "404.html")
	payload, err := os.ReadFile(page)
	if err != nil {
		payload = []byte(BareNotFound)
	}
	h.send(w, r, http.StatusNotFound, payload, "text/html; charset=utf-8", body)
}

// respond resolves the request's address against the tree and answers it.
func (h *PreviewHandler) respond(w http.ResponseWriter, r *http.Request, body bool) {
	path := r.URL.Path
	target, ok := serving.ResolveUnder(h.Root, strings.TrimLeft(path, "/"))
	if !ok {
		h.notFound(w, r, body)
		return
	}
	info, err := os.Stat(target)
	if err == nil && info.IsDir() {
		if !strings.HasSuffix(path, "/") {
			h.redirect(w, r, path+"/")
			return
		}
		target = filepath.Join(target, "index.html")
		info, err = os.Stat(target)
	}
	if err != nil || !info.Mode().IsRegular() {
		h.notFound(w, r, body)
		return
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		h.notFound(w, r, body)
		return
	}
	h.send(w, r, http.StatusOK, payload, serving.ContentType(target), body)
}

// Server is one bound preview server: a listener on loopback and the handler
// over it.
//
// It binds at construction so the caller can read the port before anything is
// served, which is what lets the suite ask for an ephemeral one.
type Server struct {
	handler  *PreviewHandler
	server   *http.Server
	listener net.Listener
}

// MakePreviewServer binds a preview server for root to loopback on port.
//
// port 0 binds an ephemeral port, which is what the suite uses; the command
// itself requires an explicit one. The bind address is not configurable: the
// preview serves an unreleased site and authenticates nothing, so it is
// reachable from this machine only.
func MakePreviewServer(root string, port int) (*Server, error) {
	handler := NewPreviewHandler(root)
	listener, err := net.Listen("tcp", net.JoinHostPort(serving.Host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	return &Server{
		handler:  handler,
		server:   &http.Server{Handler: handler},
		listener: listener,
	}, nil
}

// Handler returns the handler this server answers through, so a caller can
// redirect its request log.
func (s *Server) Handler() *PreviewHandler { return s.handler }

// Port returns the port the server bound.
func (s *Server) Port() int {
	if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// Serve serves until [Server.Stop], and reports nothing when that is why it
// returned.
func (s *Server) Serve() error {
	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop ends the accept loop and closes the listening socket.
func (s *Server) Stop() error {
	return s.server.Shutdown(context.Background())
}

// ServePreview serves root until interrupted, then closes the listening
// socket, and reports the exit code the command exits with.
//
// onReady, when given, is called with the bound port once the socket is
// listening and before anything is served.
func ServePreview(root string, port int, onReady func(port int)) (int, error) {
	server, err := MakePreviewServer(root, port)
	if err != nil {
		return 1, err
	}
	if onReady != nil {
		onReady(server.Port())
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	served := make(chan error, 1)
	go func() { served <- server.Serve() }()

	select {
	case <-ctx.Done():
		if stopErr := server.Stop(); stopErr != nil {
			return 1, stopErr
		}
		<-served
		return 0, nil
	case serveErr := <-served:
		_ = server.Stop()
		if serveErr != nil {
			return 1, serveErr
		}
		return 0, nil
	}
}
