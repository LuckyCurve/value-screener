package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/egoist/mygo/ui"
)

// TestShots writes pictures of the window for looking at by hand:
//
//	VALUE_SCREENER_SHOTS=dir go test ./app -run TestShots
func TestShots(t *testing.T) {
	dir := os.Getenv("VALUE_SCREENER_SHOTS")
	if dir == "" {
		t.Skip("set VALUE_SCREENER_SHOTS to a directory to write the pictures")
	}

	shots := []struct {
		name string
		app  func() *App
		act  func(*ui.Tester)
	}{
		{"idle", func() *App { return newTestApp() }, nil},
		{"missing", func() *App {
			a := newTestApp()
			a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
				return withMissing(), nil
			}
			return a
		}, func(tt *ui.Tester) { _ = tt.Click("No data (1)") }},
		{"dialog", func() *App { return newTestApp() }, func(tt *ui.Tester) {
			_ = tt.Click("Tencent")
		}},
		{"drill", func() *App {
			a := newTestApp()
			a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
				return withMissing(), nil
			}
			return a
		}, func(tt *ui.Tester) {
			_ = tt.Click("No data (1)")
			_ = tt.Click("Tencent (ADR)")
		}},
		{"stopped", func() *App {
			a := newTestApp()
			a.Screen = func(context.Context, config.Criteria, func(int, int, string)) (engine.Result, error) {
				return sample(), context.Canceled
			}
			return a
		}, nil},
		{"stopping", func() *App {
			a := newTestApp()
			a.Screen = func(ctx context.Context, _ config.Criteria, _ func(int, int, string)) (engine.Result, error) {
				<-ctx.Done()
				select {} // the child has not died yet
			}
			return a
		}, func(tt *ui.Tester) { _ = tt.Click("Cancel") }},
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range shots {
		a := s.app()
		tt := ui.NewTester(a.View, 1160, 760)
		runAndWait(t, tt)
		if s.act != nil {
			s.act(tt)
			tt.Frame()
		}
		f, err := os.Create(filepath.Join(dir, s.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Log("wrote", f.Name())
	}
}
