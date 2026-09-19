package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/blog/serving"
	"github.com/stricttools/selfdoc/internal/util"
)

// Server is one bound editor server: a listener on loopback, the routes over
// it, and the stop that releases every held event stream.
//
// It binds at construction so the caller can read the port before anything is
// served, which is what lets the suite ask for an ephemeral one.
type Server struct {
	state    *State
	server   *http.Server
	listener net.Listener
}

// NewServer binds the editor server to loopback on port.
//
// port 0 binds an ephemeral port, which is what the suite uses; the command
// itself requires an explicit one. The bind address is not configurable: the
// editor writes working trees and authenticates nothing, so it is reachable
// from this machine only.
func NewServer(state *State, port int) (*Server, error) {
	if state == nil {
		return nil, errors.New("the editor server needs a state")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(serving.Host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	server := &Server{state: state, listener: listener}
	server.server = &http.Server{Handler: http.HandlerFunc(server.route)}
	return server, nil
}

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

// Stop ends the accept loop, releases every held event stream and closes the
// listening socket.
//
// The streams are released FIRST: each one is a request still in flight, and
// a shutdown that waited for them before telling them to go would wait for as
// long as a browser tab stays open.
func (s *Server) Stop() error {
	s.state.stop()
	return s.server.Shutdown(context.Background())
}

// Serve runs the editor until interrupted, then stops cleanly, and reports
// the exit code the command exits with.
//
// Ctrl-C is the graceful stop: the accept loop ends, every held event stream
// is released, and the listening socket is closed. A termination signal is
// treated the same way, because the alternative is a process killed with a
// browser still holding a stream.
//
// onReady, when given, is called with the bound port once the socket is
// listening and before anything is served.
func Serve(state *State, port int, onReady func(port int)) (int, error) {
	server, err := NewServer(state, port)
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

// -- plumbing ---------------------------------------------------------------

// errorPayload is the body every refusal answers with.
type errorPayload struct {
	Error string `json:"error"`
}

// send writes one response, with the no-store every editor response carries:
// nothing the editor serves may be held by a browser cache, because every
// answer is about a working tree that is being edited.
//
// A write that fails is the client having gone away, which is not a condition
// the server reports to it.
func (s *Server) send(w http.ResponseWriter, status int, body []byte, contentType string) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return nil
}

// sendJSON writes one JSON response, encoded the way the Python encoded it.
func (s *Server) sendJSON(w http.ResponseWriter, status int, payload any) error {
	body, err := encodeJSON(payload)
	if err != nil {
		return err
	}
	return s.send(w, status, body, "application/json; charset=utf-8")
}

// sendError writes the body a refusal answers with: the status the refusal
// carries, or the internal-failure shape for anything that is not one.
func (s *Server) sendError(w http.ResponseWriter, err error) {
	var editorError *Error
	if errors.As(err, &editorError) {
		status := editorError.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		_ = s.sendJSON(w, status, errorPayload{Error: editorError.Message})
		return
	}
	_ = s.sendJSON(w, http.StatusInternalServerError, errorPayload{
		Error: fmt.Sprintf("%s: %s", errorTypeName(err), err.Error()),
	})
}

// errorTypeName is the name of an error's own type, with its package
// qualifier dropped -- the Go reading of the Python's
// type(exc).__name__.
func errorTypeName(err error) string {
	name := reflect.TypeOf(err).String()
	name = strings.TrimPrefix(name, "*")
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}

// readBody reads a request body as text.
func readBody(r *http.Request) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	content, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// entry returns the registry entry called name, or a 404 naming what is on
// offer.
func (s *Server) entry(name string) (registry.Entry, error) {
	found, err := s.state.registry.Get(name)
	if err != nil {
		return nil, notFound("%s", err.Error())
	}
	return found, nil
}

// resolveInTree cleans rel into a path inside an embedded tree, reporting
// false when it escapes.
//
// A ".." segment is refused rather than normalized away: normalizing one
// would serve a different file than the request named, and the request that
// names one is the request this boundary exists for.
func resolveInTree(rel string) (string, bool) {
	for _, segment := range strings.Split(rel, "/") {
		if segment == ".." {
			return "", false
		}
	}
	cleaned := path.Clean("/" + rel)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		cleaned = "."
	}
	if !fs.ValidPath(cleaned) {
		return "", false
	}
	return cleaned, true
}

// serveTreeFile serves one file out of an embedded asset tree.
func (s *Server) serveTreeFile(w http.ResponseWriter, tree fs.FS, rel string) error {
	resolved, ok := resolveInTree(rel)
	if !ok {
		return notFound("%s is outside the served directory", rel)
	}
	content, err := fs.ReadFile(tree, resolved)
	if err != nil {
		return notFound("no such file: %s", rel)
	}
	return s.send(w, http.StatusOK, content, serving.ContentType(resolved))
}

// serveDiskFile serves one file out of a directory on disk.
func (s *Server) serveDiskFile(w http.ResponseWriter, root, rel string) error {
	full, ok := serving.ResolveUnder(root, rel)
	if !ok {
		return notFound("%s is outside the served directory", rel)
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return notFound("no such file: %s", rel)
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	return s.send(w, http.StatusOK, content, serving.ContentType(full))
}

// -- dispatch ---------------------------------------------------------------

// route dispatches one request and renders whatever refusal it produced.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = s.sendJSON(w, http.StatusInternalServerError, errorPayload{
				Error: fmt.Sprintf("%s: %v", util.PythonTypeName(recovered), recovered),
			})
		}
	}()

	requestPath := r.URL.Path
	query := r.URL.Query()

	var err error
	switch r.Method {
	case http.MethodGet:
		err = s.get(w, r, requestPath, query)
	case http.MethodPut:
		body, readErr := readBody(r)
		if readErr != nil {
			err = readErr
			break
		}
		err = s.put(w, requestPath, query, body)
	case http.MethodPost:
		body, readErr := readBody(r)
		if readErr != nil {
			err = readErr
			break
		}
		err = s.post(w, requestPath, query, body)
	default:
		// The Python's stdlib handler answered an HTML error page for a
		// method it declared no handler for; this answers the editor's own
		// JSON refusal under the same status.
		err = &Error{
			Message: fmt.Sprintf("Unsupported method (%s)", util.PythonRepr(r.Method)),
			Status:  http.StatusNotImplemented,
		}
	}
	if err != nil {
		s.sendError(w, err)
	}
}

