// Package watch implements the `optiqor watch` command: re-analyzes a
// Helm chart directory or values file whenever a YAML file changes.
//
// Hard rules (from CLAUDE.md):
//   - No telemetry. No network calls.
//   - Ctrl-C exits with code 0 (clean shutdown).
//   - Windows is explicitly out of scope per CLAUDE.md; fsnotify still
//     compiles on Windows but is not a test target.
package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/optiqor/optiqor-cli/internal/analyze"
	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/internal/render/style"
)

// debounceDelay is the quiet period after the last filesystem event
// before we re-run analysis. 200 ms absorbs editor "save storms"
// (vim writes a swap file then the real file; most editors do two or
// three writes per save).
const debounceDelay = 200 * time.Millisecond

// clearScreen is the ANSI escape to move the cursor to the home
// position and erase the display. Only emitted when stdout is a TTY.
const clearScreen = "\033[H\033[2J"

// Options controls a single watch session.
type Options struct {
	// JSON streams newline-delimited JSON events instead of clearing
	// and reprinting the full text report.
	JSON bool

	// Color controls ANSI output in text mode. Ignored in JSON mode.
	Color bool

	// Width is the terminal width for text rendering. 0 → 80.
	Width int

	// Out is where rendered output goes. nil → os.Stdout.
	Out io.Writer

	// IsTTY reports whether Out is a real terminal (used for screen
	// clear). Exported as a field so tests can stub it without
	// needing a real TTY file descriptor.
	IsTTY bool

	// ctx is an optional context for testing. When nil, Run creates
	// its own context from SIGINT/SIGTERM. Tests inject a cancellable
	// context so they don't need to send OS signals.
	// The field is unexported; callers use WithContext to set it.
	ctx context.Context //nolint:containedctx // intentional: test-only injection, not passed via function arg
}

// WithContext returns a copy of opts with the given context set.
// Used by tests to inject a cancellable context instead of relying
// on OS signals, which are not reliably sendable across goroutines
// in the test runner on all platforms.
// FIX: ctx is first parameter per Go convention (revive: context-as-argument).
func WithContext(ctx context.Context, opts Options) Options {
	opts.ctx = ctx
	return opts
}

// safeWriter wraps an io.Writer with a mutex so concurrent goroutines
// (the Run loop and the test reader) don't race on the same buffer.
type safeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// jsonEvent is the schema for --json streaming output.
type jsonEvent struct {
	Event     string         `json:"event"`     // "start" | "update" | "error"
	Timestamp string         `json:"timestamp"` // RFC3339
	Path      string         `json:"path"`
	Findings  []any          `json:"findings,omitempty"`
	Error     string         `json:"error,omitempty"`
	Report    *render.Report `json:"report,omitempty"`
}

// Run starts the watch loop for path and blocks until the process
// receives SIGINT or SIGTERM (or opts.ctx is cancelled in tests).
// Returns nil on clean shutdown; non-nil only on setup failure.
func Run(path string, opts Options) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("watch: resolve path: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("watch: stat %s: %w", abs, err)
	}

	watchDir := abs
	if !info.IsDir() {
		watchDir = filepath.Dir(abs)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: create watcher: %w", err)
	}
	defer func() { _ = w.Close() }()

	if err := w.Add(watchDir); err != nil {
		return fmt.Errorf("watch: add %s: %w", watchDir, err)
	}

	// Wrap the output writer in a mutex-protected wrapper so the
	// background Run goroutine and the test reader goroutine don't
	// race on the same bytes.Buffer.
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	safe := &safeWriter{w: out}

	// Use injected context (tests) or create one from OS signals (production).
	ctx := opts.ctx
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
	}

	// Run once immediately on start.
	runOnce(abs, opts, safe)

	// Debounce timer: stop immediately so it doesn't fire on its own.
	timer := time.NewTimer(0)
	timer.Stop()
	select {
	case <-timer.C:
	default:
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !isRelevant(event) {
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceDelay)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			emitError(safe, opts, abs, err)

		case <-timer.C:
			runOnce(abs, opts, safe)
		}
	}
}

// isRelevant returns true for filesystem events we care about: writes,
// creates, and removes of YAML files.
func isRelevant(e fsnotify.Event) bool {
	if e.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return false
	}
	name := strings.ToLower(e.Name)
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// runOnce runs analysis on path and prints the result to out.
// eventKind is "start" or "update" (JSON mode only).
func runOnce(path string, opts Options, out io.Writer) {
	rep, err := analyze.RunPath(path)
	if err != nil {
		emitError(out, opts, path, err)
		return
	}

	if opts.JSON {
		emitJSON(out, "start", path, rep)
		return
	}

	if opts.IsTTY {
		_, _ = fmt.Fprint(out, clearScreen)
	}

	rOpts := render.Options{
		Color: opts.Color,
		Width: opts.Width,
	}
	_ = render.Text(out, rep, rOpts)
}

// emitJSON writes a single newline-delimited JSON event to out.
func emitJSON(out io.Writer, eventKind, path string, rep render.Report) {
	findings := make([]any, len(rep.Findings))
	for i, f := range rep.Findings {
		findings[i] = f
	}
	evt := jsonEvent{
		Event:     eventKind,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Path:      path,
		Findings:  findings,
		Report:    &rep,
	}
	b, err := json.Marshal(evt)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(out, "%s\n", b)
}

// emitError writes an error in JSON or plain text depending on opts.
func emitError(out io.Writer, opts Options, path string, err error) {
	if opts.JSON {
		evt := jsonEvent{
			Event:     "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Path:      path,
			Error:     err.Error(),
		}
		b, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(out, "%s\n", b)
		return
	}
	t := style.NewTheme(opts.Color)
	_, _ = fmt.Fprintln(out, t.SevHigh.Render(" ERROR ")+" "+err.Error())
}

// safeBuffer is a bytes.Buffer protected by a mutex, used in tests
// so the Run goroutine and the test goroutine don't race.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}
