package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev" // set at build time: -ldflags "-X main.version=..."

// defaultTaskTimeout bounds a single tool call unless --task-timeout overrides it.
const defaultTaskTimeout = 5 * time.Minute

// Server wraps the MCP server and manages dynamic tool registration from Taskfile tasks.
type Server struct {
	mcp    *mcp.Server
	logger *slog.Logger
	dir    string
	// taskTimeout bounds one tool call (task run); 0 means no bound.
	taskTimeout time.Duration

	mu       sync.Mutex
	tasks    []TaskInfo
	sources  []string
	toolKeys []string // registered tool names (for removal on reload)
}

// NewServer creates a new MCP server configured for taskfile bridging.
func NewServer(dir string, logger *slog.Logger) *Server {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-taskfile",
		Version: version,
	}, nil)

	return &Server{
		mcp:         mcpServer,
		logger:      logger,
		dir:         dir,
		taskTimeout: defaultTaskTimeout,
	}
}

// LoadTools parses the Taskfile and registers all public tasks as MCP tools.
func (s *Server) LoadTools() error {
	parsed, err := ParseTaskfile(s.dir)
	if err != nil {
		if errors.Is(err, ErrNoTaskfile) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.clearToolsLocked()
			return err
		}
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.replaceToolsLocked(parsed.Tasks)
	s.sources = append([]string(nil), parsed.TaskfilePaths...)
	s.logCollisions(parsed.Collisions)

	s.logger.Info("tools loaded", "count", len(parsed.Tasks), "dir", s.dir)
	return nil
}

// ReloadTools re-parses the Taskfile and updates tool registrations.
// Returns true if the tool set changed.
func (s *Server) ReloadTools() (bool, error) {
	parsed, err := ParseTaskfile(s.dir)
	if err != nil {
		if errors.Is(err, ErrNoTaskfile) {
			s.mu.Lock()
			defer s.mu.Unlock()

			changed := len(s.tasks) > 0
			s.clearToolsLocked()
			s.logger.Info("taskfile disappeared, tools cleared", "dir", s.dir)
			return changed, nil
		}
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := tasksChanged(s.tasks, parsed.Tasks)
	s.sources = append([]string(nil), parsed.TaskfilePaths...)
	if changed {
		s.logCollisions(parsed.Collisions)
	}

	if !changed {
		return false, nil
	}

	s.replaceToolsLocked(parsed.Tasks)

	s.logger.Info("tools reloaded", "count", len(parsed.Tasks), "dir", s.dir)
	return true, nil
}

// SourceFiles returns the current Taskfile source paths that should be watched
// for reloads.
func (s *Server) SourceFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sources...)
}

func (s *Server) registerTool(t TaskInfo) {
	props := map[string]any{
		"cli_args": map[string]any{
			"type":        "string",
			"description": "Additional arguments passed to the task (maps to {{.CLI_ARGS}})",
		},
	}

	for _, v := range t.Vars {
		props[v] = map[string]any{
			"type":        "string",
			"description": "Task variable: " + v,
		}
	}

	tool := &mcp.Tool{
		Name:        t.Name,
		Description: t.Desc,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": props,
		},
	}

	taskName := t.Task
	dir := s.dir
	timeout := s.taskTimeout
	logger := s.logger
	s.logger.Debug("registering taskfile tool", "task", t.Name, "vars", t.Vars)
	s.mcp.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]string
		if req.Params.Arguments != nil {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				logger.Warn("task arguments decode failed", "task", taskName, "err", err)
				return nil, err
			}
		}

		cliArgs := args["cli_args"]
		delete(args, "cli_args")
		logger.Debug("task execution start", "task", taskName, "dir", dir, "cli_args", cliArgs, "vars", args)

		result, err := RunTask(ctx, dir, taskName, cliArgs, args, timeout, logger)
		if err != nil {
			logger.Warn("task execution failed", "task", taskName, "dir", dir, "err", err)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: err.Error()},
				},
				IsError: true,
			}, nil
		}
		logger.Debug("task execution completed", "task", taskName, "dir", dir)
		return result, nil
	})

	s.toolKeys = append(s.toolKeys, t.Name)
}

func (s *Server) logCollisions(collisions []ToolNameCollision) {
	for _, c := range collisions {
		s.logger.Warn("tool name collision, task not exposed", "tool", c.Tool, "kept", c.Kept, "dropped", c.Dropped)
	}
}

// Run starts the stdio MCP transport. Blocks until the client disconnects.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

func tasksChanged(old, new_ []TaskInfo) bool {
	if len(old) != len(new_) {
		return true
	}
	oldMap := make(map[string]TaskInfo, len(old))
	for _, t := range old {
		oldMap[t.Name] = normalizedTaskInfo(t)
	}
	for _, t := range new_ {
		prev, ok := oldMap[t.Name]
		if !ok || !taskInfoEqual(prev, normalizedTaskInfo(t)) {
			return true
		}
	}
	return false
}

func (s *Server) replaceToolsLocked(tasks []TaskInfo) {
	s.clearToolsLocked()
	s.tasks = append([]TaskInfo(nil), tasks...)
	for _, t := range tasks {
		s.registerTool(t)
	}
}

func (s *Server) clearToolsLocked() {
	if len(s.toolKeys) > 0 {
		s.mcp.RemoveTools(s.toolKeys...)
		s.toolKeys = nil
	}
	s.tasks = nil
	s.sources = nil
}

func normalizedTaskInfo(t TaskInfo) TaskInfo {
	normalized := TaskInfo{
		Name: t.Name,
		Task: t.Task,
		Desc: t.Desc,
		Vars: append([]string(nil), t.Vars...),
	}
	sort.Strings(normalized.Vars)
	return normalized
}

func taskInfoEqual(a, b TaskInfo) bool {
	if a.Name != b.Name || a.Task != b.Task || a.Desc != b.Desc || len(a.Vars) != len(b.Vars) {
		return false
	}
	for i := range a.Vars {
		if a.Vars[i] != b.Vars[i] {
			return false
		}
	}
	return true
}
