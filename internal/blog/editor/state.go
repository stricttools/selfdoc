package editor

import (
	"errors"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/editor/assets"
	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/effects"
)

// HeartbeatInterval is how often an idle event stream emits a comment, so a
// client that went away is noticed rather than held forever.
const HeartbeatInterval = 15 * time.Second

// sseClient is one connected shell's end of the event stream.
//
// Its writer is shared between two goroutines -- the request's own, which
// emits the heartbeat, and whichever one broadcasts a preview -- so every
// write goes through the client's own mutex. A write that fails marks the
// client dead and it is dropped from the channel.
type sseClient struct {
	mu     sync.Mutex
	writer io.Writer
	flush  func() error
	dead   bool
}

// send writes one frame and flushes it, reporting whether the client is still
// there.
func (c *sseClient) send(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead {
		return errors.New("the event stream is closed")
	}
	if _, err := c.writer.Write(frame); err != nil {
		c.dead = true
		return err
	}
	if c.flush != nil {
		if err := c.flush(); err != nil {
			c.dead = true
			return err
		}
	}
	return nil
}

// SSEChannel is the one event stream every connected shell listens on.
type SSEChannel struct {
	mu      sync.Mutex
	clients []*sseClient
}

// add registers one client.
func (ch *SSEChannel) add(client *sseClient) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.clients = append(ch.clients, client)
}

// remove drops one client.
func (ch *SSEChannel) remove(client *sseClient) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	for index, held := range ch.clients {
		if held == client {
			ch.clients = append(ch.clients[:index], ch.clients[index+1:]...)
			return
		}
	}
}

// Count returns how many clients hold the stream open.
func (ch *SSEChannel) Count() int {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return len(ch.clients)
}

// Broadcast pushes one event to every client, dropping the ones that went
// away.
func (ch *SSEChannel) Broadcast(event string, payload any) error {
	encoded, err := encodeJSON(payload)
	if err != nil {
		return err
	}
	frame := append([]byte("event: "+event+"\ndata: "), encoded...)
	frame = append(frame, "\n\n"...)

	ch.mu.Lock()
	clients := append([]*sseClient(nil), ch.clients...)
	ch.mu.Unlock()

	for _, client := range clients {
		if client.send(frame) != nil {
			ch.remove(client)
		}
	}
	return nil
}

// StateOptions is what one editor state is built from.
//
// Every member is declared rather than defaulted, with two exceptions the
// zero value really answers: a nil UI means the front-end this build embeds,
// and a nil Tinymoon means no tinymoon tree is configured -- which every
// /tinymoon/ request then refuses by name.
type StateOptions struct {
	// Registry is the list of repositories the editor may open.
	Registry *registry.Registry
	// Tinymoon is the framework asset tree, addressed the way the shell
	// addresses it ("css/tokens.css", "js/editor.js"). Nil serves none.
	Tinymoon fs.FS
	// UI is the editor's own front-end ("index.html", "app.js", "app.css").
	// Nil uses the tree this build embeds.
	UI fs.FS
	// Publisher is the publish surface's door into the CLI. It is required:
	// the shell renders a publish button unconditionally, so an editor
	// without one is an editor with a button that cannot work.
	Publisher Publisher
	// Handle is the effects handle every write and every subprocess the
	// editor performs goes through.
	Handle *effects.Handle
}

// State is everything one running editor holds: registry, assets, live
// previews, the event stream and the publish surface.
//
// It is safe to use from several goroutines: the preview map and the event
// stream each carry their own mutex, and everything else is immutable after
// construction.
type State struct {
	registry  *registry.Registry
	tinymoon  fs.FS
	ui        fs.FS
	publisher Publisher
	handle    *effects.Handle

	channel *SSEChannel
	targets *TargetIndex

	// heartbeat is the idle comment interval. It is a field rather than the
	// constant so a test can watch a ping arrive without waiting for one.
	heartbeat time.Duration

	// stopping is closed by Server.Stop, which is what releases every held
	// event stream.
	stopping chan struct{}
	stopOnce sync.Once

	// previews maps a repository name and a published address to the
	// rendered HTML. The preview pane loads the document from here by URL
	// rather than through srcdoc, so the page's own relative links -- its
	// stylesheet, its feed, its siblings -- resolve against the
	// repository's built output instead of against the editor.
	previewMu sync.Mutex
	previews  map[previewKey]string
}

// previewKey is one stored preview's identity: which repository it belongs to
// and the address it publishes at.
type previewKey struct {
	repo    string
	address string
}

// NewState builds the state one running editor holds, or refuses.
//
// A missing registry, publisher or effects handle is a refusal rather than a
// zero value: each of them is something the caller decided, and an editor
// that discovered one at request time would answer a broken route on a
// surface the shell already drew.
func NewState(opts StateOptions) (*State, error) {
	if opts.Registry == nil {
		return nil, errors.New("the editor needs a registry: no repositories were declared")
	}
	if opts.Publisher == nil {
		return nil, errors.New(
			"the editor needs a publisher: the shell renders a publish " +
				"button, so a server that cannot publish is not one")
	}
	if opts.Handle == nil {
		return nil, errors.New(
			"the editor needs an effects handle: every write it performs " +
				"goes through one")
	}
	ui := opts.UI
	if ui == nil {
		ui = assets.UI()
	}
	return &State{
		registry:  opts.Registry,
		tinymoon:  opts.Tinymoon,
		ui:        ui,
		publisher: opts.Publisher,
		handle:    opts.Handle,
		channel:   &SSEChannel{},
		targets:   NewTargetIndex(opts.Registry),
		heartbeat: HeartbeatInterval,
		stopping:  make(chan struct{}),
		previews:  map[previewKey]string{},
	}, nil
}

// Registry returns the repositories this editor may open.
func (s *State) Registry() *registry.Registry { return s.registry }

// Channel returns the one event stream every connected shell listens on.
func (s *State) Channel() *SSEChannel { return s.channel }

// Targets returns the cross-repository link target index.
func (s *State) Targets() *TargetIndex { return s.targets }

// Handle returns the effects handle the editor's writes go through.
func (s *State) Handle() *effects.Handle { return s.handle }

// Publisher returns the publish surface's door into the CLI.
func (s *State) Publisher() Publisher { return s.publisher }

// StorePreview holds one rendered preview under the address it publishes at.
func (s *State) StorePreview(repoName, address, html string) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	s.previews[previewKey{repo: repoName, address: address}] = html
}

// GetPreview returns a held preview, reporting whether one is held.
func (s *State) GetPreview(repoName, address string) (string, bool) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	html, found := s.previews[previewKey{repo: repoName, address: address}]
	return html, found
}

// stop releases every held event stream. It is idempotent, so a second stop
// is not a panic on a closed channel.
func (s *State) stop() {
	s.stopOnce.Do(func() { close(s.stopping) })
}

// stopped returns the channel closed when the editor is stopping.
func (s *State) stopped() <-chan struct{} { return s.stopping }