// partition splits a path tail on its first slash, the way Python's
// str.partition does.
func partition(tail string) (string, string) {
	if index := strings.Index(tail, "/"); index >= 0 {
		return tail[:index], tail[index+1:]
	}
	return tail, ""
}

// one returns the single value of a query parameter, or the empty string.
func one(query map[string][]string, key string) string {
	values := query[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// -- routes -----------------------------------------------------------------

// reposPayload is the registry as the shell sees it.
type reposPayload struct {
	Repos []any `json:"repos"`
}

// localRepoPayload is one local entry: a working tree the editor serves.
type localRepoPayload struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Served bool   `json:"served"`
}

// remoteRepoPayload is one remote entry: validated, not served.
type remoteRepoPayload struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Ref    string `json:"ref"`
	Render bool   `json:"render"`
	Served bool   `json:"served"`
}

// postsPayload is one repository's post list.
type postsPayload struct {
	Repo  string        `json:"repo"`
	Posts []PostSummary `json:"posts"`
}

// documentPayload is one post's saved source.
type documentPayload struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// savedPayload is what a write reports back.
type savedPayload struct {
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Saved bool   `json:"saved"`
	Bytes int    `json:"bytes"`
	File  string `json:"file"`
}

// previewPayload is one rendered preview, both the answer to the request and
// the frame every listener on the event stream receives.
type previewPayload struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
	HTML string `json:"html"`
}

// analysisPayload is both analysis lanes for one buffer.
type analysisPayload struct {
	Repo     string            `json:"repo"`
	Path     string            `json:"path"`
	Spelling []SpellingFinding `json:"spelling"`
	Lints    []LintFinding     `json:"lints"`
}

// targetsPayload is the link targets a query answered with.
type targetsPayload struct {
	Targets []Target `json:"targets"`
}

// publishSurfacePayload is what the consent dialog is rendered from:
// declaration plus plan.
type publishSurfacePayload struct {
	Descriptor Descriptor `json:"descriptor"`
	Plan       Plan       `json:"plan"`
}

// get answers one GET.
func (s *Server) get(w http.ResponseWriter, r *http.Request, requestPath string, query map[string][]string) error {
	if requestPath == "/" || requestPath == "/index.html" {
		return s.serveTreeFile(w, s.state.ui, "index.html")
	}
	if strings.HasPrefix(requestPath, "/ui/") {
		return s.serveTreeFile(w, s.state.ui, requestPath[len("/ui/"):])
	}
	if strings.HasPrefix(requestPath, "/tinymoon/") {
		if s.state.tinymoon == nil {
			return notFound("no tinymoon assets are configured")
		}
		return s.serveTreeFile(w, s.state.tinymoon, requestPath[len("/tinymoon/"):])
	}
	if requestPath == "/events" {
		return s.streamEvents(w, r)
	}
	if requestPath == "/api/repos" {
		return s.sendJSON(w, http.StatusOK, reposPayload{
			Repos: reposOf(s.state.registry),
		})
	}
	if requestPath == "/api/link-targets" {
		return s.linkTargets(w, query)
	}
	if strings.HasPrefix(requestPath, "/api/repos/") {
		name, tail := partition(requestPath[len("/api/repos/"):])
		entry, err := s.entry(name)
		if err != nil {
			return err
		}
		switch tail {
		case "publish":
			return s.publishSurface(w, entry)
		case "posts":
			summaries, err := RepoPosts(entry, s.state.handle)
			if err != nil {
				return err
			}
			return s.sendJSON(w, http.StatusOK, postsPayload{Repo: name, Posts: summaries})
		case "document":
			rel := one(query, "path")
			if _, err := RequireLocal(entry); err != nil {
				return err
			}
			content, err := ReadPost(entry, rel)
			if err != nil {
				return err
			}
			return s.sendJSON(w, http.StatusOK, documentPayload{
				Repo: name, Path: rel, Content: content,
			})
		}
	}
	if strings.HasPrefix(requestPath, "/preview/") {
		return s.servePreview(w, requestPath[len("/preview/"):])
	}
	return notFound("no route for GET %s", requestPath)
}

