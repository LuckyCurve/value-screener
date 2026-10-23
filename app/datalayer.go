package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// The data layer is a frozen Python build. A packaged app ships it in one of
// two shapes:
//
//   - inside the executable, when it was built with `-tags datalayer_embedded`
//     and app/datalayer.bin. That is the one-file release: the app unpacks it
//     into the user's cache the first time a run needs it.
//   - beside the executable, as value-screener-dl.exe, for a folder build.
//
// A source build has neither, and runs `python -m datalayer` from the project.

// dataLayerBinary is the frozen data layer's file name.
const dataLayerName = "value-screener-dl"

var (
	layerOnce sync.Once
	layerPath string
	layerErr  error
)

// DataLayer returns the frozen data layer to run directly, "" to run the
// Python package instead, or the reason it could not be unpacked.
func DataLayer() (string, error) {
	layerOnce.Do(func() { layerPath, layerErr = resolveDataLayer() })
	return layerPath, layerErr
}

func resolveDataLayer() (string, error) {
	name := dataLayerName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if len(embeddedDataLayer) > 0 {
		return unpackDataLayer(name)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil
	}
	path := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", nil
}

// unpackDataLayer writes the embedded data layer into the user's cache,
// once per build, and returns where it landed.
//
// The content's hash names the file, so a new build never reads an old
// copy, and a half-written file is never used: the bytes go to a temporary
// name first and are renamed into place.
func unpackDataLayer(name string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	sum := sha256.Sum256(embeddedDataLayer)
	dir = filepath.Join(dir, "value-screener", hex.EncodeToString(sum[:8]))
	path := filepath.Join(dir, name)

	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(embeddedDataLayer)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare the data layer's folder: %w", err)
	}

	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return "", fmt.Errorf("unpack the data layer: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(embeddedDataLayer); err != nil {
		tmp.Close()
		return "", fmt.Errorf("unpack the data layer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("unpack the data layer: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil && !errors.Is(err, fs.ErrPermission) {
		return "", fmt.Errorf("unpack the data layer: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("unpack the data layer: %w", err)
	}
	return path, nil
}
