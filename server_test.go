package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindTaskfile(t *testing.T) {
	dir := filepath.Join("testdata")
	path, err := FindTaskfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Taskfile.yml" {
		t.Errorf("expected Taskfile.yml, got %s", filepath.Base(path))
	}
}

func TestFindTaskfileMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := FindTaskfile(dir)
	if !errors.Is(err, ErrNoTaskfile) {
		t.Fatalf("expected ErrNoTaskfile, got %v", err)
	}
}

func TestParseTaskfile(t *testing.T) {
	parsed, err := ParseTaskfile("testdata")
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, task := range parsed.Tasks {
		names[task.Name] = true
	}

	for _, want := range []string{"build", "test", "lint", "deploy"} {
		if !names[want] {
			t.Errorf("expected task %q to be present, got tasks: %v", want, names)
		}
	}

	for _, excluded := range []string{"_internal", "no-desc"} {
		if names[excluded] {
			t.Errorf("expected task %q to be excluded", excluded)
		}
	}
}

func TestParseTaskfileVars(t *testing.T) {
	parsed, err := ParseTaskfile("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, task := range parsed.Tasks {
		if task.Name == "deploy" {
			if len(task.Vars) == 0 {
				t.Error("expected deploy task to have vars")
			}
			found := false
			for _, v := range task.Vars {
				if v == "ENV" {
					found = true
				}
			}
			if !found {
				t.Errorf("expected deploy task to have ENV var, got %v", task.Vars)
			}
			return
		}
	}
	t.Error("deploy task not found")
}

func TestParseTaskfileSourceFiles(t *testing.T) {
	parsed, err := ParseTaskfile("testdata")
	if err != nil {
		t.Fatal(err)
	}

	bases := fileBases(parsed.TaskfilePaths)
	if !bases["Taskfile.yml"] {
		t.Fatalf("expected root Taskfile.yml in source files, got %v", parsed.TaskfilePaths)
	}
}

func TestParseTaskfileMatchesTaskListWithIncludes(t *testing.T) {
	parsed, err := ParseTaskfile(filepath.Join("testdata", "includes"))
	if err != nil {
		t.Fatal(err)
	}

	got := taskNames(parsed.Tasks)
	want := []string{"root", "shared_public"}
	if !equalStringSlices(got, want) {
		t.Fatalf("unexpected public tasks: got %v want %v", got, want)
	}

	bases := fileBases(parsed.TaskfilePaths)
	for _, name := range []string{"Taskfile.yml", "shared.yml", "hidden.yml"} {
		if !bases[name] {
			t.Fatalf("expected %s in source files, got %v", name, parsed.TaskfilePaths)
		}
	}
}

func TestParseTaskfileSourceFilesIncludeNestedVarsOnlyFile(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "sourcegraph", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))
	copyFile(t, filepath.Join("testdata", "sourcegraph", "shared-with-nested.yml"), filepath.Join(dir, "shared.yml"))
	copyFile(t, filepath.Join("testdata", "sourcegraph", "nested", "vars.yml"), filepath.Join(dir, "nested", "vars.yml"))

	parsed, err := ParseTaskfile(dir)
	if err != nil {
		t.Fatal(err)
	}

	bases := fileBases(parsed.TaskfilePaths)
	for _, name := range []string{"Taskfile.yml", "shared.yml", "vars.yml"} {
		if !bases[name] {
			t.Fatalf("expected %s in source files, got %v", name, parsed.TaskfilePaths)
		}
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"build", "build"},
		{"ns:task", "ns_task"},
		{"a:b:c", "a_b_c"},
	}
	for _, tc := range tests {
		got := sanitizeToolName(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTasksChanged(t *testing.T) {
	a := []TaskInfo{{Name: "build", Desc: "Build"}, {Name: "test", Desc: "Test"}}
	b := []TaskInfo{{Name: "build", Desc: "Build"}, {Name: "test", Desc: "Test"}}
	if tasksChanged(a, b) {
		t.Error("identical sets should not be considered changed")
	}

	c := []TaskInfo{{Name: "build", Desc: "Build"}, {Name: "deploy", Desc: "Deploy"}}
	if !tasksChanged(a, c) {
		t.Error("different sets should be considered changed")
	}

	d := []TaskInfo{{Name: "build", Desc: "Build"}}
	if !tasksChanged(a, d) {
		t.Error("different lengths should be considered changed")
	}

	e := []TaskInfo{{Name: "deploy", Desc: "Deploy", Vars: []string{"ENV"}}}
	f := []TaskInfo{{Name: "deploy", Desc: "Deploy", Vars: []string{"REGION", "ENV"}}}
	if !tasksChanged(e, f) {
		t.Error("var changes should be considered changed")
	}

	g := []TaskInfo{{Name: "deploy", Desc: "Deploy", Vars: []string{"REGION", "ENV"}}}
	h := []TaskInfo{{Name: "deploy", Desc: "Deploy", Vars: []string{"ENV", "REGION"}}}
	if tasksChanged(g, h) {
		t.Error("var ordering should not be considered changed")
	}
}

func TestServerLoadTools(t *testing.T) {
	srv := NewServer("testdata", testLogger())

	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	if len(srv.toolKeys) == 0 {
		t.Error("expected tools to be registered")
	}

	sort.Strings(srv.toolKeys)
	expected := []string{"build", "deploy", "lint", "test"}
	sort.Strings(expected)

	if len(srv.toolKeys) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(srv.toolKeys), srv.toolKeys)
	}
	for i, name := range expected {
		if srv.toolKeys[i] != name {
			t.Errorf("tool[%d] = %q, want %q", i, srv.toolKeys[i], name)
		}
	}
}

