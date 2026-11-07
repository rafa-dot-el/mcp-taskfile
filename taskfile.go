package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	task "github.com/go-task/task/v3"
	taskfilepkg "github.com/go-task/task/v3/taskfile"
	taskast "github.com/go-task/task/v3/taskfile/ast"
)

// TaskInfo holds the metadata extracted from a parsed task for tool registration.
type TaskInfo struct {
	Name string // MCP tool name (sanitized: ":" replaced by "_")
	Task string // task name as declared in the Taskfile, passed to the executor
	Desc string
	Vars []string // variable names that have no default (become tool params)
}

// ParsedTaskfile contains the public task surface plus the Taskfile sources
// that should be watched for future reloads.
type ParsedTaskfile struct {
	Tasks         []TaskInfo
	TaskfilePaths []string
	// Collisions lists tasks dropped because an earlier task (in task-list
	// order) already sanitizes to the same tool name.
	Collisions []ToolNameCollision
}

// ToolNameCollision records a task that could not be registered because its
// tool name is already taken by Kept.
type ToolNameCollision struct {
	Tool    string
	Kept    string
	Dropped string
}

// taskfileLoadTimeout bounds reading the Taskfile graph (remote includes).
// It does not bound task execution; see Server.taskTimeout.
const taskfileLoadTimeout = 30 * time.Second

// ErrNoTaskfile is returned when the target directory does not contain a root
// Taskfile entrypoint.
var ErrNoTaskfile = errors.New("no Taskfile.yml or Taskfile.yaml found")

// FindTaskfile locates Taskfile.yml or Taskfile.yaml in the given directory.
func FindTaskfile(dir string) (string, error) {
	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w in %s", ErrNoTaskfile, dir)
}

// newExecutor creates and sets up a task.Executor for the given directory.
func newExecutor(dir string) (*task.Executor, error) {
	e := &task.Executor{
		Dir:     dir,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Stdin:   io.NopCloser(strings.NewReader("")),
		Silent:  true,
		Timeout: taskfileLoadTimeout,
	}
	if err := e.Setup(); err != nil {
		return nil, fmt.Errorf("executor setup for %s: %w", dir, err)
	}
	return e, nil
}

// ParseTaskfile reads the Taskfile in dir and returns tool-relevant metadata
// for each public task.
func ParseTaskfile(dir string) (*ParsedTaskfile, error) {
	if _, err := FindTaskfile(dir); err != nil {
		return nil, err
	}

	e, err := newExecutor(dir)
	if err != nil {
		return nil, err
	}

	sourcePaths, err := collectTaskfilePaths(e)
	if err != nil {
		return nil, err
	}
	return extractTasks(e, sourcePaths)
}

func extractTasks(e *task.Executor, sourcePaths []string) (*ParsedTaskfile, error) {
	tasks, err := e.GetTaskList(task.FilterOutInternal, task.FilterOutNoDesc)
	if err != nil {
		return nil, fmt.Errorf("getting task list: %w", err)
	}

	var infos []TaskInfo
	for _, t := range tasks {
		info := TaskInfo{
			Name: sanitizeToolName(t.Task),
			Task: t.Task,
			Desc: t.Desc,
		}

		// Extract required vars from the `requires:` block — these become
		// tool parameters since the task explicitly declares them as inputs.
		if t.Requires != nil {
			for _, v := range t.Requires.Vars {
				info.Vars = append(info.Vars, v.Name)
			}
		}
		sort.Strings(info.Vars)

		infos = append(infos, info)
	}

	kept, collisions := dedupeToolNames(infos)
	return &ParsedTaskfile{
		Tasks:         kept,
		TaskfilePaths: sourcePaths,
		Collisions:    collisions,
	}, nil
}

func collectTaskfilePaths(e *task.Executor) ([]string, error) {
	graph, err := readTaskfileGraph(e)
	if err != nil {
		return nil, err
	}

	adjacency, err := graph.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("building taskfile adjacency map: %w", err)
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0, len(adjacency))

	addPath := func(path string) {
		normalized, ok := normalizeTaskfilePath(e.Dir, path)
		if !ok {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		paths = append(paths, normalized)
	}

	for uri := range adjacency {
		vertex, err := graph.Vertex(uri)
		if err != nil {
			return nil, fmt.Errorf("getting taskfile graph vertex %q: %w", uri, err)
		}
		if vertex == nil || vertex.Taskfile == nil {
			continue
		}
		addPath(vertex.Taskfile.Location)
	}

	sort.Strings(paths)
	return paths, nil
}

func readTaskfileGraph(e *task.Executor) (*taskast.TaskfileGraph, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()

	node, err := taskfilepkg.NewRootNode(
		e.Entrypoint,
		e.Dir,
		e.Insecure,
		e.Timeout,
		taskfilepkg.WithCACert(e.CACert),
		taskfilepkg.WithCert(e.Cert),
		taskfilepkg.WithCertKey(e.CertKey),
	)
	if err != nil {
		return nil, fmt.Errorf("building taskfile root node: %w", err)
	}

	reader := taskfilepkg.NewReader(
		taskfilepkg.WithInsecure(e.Insecure),
		taskfilepkg.WithDownload(e.Download),
		taskfilepkg.WithOffline(e.Offline),
		taskfilepkg.WithTrustedHosts(e.TrustedHosts),
		taskfilepkg.WithTempDir(e.TempDir.Remote),
		taskfilepkg.WithCacheExpiryDuration(e.CacheExpiryDuration),
		taskfilepkg.WithReaderCACert(e.CACert),
		taskfilepkg.WithReaderCert(e.Cert),
		taskfilepkg.WithReaderCertKey(e.CertKey),
	)

	graph, err := reader.Read(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("reading taskfile graph: %w", err)
	}
	return graph, nil
}

// normalizeTaskfilePath joins a relative path onto baseDir and cleans it. It is
// pure: callers pass absolute paths (main resolves --dir once at startup).
func normalizeTaskfilePath(baseDir, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Clean(path), true
}

// dedupeToolNames keeps the first task for each tool name and reports the rest.
func dedupeToolNames(infos []TaskInfo) ([]TaskInfo, []ToolNameCollision) {
	var (
		kept    []TaskInfo
		dropped []ToolNameCollision
		owner   = make(map[string]string, len(infos))
	)
	for _, info := range infos {
		if first, taken := owner[info.Name]; taken {
			dropped = append(dropped, ToolNameCollision{Tool: info.Name, Kept: first, Dropped: info.Task})
			continue
		}
		owner[info.Name] = info.Task
		kept = append(kept, info)
	}
	return kept, dropped
}

// sanitizeToolName replaces characters invalid in MCP tool names.
func sanitizeToolName(name string) string {
	return strings.ReplaceAll(name, ":", "_")
}
