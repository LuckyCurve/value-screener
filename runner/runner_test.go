package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func collect(lines string) []Event {
	var got []Event
	scanEvents(strings.NewReader(lines), func(ev Event) { got = append(got, ev) })
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