// put answers one PUT.
func (s *Server) put(w http.ResponseWriter, requestPath string, query map[string][]string, body string) error {
	if strings.HasPrefix(requestPath, "/api/repos/") {
		name, tail := partition(requestPath[len("/api/repos/"):])
		entry, err := s.entry(name)
		if err != nil {
			return err
		}
		if tail == "document" {
			rel := one(query, "path")
			if _, err := RequireLocal(entry); err != nil {
				return err
			}
			full, err := SavePost(entry, rel, body, s.state.handle)
			if err != nil {
				return err
			}
			return s.sendJSON(w, http.StatusOK, savedPayload{
				Repo: name, Path: rel, Saved: true,
				Bytes: len(body), File: full,
			})
		}
	}
	return notFound("no route for PUT %s", requestPath)
}

// post answers one POST.
func (s *Server) post(w http.ResponseWriter, requestPath string, query map[string][]string, body string) error {
	if strings.HasPrefix(requestPath, "/api/repos/") {
		name, tail := partition(requestPath[len("/api/repos/"):])
		entry, err := s.entry(name)
		if err != nil {
			return err
		}
		switch tail {
		case "preview":
			return s.preview(w, name, entry, one(query, "path"), body)
		case "analysis":
			return s.analysis(w, name, entry, one(query, "path"), body)
		case "publish":
			return s.publish(w, entry, body)
		}
	}
	return notFound("no route for POST %s", requestPath)
}

// reposOf renders the registry the way the shell reads it: a local entry
// names its working tree and is served, a remote entry names its repository
// and is not.
func reposOf(reg *registry.Registry) []any {
	payload := make([]any, 0, reg.Len())
	for _, entry := range reg.Entries {
		switch typed := entry.(type) {
		case *registry.LocalRepo:
			payload = append(payload, localRepoPayload{
				Name: typed.Name(), Kind: "local", Path: typed.Path(), Served: true,
			})
		case *registry.RemoteRepo:
			payload = append(payload, remoteRepoPayload{
				Name: typed.Name(), Kind: "remote", Repo: typed.Repo(),
				Ref: typed.Ref(), Render: typed.Render(), Served: false,
			})
		}
	}
	return payload
}

// -- analysis ---------------------------------------------------------------

// analysis answers spelling and lint findings for an unsaved buffer.
func (s *Server) analysis(w http.ResponseWriter, name string, entry registry.Entry, rel, content string) error {
	if _, err := RequireLocal(entry); err != nil {
		return err
	}
	safe, err := SafeRel(rel)
	if err != nil {
		return err
	}
	findings, err := AnalyzeBuffer(entry, safe, content, nil, s.state.handle)
	if err != nil {
		return err
	}
	return s.sendJSON(w, http.StatusOK, analysisPayload{
		Repo: name, Path: safe,
		Spelling: findings.Spelling, Lints: findings.Lints,
	})
}

// -- link targets -----------------------------------------------------------

// linkTargets answers page and section targets across every registered
// repository.
func (s *Server) linkTargets(w http.ResponseWriter, query map[string][]string) error {
	limit := one(query, "limit")
	count := 40
	if limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil {
			return badRequest("limit must be a whole number, got %s", util.PythonRepr(limit))
		}
		count = parsed
	}
	if count < 1 {
		return badRequest("limit must be at least 1, got %d", count)
	}
	targets, err := s.state.targets.Search(one(query, "q"), count)
	if err != nil {
		return err
	}
	return s.sendJSON(w, http.StatusOK, targetsPayload{Targets: targets})
}

// -- publish ----------------------------------------------------------------

