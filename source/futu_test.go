package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuckyCurve/value-screener/config"
)

// stubLayer writes a stand-in for the frozen data layer: it emits the two
// kinds of progress line the real one does, writes a one-stock packet, and
// exits. It is called as `figures --out <path> --config <path> …`, as the
// app calls the real one.
func stubLayer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packet := `{"stocks":[{"code":"US.TCEHY","name":"Tencent (ADR)",` +
		`"roe":{"2024":0.31},"balance":{"2024":{"cash":1,"debt":0}}}]}`

	var path, body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "layer.bat")
		body = "echo {\"event\":\"found\",\"count\":1} 1>&2\r\n" +
			"echo {\"event\":\"stock\",\"index\":1,\"total\":1,\"code\":\"US.TCEHY\"} 1>&2\r\n" +
			"> \"%~3\" echo " + packet + "\r\n" +
			"exit /b 0\r\n"
	} else {
		path = filepath.Join(dir, "layer")
		body = "#!/bin/sh\n" +
			"echo '{\"event\":\"found\",\"count\":1}' >&2\n" +
			"echo '{\"event\":\"stock\",\"index\":1,\"total\":1,\"code\":\"US.TCEHY\"}' >&2\n" +
			"printf '%s' '" + packet + "' > \"$3\"\n" +
			"exit 0\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A retry asks the channel about one stock with no progress to report: the
// window shows a row turning, not a bar. The channel must take that, not
// call through a nil function and take the process down with it.
func TestFiguresWithoutProgressIsFine(t *testing.T) {
	dir := t.TempDir()
	f := Futu{Dir: dir, Binary: stubLayer(t), Temp: dir}

	got, err := f.Figures(context.Background(), Query{}, []string{"US.TCEHY"}, nil)
	if err != nil {
		t.Fatalf("figures: %v", err)
	}
	if len(got) != 1 || got[0].Code != "US.TCEHY" {
		t.Fatalf("got %+v", got)
	}
	if got[0].ROE[2024] != 0.31 {
		t.Errorf("the figures were not read back: %+v", got[0])
	}
}

func TestCandidatesWithoutProgressIsFine(t *testing.T) {
	dir := t.TempDir()
	f := Futu{Dir: dir, Binary: stubLayer(t), Temp: dir}

	if _, err := f.Candidates(context.Background(), Query{}, nil); err != nil {
		t.Fatalf("candidates: %v", err)
	}
}

func TestProgressIsReportedWhenThereIsSomeoneListening(t *testing.T) {
	dir := t.TempDir()
	f := Futu{Dir: dir, Binary: stubLayer(t), Temp: dir}

	var found, stock bool
	_, err := f.Figures(context.Background(), Query{}, []string{"US.TCEHY"},
		func(index, total int, message string) {
			if total == 1 && message == "" {
				found = true
			}
			if message == "US.TCEHY" {
				stock = true
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if !found || !stock {
		t.Errorf("found=%v stock=%v", found, stock)
	}
}

// The query file is what tells the channel which markets and windows to
// fetch; it must carry the criteria through unchanged.
func TestQueryReachesTheDataLayer(t *testing.T) {
	dir := t.TempDir()
	f := Futu{Dir: dir, Binary: stubLayer(t), Temp: dir}

	c := config.Default()
	c.Markets = []string{"HK"}
	if _, err := f.Candidates(context.Background(), queryOf(c), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "criteria.run.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"markets": [`, `"HK"`, `"pe_max": 20`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the criteria file should hold %s, got %s", want, data)
		}
	}
}

// A stock the data layer finished reaches the caller before the run ends,
// so the window can show it while the rest are still being fetched.
func TestFiguresStreamReportsEachStockAsItArrives(t *testing.T) {
	dir := t.TempDir()
	layer := filepath.Join(dir, "layer")
	packet := `{"stocks":[{"code":"HK.00700","roe":{"2024":0.3}},` +
		`{"code":"HK.00005","roe":{"2024":0.2}}]}`
	line := `{"event":"judged","stock":{"code":"HK.00700","roe":{"2024":0.3},` +
		`"balance":{"2024":{"cash":1,"debt":0}}}}`
	body := "echo '" + line + "' >&2\n" +
		"printf '%s' '" + packet + "' > \"$3\"\n"
	if runtime.GOOS == "windows" {
		layer += ".bat"
		body = "echo " + line + " 1>&2\r\n" +
			"> \"%~3\" echo " + packet + "\r\n"
	} else {
		body = "#!/bin/sh\n" + body
	}
	if err := os.WriteFile(layer, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	f := Futu{Dir: dir, Binary: layer, Temp: dir}
	var streamed []string
	got, err := f.FiguresStream(context.Background(), Query{},
		[]string{"HK.00700", "HK.00005"}, func(a Annual) {
			streamed = append(streamed, a.Code)
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 1 || streamed[0] != "HK.00700" {
		t.Errorf("the finished stock should arrive streaming, got %v", streamed)
	}
	// The packet is still the authority, and holds both.
	if len(got) != 2 {
		t.Errorf("the packet should supply every stock, got %+v", got)
	}
}

// A channel with nothing listening is unplugged, not broken: it says so with
// ErrUnavailable, and keeps the log to itself.
func TestAnUnreachableChannelReportsErrUnavailable(t *testing.T) {
	dir := t.TempDir()
	layer := filepath.Join(dir, "layer")
	body := "echo 'OpenD is not running on 127.0.0.1:11111' >&2\nexit 3\n"
	if runtime.GOOS == "windows" {
		layer += ".bat"
		body = "echo OpenD is not running on 127.0.0.1:11111 1>&2\r\n" +
			"exit /b 3\r\n"
	} else {
		body = "#!/bin/sh\n" + body
	}
	if err := os.WriteFile(layer, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	f := Futu{Dir: dir, Binary: layer, Temp: dir}
	_, err := f.Candidates(context.Background(), Query{}, nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "echo") {
		t.Errorf("the log should not travel with the error: %v", err)
	}
}

func queryOf(c config.Criteria) Query {
	return Query{Markets: c.Markets, PEmax: c.PEmax, ROEMin: c.ROEMin,
		ROEYears: c.ROEYears, ROESlack: c.ROESlack,
		CashYears: c.CashYears, PercentileYears: c.PercentileYears}
}
