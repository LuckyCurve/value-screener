package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/LuckyCurve/value-screener/cache"
	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/screener"
	"github.com/LuckyCurve/value-screener/source"
)

// BaseDir is the folder the app reads its settings from and keeps its cache
// in: its own folder when packaged, the project when run from source.
func BaseDir() string {
	if packaged() {
		if dir, err := exeDir(); err == nil {
			return dir
		}
	}
	if dir, err := ProjectDir(); err == nil {
		return dir
	}
	if dir, err := exeDir(); err == nil {
		return dir
	}
	wd, _ := os.Getwd()
	return wd
}

// packaged reports whether this build ships as an app of its own rather than
// running from the project: it carries the data layer, or has one beside it.
// A source build has neither, and would find the project even from a folder
// inside it, which is why the layer decides and the project is the fallback.
func packaged() bool {
	if len(embeddedDataLayer) > 0 {
		return true
	}
	dir, err := exeDir()
	if err != nil {
		return false
	}
	name := dataLayerName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(dir, "criteria.json"))
	return err == nil
}

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// cachePath is where the closed fiscal years are kept, beside the settings.
func cachePath() string {
	return filepath.Join(BaseDir(), "value-screener-cache.db")
}

// ProjectDir finds the directory holding the datalayer package, so a source
// build knows where to run Python. It searches from the executable outward.
func ProjectDir() (string, error) {
	if dir, err := exeDir(); err == nil {
		if dir, ok := findProject(dir); ok {
			return dir, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if dir, ok := findProject(wd); ok {
			return dir, nil
		}
	}
	return "", fmt.Errorf("cannot find the datalayer package; run the app from the project directory")
}

func findProject(start string) (string, bool) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "datalayer")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// liveScreen is a screener wired to the channel and the cache, and the cache
// to close.
type liveScreen struct {
	screen screener.Screen
	store  *cache.Cache
}

func (l liveScreen) close() { l.store.Close() }

// newScreen wires one run: a channel, a SQLite cache of closed years, and
// the engine deciding. Only the source names Futu, so swapping or adding a
// channel leaves the cache and the rules alone.
func newScreen(c config.Criteria) (liveScreen, error) {
	layer, err := DataLayer()
	if err != nil {
		return liveScreen{}, err
	}

	dir := BaseDir()
	if layer == "" {
		if pd, err := ProjectDir(); err == nil {
			dir = pd
		}
	}
	if err := os.MkdirAll(filepath.Join(BaseDir(), "data"), 0o755); err != nil {
		return liveScreen{}, fmt.Errorf("prepare the data folder: %w", err)
	}

	store := cache.Open(cachePath())
	return liveScreen{
		screen: screener.Screen{
			Criteria: c,
			Source: source.Futu{
				Dir:    dir,
				Binary: layer,
				Temp:   filepath.Join(BaseDir(), "data"),
			},
			Cache: store,
		},
		store: store,
	}, nil
}

// errOpenD is what the window is told when the channel could not be reached:
// the run never began, so the answer is setup, not the rules.
var errOpenD = errors.New("OpenD is not running")

// errRunFailed is a run that stopped for a reason the user cannot act on
// from the window. The window keeps the cause in its message but leads with
// what to try.
var errRunFailed = errors.New("the screen could not finish")

// DefaultScreen builds the real pipeline: a Futu channel, a SQLite cache of
// closed years, and the engine deciding. Only the source names Futu, so
// swapping or adding a channel leaves the cache and the rules alone.
//
// onJudged is called for each stock as it is decided, so the window fills
// while the run is still fetching.
func DefaultScreen(onJudged func(engine.Verdict)) Screen {
	return func(ctx context.Context, c config.Criteria,
		progress func(index, total int, message string)) (engine.Result, error) {

		l, err := newScreen(c)
		if err != nil {
			return engine.Result{}, err
		}
		defer l.close()
		res, err := l.screen.Run(ctx, screener.Watch{
			Progress: func(i, n int, msg string) { progress(i, n, msg) },
			Judged:   onJudged,
		})
		// The result stands even when the run failed or was stopped: it
		// carries every stock that was judged before that.
		return res, dataLayerError(err)
	}
}

// DefaultRetry builds the real retry: the same channel and cache as a run,
// asked about one stock.
func DefaultRetry() Retrier {
	return func(ctx context.Context, c config.Criteria,
		stock engine.Stock) (engine.Verdict, error) {
		l, err := newScreen(c)
		if err != nil {
			return engine.Verdict{}, err
		}
		defer l.close()
		return l.screen.Retry(ctx, stock)
	}
}

// dataLayerError turns a channel failure into something the user can act on,
// in the words the window shows.
func dataLayerError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, source.ErrUnavailable) {
		return errOpenD
	}
	// The child's own words name things nobody outside the project can act
	// on; the window's Tips page says what to do about them.
	return errRunFailed
}
