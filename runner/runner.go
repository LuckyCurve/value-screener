// Package runner invokes the Python data layer as a child process and reads
// its progress lines.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Unreachable is the data layer's exit code when OpenD was not answering.
// It is the one failure the window explains itself rather than merely
// reporting: nothing was asked, so nothing is wrong with the rules.
const Unreachable = 3

// ErrUnreachable is returned when the data layer could not reach OpenD.
var ErrUnreachable = errors.New("OpenD is not running")

// Event is one progress line the data layer writes to stderr.
//
// Stocks and Stock are the two streamed lines: the first pass, and one
// stock's report. Their shape belongs to the channel, so they stay raw here;
// the runner carries them and does not read them.
type Event struct {
	Event   string          `json:"event"`
	Name    string          `json:"name,omitempty"`
	Index   int             `json:"index,omitempty"`
	Total   int             `json:"total,omitempty"`
	Count   int             `json:"count,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Stocks  json.RawMessage `json:"stocks,omitempty"`
	Stock   json.RawMessage `json:"stock,omitempty"`
}

// Options configure one run.
type Options struct {
	// Python is the interpreter to use. Empty means the app looks for one.
	Python string
	// DataLayer, when set, is a self-contained data layer binary (a frozen
	// build) run directly instead of `python -m datalayer`. This is what
	// lets the app ship as a single folder with no Python installed.
	DataLayer string
	// Args are the data layer's own arguments, such as its subcommand and
	// the criteria file. They follow `-m datalayer` for the Python package
	// and lead for a frozen binary.
	Args []string
	// OutPath receives the dataset JSON.
	OutPath string
	// OnEvent, when set, is called for each progress line as it arrives.
	OnEvent func(Event)
}

// Run executes the data layer and returns its dataset JSON.
//
// On cancellation it returns whatever the data layer had written with
// ctx.Err(); a caller that wants the partial result uses both.
//
// The child is started in its own process group so a cancelled run leaves no
// orphaned Python holding an OpenD connection.
func Run(ctx context.Context, dir string, opts Options) ([]byte, error) {
	program := opts.DataLayer
	var args []string
	if program == "" {
		python, err := resolvePython(dir, opts.Python)
		if err != nil {
			return nil, err
		}
		program = python
		args = append(args, "-m", "datalayer")
	}
	args = append(args, opts.Args...)

	// The dataset of the previous run must not be mistaken for this one's,
	// which is read back even after a cancel.
	if opts.OutPath != "" {
		_ = os.Remove(opts.OutPath)
	}

	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	prepare(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start data layer: %w", err)
	}

	// Cancelling must stop the work, not merely stop waiting for it: the
	// child's own children go with it.
	stop, err := watch(ctx, cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("watch data layer: %w", err)
	}
	defer stop()

	// Keep the last of the child's own words: the SDK writes its reason for
	// failing there, and without it a failed run says only "exit status 2".
	var tail tailBuffer
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanEvents(stderr, &tail, opts.OnEvent)
	}()
	// Drain the pipe before Wait: Wait closes it as soon as the child exits,
	// and anything still buffered would be lost.
	<-scanned
	waitErr := cmd.Wait()

	// A cancelled run was killed mid-way, but the data layer writes each
	// stock as it goes: whatever it reached is worth reading back, so the
	// window can show the stocks it did finish.
	data, readErr := os.ReadFile(opts.OutPath)
	if ctx.Err() != nil {
		if readErr == nil {
			return data, ctx.Err()
		}
		return nil, ctx.Err()
	}
	if waitErr != nil {
		if code, ok := exitCode(waitErr); ok && code == Unreachable {
			return nil, fmt.Errorf("%w%s", ErrUnreachable, tail.suffix())
		}
		return nil, fmt.Errorf("data layer failed: %w%s", waitErr, tail.suffix())
	}
	if readErr != nil {
		return nil, fmt.Errorf("read dataset: %w", readErr)
	}
	return data, nil
}

// tailBuffer keeps the recent lines that were not progress events, so a
// failure can report the child's own explanation.
type tailBuffer struct {
	mu    sync.Mutex
	lines []string
}

const tailLines = 6

func (b *tailBuffer) add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > tailLines {
		b.lines = b.lines[len(b.lines)-tailLines:]
	}
}

func (b *tailBuffer) suffix() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(b.lines, "\n")
}

func scanEvents(r io.Reader, tail *tailBuffer, onEvent func(Event)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line[0] != '{' {
			tail.add(line)
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			tail.add(line)
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
}

// exitCode is the status a finished child exited with, if it exited on its
// own rather than being killed.
func exitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// resolvePython honours an explicit interpreter, then the venv in dir, then
// whatever `python` is on PATH. The venv is looked for in dir rather than the
// working directory, because the app is usually launched from elsewhere.
func resolvePython(dir, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	candidates := []string{}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join("Scripts", "python.exe"))
	} else {
		candidates = append(candidates, filepath.Join("bin", "python3"),
			filepath.Join("bin", "python"))
	}
	for _, c := range candidates {
		venv := filepath.Join(dir, ".venv", c)
		if _, err := os.Stat(venv); err == nil {
			return venv, nil
		}
	}
	if p, err := exec.LookPath("python"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no Python interpreter found; set one in Settings or create .venv")
}
