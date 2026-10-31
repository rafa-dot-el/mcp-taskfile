package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 500 * time.Millisecond

// Watcher monitors a Taskfile for changes and triggers tool reload.
type Watcher struct {
	server       *Server
	rootDir      string
	logger       *slog.Logger
	watchedDirs  map[string]struct{}
	watchedFiles map[string]struct{}
}

// NewWatcher creates a file watcher for the workspace root and any contributing
// Taskfiles discovered from it.
func NewWatcher(server *Server, dir string, logger *slog.Logger) *Watcher {
	rootDir, ok := normalizeTaskfilePath(".", dir)
	if !ok {
		rootDir = dir
	}

	return &Watcher{
		server:       server,
		rootDir:      rootDir,
		logger:       logger,
		watchedDirs:  make(map[string]struct{}),
		watchedFiles: make(map[string]struct{}),
	}
}

// Run starts watching the Taskfile for changes. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Error("failed to create file watcher", "err", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	if err := w.refreshWatchSet(watcher); err != nil {
		w.logger.Error("failed to configure taskfile watcher", "dir", w.rootDir, "err", err)
		return
	}

	w.logger.Info("watching taskfiles for changes", "dir", w.rootDir)

	var (
		timer  *time.Timer
		timerC <-chan time.Time
	)
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				stopTimer(timer)
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !w.shouldReloadForEvent(event) {
				continue
			}

			if timer != nil {
				stopTimer(timer)
				timer.Reset(debounceInterval)
			} else {
				timer = time.NewTimer(debounceInterval)
			}
			timerC = timer.C

		case <-timerC:
			timerC = nil
			w.reload(watcher)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("watcher error", "err", err)
		}
	}
}

func (w *Watcher) reload(watcher *fsnotify.Watcher) {
	changed, err := w.server.ReloadTools()
	if err != nil {
		w.logger.Error("failed to reload taskfile", "dir", w.rootDir, "err", err)
		return
	}
	if err := w.refreshWatchSet(watcher); err != nil {
		w.logger.Error("failed to refresh taskfile watch set", "dir", w.rootDir, "err", err)
		return
	}
	if changed {
		w.logger.Info("taskfile changed, tools updated", "dir", w.rootDir)
	}
}

func (w *Watcher) refreshWatchSet(watcher *fsnotify.Watcher) error {
	nextDirs := map[string]struct{}{
		w.rootDir: {},
	}
	nextFiles := make(map[string]struct{})

	for _, file := range w.server.SourceFiles() {
		normalized, ok := normalizeTaskfilePath(".", file)
		if !ok {
			continue
		}
		nextFiles[normalized] = struct{}{}
		nextDirs[filepath.Dir(normalized)] = struct{}{}
	}

	for dir := range nextDirs {
		if _, alreadyWatching := w.watchedDirs[dir]; alreadyWatching {
			continue
		}
		if err := watcher.Add(dir); err != nil {
			return err
		}
	}

	for dir := range w.watchedDirs {
		if _, keep := nextDirs[dir]; keep {
			continue
		}
		if err := watcher.Remove(dir); err != nil {
			w.logger.Warn("failed to remove watch directory", "dir", dir, "err", err)
		}
	}

	w.watchedDirs = nextDirs
	w.watchedFiles = nextFiles
	return nil
}

func (w *Watcher) shouldReloadForEvent(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}

	path, ok := normalizeTaskfilePath(".", event.Name)
	if !ok {
		return false
	}

	if _, watched := w.watchedFiles[path]; watched {
		return true
	}

	return filepath.Dir(path) == w.rootDir && isRootTaskfileName(filepath.Base(path))
}

func isRootTaskfileName(name string) bool {
	return name == "Taskfile.yml" || name == "Taskfile.yaml"
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
