// Package runner invokes the Python data layer as a child process and reads
// its progress lines.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Event is one progress line the data layer writes to stderr.
type Event struct {
	Event   string `json:"event"`
	Name    string `json:"name,omitempty"`
	Index   int    `json:"index,omitempty"`
	Total   int    `json:"total,omitempty"`
	Count   int    `json:"count,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Options configure one run.
type Options struct {
	// Python is the interpreter to use. Empty means the app looks for one.
	Python string
	// ConfigPath is the criteria file handed to the data layer.
	ConfigPath string
	// OutPath receives the dataset JSON.
	OutPath string
	// Limit stops after N candidates. Zero fetches them all; tests and a
	// smoke run use a small one.
	Limit int
	// OnEvent, when set, is called for each progress line as it arrives.
	OnEvent func(Event)
}

// Run executes the data layer and returns its dataset JSON.
//
// The child is started in its own process group so a cancelled run leaves no
// orphaned Python holding an OpenD connection.
func Run(ctx context.Context, dir string, opts Options) ([]byte, error) {
	python, err := resolvePython(dir, opts.Python)
	if err != nil {
		return nil, err
	}

	args := []string{"-m", "datalayer", "--out", opts.OutPath}
	if opts.ConfigPath != "" {
		args = append(args, "--config", opts.ConfigPath)
	}
	if opts.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(opts.Limit))
	}

	cmd := exec.CommandContext(ctx, python, args...)
	cmd.Dir = dir
	hideWindow(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	var stdout strings.Builder
	cmd.Stdout = &stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start data layer: %w", err)
	}

	go scanEvents(stderr, opts.OnEvent)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("data layer failed: %w\n%s", err, stdout.String())
	}

	data, err := os.ReadFile(opts.OutPath)
	if err != nil {
		return nil, fmt.Errorf("read dataset: %w", err)
	}
	return data, nil
}

func scanEvents(r io.Reader, onEvent func(Event)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
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
