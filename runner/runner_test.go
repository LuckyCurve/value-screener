package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func collect(lines string) []Event {
	var got []Event
	scanEvents(strings.NewReader(lines), &tailBuffer{}, func(ev Event) { got = append(got, ev) })
	return got
}

func TestScanEventsReadsOneJSONObjectPerLine(t *testing.T) {
	got := collect(`{"event":"phase","name":"first pass"}
{"event":"stock","index":1,"total":5,"code":"HK.00700","name":"Tencent"}
`)
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(got), got)
	}
	if got[0].Event != "phase" || got[0].Name != "first pass" {
		t.Errorf("phase event wrong: %+v", got[0])
	}
	if got[1].Code != "HK.00700" || got[1].Index != 1 || got[1].Total != 5 {
		t.Errorf("stock event wrong: %+v", got[1])
	}
}

func TestScanEventsSkipsProseLines(t *testing.T) {
	// The SDK logs its own lines to the same stream; they are not progress.
	got := collect(`2026-10-23 19:38:31 | info | connecting
{"event":"found","count":3}
`)
	if len(got) != 1 || got[0].Event != "found" || got[0].Count != 3 {
		t.Fatalf("prose should be skipped, got %+v", got)
	}
}

func TestScanEventsSkipsMalformedJSON(t *testing.T) {
	got := collect(`{not json
{"event":"done","count":2}
`)
	if len(got) != 1 || got[0].Event != "done" {
		t.Fatalf("malformed line should be skipped, got %+v", got)
	}
}

func TestScanEventsHandlesBlankLines(t *testing.T) {
	got := collect("\n\n{\"event\":\"done\",\"count\":0}\n\n")
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %+v", got)
	}
}

func TestScanEventsToleratesLongLines(t *testing.T) {
	long := `{"event":"stock","name":"` + strings.Repeat("x", 200000) + `"}`
	got := collect(long + "\n")
	if len(got) != 1 || len(got[0].Name) != 200000 {
		t.Fatalf("a long line should still parse, got %d events", len(got))
	}
}

func TestResolvePythonHonoursExplicitPath(t *testing.T) {
	got, err := resolvePython("", "/custom/python")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/custom/python" {
		t.Errorf("want the explicit path, got %q", got)
	}
}