func TestServerReload(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))

	srv := NewServer(dir, testLogger())

	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}
	initialCount := len(srv.toolKeys)

	copyFile(t, filepath.Join("testdata", "Taskfile-reload.yml"), filepath.Join(dir, "Taskfile.yml"))

	changed, err := srv.ReloadTools()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected tools to change after reload")
	}
	if len(srv.toolKeys) == initialCount {
		t.Error("expected different tool count after reload")
	}
}

func TestServerLoadToolsMissingTaskfileClearsState(t *testing.T) {
	srv := NewServer(t.TempDir(), testLogger())
	if err := srv.LoadTools(); !errors.Is(err, ErrNoTaskfile) {
		t.Fatalf("expected ErrNoTaskfile, got %v", err)
	}
	if len(currentToolKeys(srv)) != 0 {
		t.Fatalf("expected no registered tools, got %v", currentToolKeys(srv))
	}
	if len(srv.SourceFiles()) != 0 {
		t.Fatalf("expected no source files, got %v", srv.SourceFiles())
	}
}

func TestWatcherLoadsToolsWhenTaskfileCreated(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); !errors.Is(err, ErrNoTaskfile) {
		t.Fatalf("expected ErrNoTaskfile, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		return len(currentToolKeys(srv)) == 4
	})
}

func TestWatcherLoadsToolsWhenTaskfileYamlCreated(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); !errors.Is(err, ErrNoTaskfile) {
		t.Fatalf("expected ErrNoTaskfile, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yaml"))

	waitForCondition(t, 5*time.Second, func() bool {
		return len(currentToolKeys(srv)) == 4
	})
}

func TestWatcherReloadsOnIncludedTaskfileChange(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "includes", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))
	copyFile(t, filepath.Join("testdata", "includes", "shared.yml"), filepath.Join(dir, "shared.yml"))
	copyFile(t, filepath.Join("testdata", "includes", "hidden.yml"), filepath.Join(dir, "hidden.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	copyFile(t, filepath.Join("testdata", "includes", "shared-reload.yml"), filepath.Join(dir, "shared.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		tasks := currentTasks(srv)
		public, ok := taskByName(tasks, "shared_public")
		if !ok {
			return false
		}
		if !equalStringSlices(public.Vars, []string{"ENV", "REGION"}) {
			return false
		}
		_, newTaskExists := taskByName(tasks, "shared_new-task")
		return newTaskExists
	})
}

func TestWatcherClearsToolsWhenTaskfileDeletedAndReloadsOnRecreate(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	if err := os.Remove(filepath.Join(dir, "Taskfile.yml")); err != nil {
		t.Fatal(err)
	}

	waitForCondition(t, 5*time.Second, func() bool {
		return len(currentToolKeys(srv)) == 0
	})

	copyFile(t, filepath.Join("testdata", "Taskfile-reload.yml"), filepath.Join(dir, "Taskfile.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		keys := currentToolKeys(srv)
		return equalStringSlices(keys, []string{"build", "new-task"})
	})
}

func TestWatcherKeepsLastGoodToolsOnInvalidRootTaskfile(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}
	initialKeys := currentToolKeys(srv)

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	writeFile(t, filepath.Join(dir, "Taskfile.yml"), `version: "3"
tasks:
  broken:
    desc: "Broken"
    cmds:
      - echo "broken"
  invalid: [
`)

	assertConditionStaysTrue(t, 1200*time.Millisecond, func() bool {
		return equalStringSlices(currentToolKeys(srv), initialKeys)
	})

	copyFile(t, filepath.Join("testdata", "Taskfile-reload.yml"), filepath.Join(dir, "Taskfile.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		return equalStringSlices(currentToolKeys(srv), []string{"build", "new-task"})
	})
}

