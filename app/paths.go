package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LuckyCurve/value-screener/cache"
	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/screener"
	"github.com/LuckyCurve/value-screener/source"
)

// BaseDir is the folder the app reads its settings from: its own folder when
// packaged, the project when run from source.
func BaseDir() string {
	if dir, err := exeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "criteria.json")); err == nil {
			return dir
		}
		if layer, _ := DataLayer(); layer != "" {
			if _, err := os.Stat(filepath.Join(dir, filepath.Base(layer))); err == nil {
				return dir
			}
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

// DefaultScreen builds the real pipeline: a Futu channel, a SQLite cache of
// closed years, and the engine deciding. Only the source names Futu, so
// swapping or adding a channel leaves the cache and the rules alone.
func DefaultScreen() Screen {
	return func(ctx context.Context, c config.Criteria,
		progress func(index, total int, message string)) (engine.Result, error) {

		l, err := newScreen(c)
		if err != nil {
			return engine.Result{}, err
		}
		defer l.close()
		res, err := l.screen.Run(ctx, func(i, n int, msg string) { progress(i, n, msg) })
		if err != nil {
			return engine.Result{}, dataLayerError(err)
		}
		return res, nil
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

// dataLayerError turns a channel failure into something the user can act on.
func dataLayerError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("the data layer stopped: %w", err)
}