func TestResolvePythonFindsTheVenvInTheProjectDir(t *testing.T) {
	dir := t.TempDir()
	var venv string
	if runtime.GOOS == "windows" {
		venv = filepath.Join(dir, ".venv", "Scripts", "python.exe")
	} else {
		venv = filepath.Join(dir, ".venv", "bin", "python3")
	}
	if err := os.MkdirAll(filepath.Dir(venv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venv, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePython(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != venv {
		t.Errorf("want the project venv %q, got %q", venv, got)
	}
}

func TestRunUsesTheDataLayerBinaryWhenGiven(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dataset.json")
	// A script stands in for the frozen data layer: it records its arguments
	// and writes a dataset, so the wiring is checked without Python.
	script := filepath.Join(dir, "frozen")
	if runtime.GOOS == "windows" {
		script += ".bat"
	}
	body := "echo %* > " + filepath.Join(dir, "args.txt") + "\n" +
		"echo {\"stocks\":[]} > " + out + "\n"
	if runtime.GOOS != "windows" {
		body = "#!/bin/sh\necho \"$@\" > " + filepath.Join(dir, "args.txt") + "\n" +
			"echo '{\"stocks\":[]}' > " + out + "\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	data, err := Run(context.Background(), dir, Options{
		DataLayer: script,
		Args:      []string{"figures", "--config", filepath.Join(dir, "criteria.json")},
		OutPath:   out,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(data) == 0 {
		t.Error("no dataset read back")
	}
	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatalf("the binary never ran: %v", err)
	}
	got := string(args)
	if strings.Contains(got, "-m datalayer") || !strings.Contains(got, "figures") {
		t.Errorf("args %q should be the layer's own, not a python invocation", got)
	}
}

// A cancelled run must stop the work and still return what was written: the
// window shows those stocks instead of losing the whole screen.
func TestCancelStopsTheChildAndKeepsWhatItWrote(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dataset.json")
	done := filepath.Join(dir, "done.txt")
	script := filepath.Join(dir, "frozen")
	// Writes one stock, says it is there, then hangs as the real layer does
	// between stocks.
	var body string
	if runtime.GOOS == "windows" {
		script += ".bat"
		body = "> \"" + out + "\" echo {\"stocks\":[{\"code\":\"HK.00700\"}]\r\n" +
			"type nul > \"" + done + "\"\r\n" +
			"ping -n 31 127.0.0.1 > nul\r\n"
	} else {
		body = "#!/bin/sh\n" +
			"printf '%s' '{\"stocks\":[{\"code\":\"HK.00700\"}]}' > " + out + "\n" +
			"touch " + done + "\n" +
			"sleep 30\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := os.Stat(done); err == nil {
				cancel()
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()

	started := time.Now()
	data, err := Run(ctx, dir, Options{DataLayer: script, Args: []string{"figures"},
		OutPath: out})
	took := time.Since(started)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run should say so, got %v", err)
	}
	if !strings.Contains(string(data), "HK.00700") {
		t.Errorf("the stocks written before the cancel should come back, got %q", data)
	}
	if took > 10*time.Second {
		t.Errorf("cancel took %v; the child should be killed, not waited out", took)
	}
}

// The file a previous run left must not be read as this run's answer: a
// child that dies without writing would otherwise return stale figures.
func TestStaleDatasetIsNotReturned(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dataset.json")
	if err := os.WriteFile(out, []byte(`{"stocks":[{"code":"HK.OLD"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "frozen")
	body := "exit 2\n"
	if runtime.GOOS == "windows" {
		script += ".bat"
		body = "exit /b 2\r\n"
	} else {
		body = "#!/bin/sh\n" + body
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), dir, Options{DataLayer: script,
		Args: []string{"figures"}, OutPath: out})
	if err == nil {
		t.Fatal("a failing child should fail the run")
	}
	if strings.Contains(err.Error(), "HK.OLD") {
		t.Errorf("stale figures leaked into the run: %v", err)
	}
}

// A child that found nothing listening exits with Unreachable, and that is
// not the same as a broken run: the window answers it with its OpenD tips.
func TestAnUnreachableChannelIsItsOwnFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "frozen")
	body := "echo 'OpenD is not running on 127.0.0.1:11111' >&2\nexit 3\n"
	if runtime.GOOS == "windows" {
		script += ".bat"
		body = "echo OpenD is not running on 127.0.0.1:11111 1>&2\r\n" +
			"exit /b 3\r\n"
	} else {
		body = "#!/bin/sh\n" + body
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), dir, Options{DataLayer: script,
		Args: []string{"candidates"}, OutPath: filepath.Join(dir, "out.json")})
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
}

func TestFailureReportsTheChildsOwnWords(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "frozen")
	// The child's progress and its reasons for failing go to stderr, as the
	// real data layer's do; stdout carries the dataset.
	body := "echo '{\"event\":\"phase\",\"name\":\"first pass\"}' >&2\n" +
		"echo 'OpenD is not running on 127.0.0.1:11111' >&2\nexit 2\n"
	if runtime.GOOS == "windows" {
		script += ".bat"
		body = "echo {\"event\":\"phase\",\"name\":\"first pass\"} 1>&2\r\n" +
			"echo OpenD is not running on 127.0.0.1:11111 1>&2\r\n" +
			"exit /b 2\r\n"
	} else {
		body = "#!/bin/sh\n" + body
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), dir, Options{
		DataLayer: script,
		Args:      []string{"figures"},
		OutPath:   filepath.Join(dir, "dataset.json"),
	})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "OpenD is not running") {
		t.Errorf("a failure should carry the child's reason, got %v", err)
	}
}
