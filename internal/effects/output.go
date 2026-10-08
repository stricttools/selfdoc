package effects

import (
	"bytes"
	"io"
	"sync"
)

// Output reaches the user only through the dispatch's strictcli context: a
// command's answer through ctx.Out, a streamed child's stderr through ctx.Info,
// and a warning through ctx.Warn. strictcli's test helper captures nothing
// written to the process streams, and its runtime guard fails a --json run on
// a byte written to stdout outside it, so nothing in selfdoc writes to
// os.Stdout or os.Stderr. The cli package's framework-use test refuses such a
// write anywhere in the program.

// emitMu serializes every line emitted to a dispatch context: a child's
// stdout and stderr are copied by separate goroutines, and a serving command
// writes from its server's goroutines.
var emitMu sync.Mutex

// LineWriter turns a byte stream into the whole lines strictcli's line-based
// writers take. Write emits every completed line and keeps a trailing partial
// line until the next newline or Flush. It is safe for concurrent use.
type LineWriter struct {
	mu      sync.Mutex
	emit    func(string)
	pending []byte
}

// NewLineWriter builds a LineWriter emitting each line, without its newline,
// through emit.
func NewLineWriter(emit func(string)) *LineWriter {
	return &LineWriter{emit: emit}
}

// Write emits the lines p completes.
func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		line := string(w.pending[:i])
		w.pending = w.pending[i+1:]
		w.send(line)
	}
	return len(p), nil
}

// Flush emits the partial line Write is holding, if any.
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return
	}
	line := string(w.pending)
	w.pending = nil
	w.send(line)
}

func (w *LineWriter) send(line string) {
	emitMu.Lock()
	defer emitMu.Unlock()
	w.emit(line)
}

// streams returns the writers a streamed child's stdout (ctx.Out) and stderr
// (ctx.Info) go to, which the caller flushes once the child has exited. ok is
// false for a handle with no dispatch behind it, which has no one to stream
// to: its children's output is captured into the [Result] instead.
func (h *Handle) streams() (stdout, stderr *LineWriter, ok bool) {
	if h == nil || h.ctx == nil {
		return nil, nil, false
	}
	return NewLineWriter(h.ctx.Out), NewLineWriter(h.ctx.Info), true
}

// Out returns the writer an engine's progress lines go to: the dispatch's
// answer (ctx.Out), one line per newline, a write's trailing partial line
// emitted as a line of its own. A handle with no dispatch behind it discards
// them, since a library call has no command to report through.
func (h *Handle) Out() io.Writer {
	if h == nil || h.ctx == nil {
		return io.Discard
	}
	return wholeWrites{emit: h.ctx.Out}
}

// Info returns the writer an engine's informational lines go to (ctx.Info,
// hidden under --quiet), written as [Handle.Out] writes. A handle with no
// dispatch behind it discards them.
func (h *Handle) Info() io.Writer {
	if h == nil || h.ctx == nil {
		return io.Discard
	}
	return wholeWrites{emit: h.ctx.Info}
}

// Warnings returns the writer an engine's advisories go to (ctx.Warn),
// written as [Handle.Out] writes. A handle with no dispatch behind it
// discards them.
func (h *Handle) Warnings() io.Writer {
	if h == nil || h.ctx == nil {
		return io.Discard
	}
	return wholeWrites{emit: h.ctx.Warn}
}

// wholeWrites emits each Write as whole lines: every line it holds, a trailing
// partial line included, so nothing is held between writes. The engines write
// one or more whole lines per call.
type wholeWrites struct {
	emit func(string)
}

func (w wholeWrites) Write(p []byte) (int, error) {
	text := string(bytes.TrimSuffix(p, []byte("\n")))
	if len(p) == 0 {
		return 0, nil
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	for _, line := range bytes.Split([]byte(text), []byte("\n")) {
		w.emit(string(line))
	}
	return len(p), nil
}
