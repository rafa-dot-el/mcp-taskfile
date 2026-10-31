package main

import (
	"bytes"
	"github.com/fsnotify/fsnotify"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		raw  string
		want slog.Level
		ok   bool
	}{
		{"", slog.LevelInfo, true},
		{"info", slog.LevelInfo, true},
		{"  INFO ", slog.LevelInfo, true},
		{"trace", levelTrace, true},
		{"debug", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"crit", levelCrit, true},
		{"critical", levelCrit, true},
		{"fatal", levelCrit, true},
		{"loud", 0, false},
	}
	for _, tc := range cases {
		got, err := parseLogLevel(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("parseLogLevel(%q) err = %v, want ok=%v", tc.raw, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestRenderLogLevel(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{levelTrace, "TRACE"},
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
		{levelCrit, "CRIT"},
	}
	for _, tc := range cases {
		if got := renderLogLevel(tc.level); got != tc.want {
			t.Errorf("renderLogLevel(%v) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestNewComponentLogger(t *testing.T) {
	t.Setenv("MCP_TASKFILE_LOG_LEVEL", "debug")
	logger, level, err := newComponentLogger("taskfile")
	if err != nil {
		t.Fatal(err)
	}
	if logger == nil || level != slog.LevelDebug {
		t.Fatalf("got logger=%v level=%v, want non-nil logger at debug", logger, level)
	}

	t.Setenv("MCP_TASKFILE_LOG_LEVEL", "loud")
	if _, _, err := newComponentLogger("taskfile"); err == nil {
		t.Fatal("expected an error for an unsupported log level")
	}
}

func TestLogCriticalUsesCritLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: levelTrace,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Value = slog.StringValue(renderLogLevel(a.Value.Any().(slog.Level)))
			}
			return a
		},
	}))
	logCritical(logger, "boom", "k", "v")
	if out := buf.String(); !strings.Contains(out, "level=CRIT") || !strings.Contains(out, "msg=boom") {
		t.Fatalf("unexpected log line: %q", out)
	}
}

func TestNormalizeTaskfilePath(t *testing.T) {
	base := t.TempDir()
	if _, ok := normalizeTaskfilePath(base, ""); ok {
		t.Error("empty path must be rejected")
	}
	got, ok := normalizeTaskfilePath(base, "sub/../Taskfile.yml")
	if !ok || got != filepath.Join(base, "Taskfile.yml") {
		t.Errorf("relative path: got %q ok=%v", got, ok)
	}
	abs := filepath.Join(base, "x", "Taskfile.yml")
	got, ok = normalizeTaskfilePath("/ignored", abs)
	if !ok || got != abs {
		t.Errorf("absolute path: got %q ok=%v", got, ok)
	}
}

func TestCombineOutput(t *testing.T) {
	cases := []struct {
		out, err, want string
	}{
		{"", "", ""},
		{"o", "", "o"},
		{"", "e", "[stderr]\ne"},
		{"o", "e", "o\n[stderr]\ne"},
	}
	for _, tc := range cases {
		if got := combineOutput([]byte(tc.out), []byte(tc.err)); got != tc.want {
			t.Errorf("combineOutput(%q,%q) = %q, want %q", tc.out, tc.err, got, tc.want)
		}
	}
}

// normalizeTaskfilePath is pure: a relative base stays relative and the
// process working directory is never consulted (main resolves --dir once).
func TestNormalizeTaskfilePathIgnoresWorkingDirectory(t *testing.T) {
	got, ok := normalizeTaskfilePath("rel", "a/../Taskfile.yml")
	if want := filepath.Join("rel", "Taskfile.yml"); !ok || got != want {
		t.Errorf("got %q ok=%v, want %q", got, ok, want)
	}
}

func TestTaskInfoEqualComparesVarsElementwise(t *testing.T) {
	a := TaskInfo{Name: "t", Task: "t", Vars: []string{"A", "B"}}
	b := TaskInfo{Name: "t", Task: "t", Vars: []string{"A", "C"}}
	if taskInfoEqual(a, b) {
		t.Error("tasks with different vars of equal count must differ")
	}
	if !taskInfoEqual(a, a) {
		t.Error("a task must equal itself")
	}
}

func TestShouldReloadForEvent(t *testing.T) {
	root := t.TempDir()
	included := filepath.Join(root, "inc", "Taskfile.yml")
	w := &Watcher{
		rootDir:      root,
		watchedFiles: map[string]struct{}{included: {}},
	}
	cases := []struct {
		name  string
		event fsnotify.Event
		want  bool
	}{
		{"chmod only", fsnotify.Event{Name: filepath.Join(root, "Taskfile.yml"), Op: fsnotify.Chmod}, false},
		{"empty name", fsnotify.Event{Name: "", Op: fsnotify.Write}, false},
		{"watched include", fsnotify.Event{Name: included, Op: fsnotify.Write}, true},
		{"root Taskfile.yaml created", fsnotify.Event{Name: filepath.Join(root, "Taskfile.yaml"), Op: fsnotify.Create}, true},
		{"root unrelated file", fsnotify.Event{Name: filepath.Join(root, "README.org"), Op: fsnotify.Write}, false},
		{"Taskfile.yml outside root", fsnotify.Event{Name: filepath.Join(root, "other", "Taskfile.yml"), Op: fsnotify.Remove}, false},
	}
	for _, tc := range cases {
		if got := w.shouldReloadForEvent(tc.event); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