// publishSurface answers what the consent dialog is rendered from.
func (s *Server) publishSurface(w http.ResponseWriter, entry registry.Entry) error {
	if _, err := RequireLocal(entry); err != nil {
		return err
	}
	plan, err := PublishPlan(entry, s.state.publisher)
	if err != nil {
		return err
	}
	return s.sendJSON(w, http.StatusOK, publishSurfacePayload{
		Descriptor: PublishDescriptor(s.state.publisher), Plan: plan,
	})
}

// publish runs the publish, carrying whatever consent the request states.
//
// The consent is passed through untouched. Nothing here decides whether the
// call is allowed: a request that states no consent reaches the consent
// regime's refusal, which is the only place that decision is made.
func (s *Server) publish(w http.ResponseWriter, entry registry.Entry, body string) error {
	if _, err := RequireLocal(entry); err != nil {
		return err
	}

	payload := map[string]any{}
	if strings.TrimSpace(body) != "" {
		var decoded any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			return badRequest("publish body is not JSON: %s", jsonErrorText(err))
		}
		asObject, isObject := decoded.(map[string]any)
		if !isObject {
			return badRequest(
				"publish body must be a JSON object, got %s",
				util.PythonTypeName(decoded),
			)
		}
		payload = asObject
	}

	consent := false
	if declared, present := payload[ConsentParameter]; present {
		asBool, isBool := declared.(bool)
		if !isBool {
			return badRequest(
				"approve_consequential must be true or false, got %s",
				util.PythonRepr(declared),
			)
		}
		consent = asBool
	}

	result, err := RunPublish(entry, s.state.publisher, consent)
	if err != nil {
		return err
	}
	return s.sendJSON(w, http.StatusOK, result)
}

// jsonErrorText renders a JSON decoding failure the way the Python's
// JSONDecodeError read: the reason and where it was.
func jsonErrorText(err error) string {
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return fmt.Sprintf("%s: char %d", syntaxError.Error(), syntaxError.Offset)
	}
	return err.Error()
}

// -- preview ----------------------------------------------------------------

// preview renders a buffer, holds the result under the address it publishes
// at, and pushes it down the event stream.
func (s *Server) preview(w http.ResponseWriter, name string, entry registry.Entry, rel, content string) error {
	if _, err := RequireLocal(entry); err != nil {
		return err
	}
	safe, err := SafeRel(rel)
	if err != nil {
		return err
	}
	slug, err := PostSlug(safe, content)
	if err != nil {
		return err
	}
	html, err := RenderPreview(entry, safe, content, s.state.handle)
	if err != nil {
		return err
	}

	address := PreviewAddress(slug)
	s.state.StorePreview(name, address, html)

	payload := previewPayload{
		Repo: name, Path: safe, Slug: slug,
		URL:  fmt.Sprintf("/preview/%s/%s", name, address),
		HTML: html,
	}
	if err := s.state.channel.Broadcast("preview", payload); err != nil {
		return err
	}
	return s.sendJSON(w, http.StatusOK, payload)
}

// servePreview serves a previewed page, and the built assets its links reach
// for.
//
// The rendered document is the publish bytes, so its stylesheet, feed and
// sibling links are the published relative ones. Serving the page at the
// address it publishes to, with the repository's build output underneath, is
// what makes those links resolve -- and keeps the document itself
// byte-for-byte what was rendered.
func (s *Server) servePreview(w http.ResponseWriter, rest string) error {
	name, tail := partition(rest)
	entry, err := s.entry(name)
	if err != nil {
		return err
	}
	path, err := RequireLocal(entry)
	if err != nil {
		return err
	}
	if tail == "" || strings.HasSuffix(tail, "/") {
		tail += "index.html"
	}

	if stored, found := s.state.GetPreview(name, tail); found {
		return s.send(w, http.StatusOK, []byte(stored), "text/html; charset=utf-8")
	}

	cfg, err := RepoConfig(entry)
	if err != nil {
		return err
	}
	outputDir := util.PathJoin(path, strings.TrimRight(util.PythonStrOrEmpty(cfg["output"]), "/"))
	info, statErr := os.Stat(outputDir)
	if statErr != nil || !info.IsDir() {
		return notFound(
			"%s: no build output at %s. Run a posts build there for the "+
				"preview to load its stylesheet and assets.",
			name, outputDir,
		)
	}
	return s.serveDiskFile(w, outputDir, tail)
}

// -- events -----------------------------------------------------------------

// streamEvents holds the connection open as the one server-sent-events
// channel.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) error {
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = controller.Flush()

	client := &sseClient{writer: w, flush: controller.Flush}
	s.state.channel.add(client)
	defer s.state.channel.remove(client)

	if client.send([]byte(": connected\n\n")) != nil {
		return nil
	}

	heartbeat := time.NewTicker(s.state.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-s.state.stopped():
			return nil
		case <-r.Context().Done():
			return nil
		case <-heartbeat.C:
			if client.send([]byte(": ping\n\n")) != nil {
				return nil
			}
		}
	}
}
