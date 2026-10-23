package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/LuckyCurve/value-screener/config"
	"github.com/LuckyCurve/value-screener/dataset"
	"github.com/LuckyCurve/value-screener/engine"
	"github.com/LuckyCurve/value-screener/runner"
)

// DefaultScreen is the real pipeline: fetch with the Python data layer, then
// judge in Go. The verdicts go straight back to the window.
func DefaultScreen() Screen {
	return func(ctx context.Context, c config.Criteria,
		progress func(index, total int, message string)) (engine.Result, error) {

		dir, workDir := layout()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return engine.Result{}, fmt.Errorf("prepare %s: %w", dir, err)
		}
		cfgPath, err := writeCriteria(dir, c)
		if err != nil {
			return engine.Result{}, err
		}

		data, err := runner.Run(ctx, workDir, runner.Options{
			DataLayer:  DataLayer(),
			ConfigPath: cfgPath,
			OutPath:    filepath.Join(dir, "dataset.json"),
			OnEvent: func(ev runner.Event) {
				switch ev.Event {
				case "found":
					progress(0, ev.Count, "")
				case "stock":
					progress(ev.Index, ev.Total, ev.Code)
				case "error":
					progress(0, 0, ev.Message)
				}
			},
		})
		if err != nil {
			return engine.Result{}, dataLayerError(err)
		}

		verdicts, err := dataset.Parse(data)
		if err != nil {
			return engine.Result{}, err
		}
		judged := make([]engine.Verdict, 0, len(verdicts))
		for _, v := range verdicts {
			judged = append(judged, engine.Evaluate(v.Stock, v.ROE, v.Balance, c))
		}
		return engine.BuildResult(c, judged), nil
	}
}

// dataLayerError turns the data layer's exit into something the user can act
// on, rather than a Go exec error.
func dataLayerError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("the data layer stopped: %w", err)
}

// dataLayerBinary is the frozen data layer shipped beside the app.
const dataLayerBinary = "value-screener-dl"

// DataLayer returns the frozen data layer to run directly, or "" to run the
// Python package instead. A packaged build puts the binary beside the app;
// a source build uses the package and the project's venv.
func DataLayer() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	name := dataLayerBinary
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// BaseDir is the folder the app reads its settings from: its own folder when
// packaged, the project when run from source.
func BaseDir() string {
	if DataLayer() != "" {
		if exe, err := os.Executable(); err == nil {
			return filepath.Dir(exe)
		}
	}
	if dir, err := ProjectDir(); err == nil {
		return dir
	}
	wd, _ := os.Getwd()
	return wd
}

// layout returns where run files go and the directory the data layer runs in.
//
// A packaged build keeps everything in <app folder>/data and runs the frozen
// binary there. A source build writes beside the project and runs Python,
// which needs the project as its working directory.
func layout() (dir, workDir string) {
	if DataLayer() != "" {
		if exe, err := os.Executable(); err == nil {
			base := filepath.Dir(exe)
			return filepath.Join(base, "data"), base
		}
	}
	if pd, err := ProjectDir(); err == nil {
		return pd, pd
	}
	wd, _ := os.Getwd()
	return wd, wd
}

// ProjectDir finds the directory holding the datalayer package, searching
// from the executable outward so the app works wherever it is built to. Run
// from the source tree it resolves to that tree; failing that, the working
// directory.
func ProjectDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the app: %w", err)
	}
	if dir, ok := findProject(filepath.Dir(exe)); ok {
		return dir, nil
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

// writeCriteria hands the run's rules to the data layer, which needs the
// markets and windows to know what to fetch.
func writeCriteria(dir string, c config.Criteria) (string, error) {
	data, err := config.Marshal(c)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "criteria.run.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write criteria: %w", err)
	}
	return path, nil
}
