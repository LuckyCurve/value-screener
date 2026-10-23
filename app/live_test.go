//go:build live

// This test reaches the running OpenD on 127.0.0.1:11111 and the live Futu
// servers: `go test -tags live ./app -run Live -v`. It stays out of ordinary
// runs so the suite never depends on the market being open.
package app

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyCurve/value-screener/config"
)

// TestLiveScreenRunsTheWholePipeline checks the pieces the app glues
// together against a real OpenD: the child process, its progress lines, the
// figures it reports, the cache, and the verdicts the engine draws.
func TestLiveScreenRunsTheWholePipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c := config.Default()
	c.Markets = []string{"HK"}

	var sawProgress bool
	started := time.Now()
	res, err := DefaultScreen()(ctx, c, func(index, total int, message string) {
		sawProgress = true
	})
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if !sawProgress {
		t.Error("progress never reached the window")
	}
	if n := len(res.Passed) + len(res.Rejected) + len(res.Missing); n == 0 {
		t.Fatal("the screen returned no stocks")
	}
	t.Logf("judged %d stocks in %s: %d passed, %d rejected, %d missing",
		len(res.Passed)+len(res.Rejected)+len(res.Missing),
		time.Since(started).Round(time.Second), len(res.Passed),
		len(res.Rejected), len(res.Missing))

	for _, v := range res.Missing {
		t.Logf("missing: %s %s: %s", v.Stock.Code, v.Stock.Name, v.Unavailable)
	}
}