func TestWatcherKeepsLastGoodToolsOnInvalidIncludedTaskfile(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "includes", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))
	copyFile(t, filepath.Join("testdata", "includes", "shared.yml"), filepath.Join(dir, "shared.yml"))
	copyFile(t, filepath.Join("testdata", "includes", "hidden.yml"), filepath.Join(dir, "hidden.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}
	initialKeys := currentToolKeys(srv)

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	writeFile(t, filepath.Join(dir, "shared.yml"), `version: "3"
tasks:
  public:
    desc: "Broken include"
    cmds:
      - echo "broken"
  invalid: [
`)

	assertConditionStaysTrue(t, 1200*time.Millisecond, func() bool {
		return equalStringSlices(currentToolKeys(srv), initialKeys)
	})

	copyFile(t, filepath.Join("testdata", "includes", "shared-reload.yml"), filepath.Join(dir, "shared.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		_, ok := taskByName(currentTasks(srv), "shared_new-task")
		return ok
	})
}

func TestWatcherRefreshesWatchSetWhenSourceGraphChangesWithoutToolChanges(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "sourcegraph", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))
	copyFile(t, filepath.Join("testdata", "sourcegraph", "shared-base.yml"), filepath.Join(dir, "shared.yml"))
	copyFile(t, filepath.Join("testdata", "sourcegraph", "nested", "vars.yml"), filepath.Join(dir, "nested", "vars.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	initialTask, ok := taskByName(currentTasks(srv), "shared_public")
	if !ok {
		t.Fatal("expected shared_public task to be present")
	}
	if initialTask.Desc != "Shared public task" {
		t.Fatalf("unexpected initial description: %q", initialTask.Desc)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startWatcher(t, ctx, srv, dir)
	defer stopWatcher(t, cancel, done)

	copyFile(t, filepath.Join("testdata", "sourcegraph", "shared-with-nested.yml"), filepath.Join(dir, "shared.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		bases := fileBases(srv.SourceFiles())
		task, ok := taskByName(currentTasks(srv), "shared_public")
		return bases["vars.yml"] && ok && task.Desc == "Shared public task"
	})

	copyFile(t, filepath.Join("testdata", "sourcegraph", "nested", "vars-reload.yml"), filepath.Join(dir, "nested", "vars.yml"))

	waitForCondition(t, 5*time.Second, func() bool {
		task, ok := taskByName(currentTasks(srv), "shared_public")
		return ok && task.Desc == "Shared public task updated"
	})
}

func TestServerReloadNotifiesConnectedClient(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "Taskfile.yml"), filepath.Join(dir, "Taskfile.yml"))

	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverSession, err := srv.mcp.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()

	notifications := make(chan struct{}, 1)
	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "1.0.0"},
		&mcp.ClientOptions{
			Logger: testLogger(),
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
				select {
				case notifications <- struct{}{}:
				default:
				}
			},
		},
	)

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientSession.Close() }()

	copyFile(t, filepath.Join("testdata", "Taskfile-reload.yml"), filepath.Join(dir, "Taskfile.yml"))

	changed, err := srv.ReloadTools()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected tool set to change after reload")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		select {
		case <-notifications:
			return true
		default:
			return false
		}
	})
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src) //nolint:gosec // test helper: src is a fixture path chosen by the test
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil { //nolint:gosec // test helper: dst is under t.TempDir()
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, dst, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func startWatcher(t *testing.T, ctx context.Context, srv *Server, dir string) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		NewWatcher(srv, dir, testLogger()).Run(ctx)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	return done
}

func stopWatcher(t *testing.T, cancel context.CancelFunc, done chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop in time")
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func assertConditionStaysTrue(t *testing.T, duration time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if !condition() {
			t.Fatal("condition became false before timeout")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func currentToolKeys(srv *Server) []string {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	keys := append([]string(nil), srv.toolKeys...)
	sort.Strings(keys)
	return keys
}

func currentTasks(srv *Server) []TaskInfo {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	tasks := make([]TaskInfo, 0, len(srv.tasks))
	for _, task := range srv.tasks {
		tasks = append(tasks, TaskInfo{
			Name: task.Name,
			Desc: task.Desc,
			Vars: append([]string(nil), task.Vars...),
		})
	}
	return tasks
}

func taskByName(tasks []TaskInfo, name string) (TaskInfo, bool) {
	for _, task := range tasks {
		if task.Name == name {
			sorted := append([]string(nil), task.Vars...)
			sort.Strings(sorted)
			task.Vars = sorted
			return task, true
		}
	}
	return TaskInfo{}, false
}

func taskNames(tasks []TaskInfo) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
}

func fileBases(paths []string) map[string]bool {
	bases := make(map[string]bool, len(paths))
	for _, path := range paths {
		bases[filepath.Base(path)] = true
	}
	return bases
}

func equalStringSlices(got, want []string) bool {
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if len(gotCopy) != len(wantCopy) {
		return false
	}
	for i := range gotCopy {
		if gotCopy[i] != wantCopy[i] {
			return false
		}
	}
	return true
}
