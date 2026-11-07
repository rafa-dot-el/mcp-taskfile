package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	task "github.com/go-task/task/v3"
	"github.com/go-task/task/v3/taskfile/ast"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunTask executes a single task by its Taskfile name (not the sanitized tool name), capturing its output.
// A timeout > 0 bounds the run; on expiry the task's processes are killed via the context.
func RunTask(ctx context.Context, dir, taskName, cliArgs string, vars map[string]string, timeout time.Duration, logger *slog.Logger) (*mcp.CallToolResult, error) {
	var stdout, stderr bytes.Buffer

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	e := &task.Executor{
		Dir:     dir,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Stdin:   io.NopCloser(strings.NewReader("")),
		Silent:  true,
		Timeout: taskfileLoadTimeout, // Taskfile loading only; the run is bounded by ctx above
	}

	if err := e.Setup(); err != nil {
		return nil, fmt.Errorf("task executor setup: %w", err)
	}

	call := &task.Call{
		Task: taskName,
	}

	// Build vars map: CLI_ARGS is a special variable the task template expands.
	varMap := ast.NewVars()
	if cliArgs != "" {
		varMap.Set("CLI_ARGS", ast.Var{Value: cliArgs})
	}
	for k, v := range vars {
		varMap.Set(k, ast.Var{Value: v})
	}
	if varMap.Len() > 0 {
		call.Vars = varMap
	}

	logger.Debug("executing task", "task", taskName, "dir", dir, "cli_args", cliArgs)

	if err := e.RunTask(ctx, call); err != nil {
		output := combineOutput(stdout.Bytes(), stderr.Bytes())
		errMsg := fmt.Sprintf("task %q failed: %v\n\n%s", taskName, err, output)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			errMsg = fmt.Sprintf("task %q timed out after %s\n\n%s", taskName, timeout, output)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: errMsg},
			},
			IsError: true,
		}, nil
	}

	output := combineOutput(stdout.Bytes(), stderr.Bytes())
	if output == "" {
		output = fmt.Sprintf("task %q completed successfully (no output)", taskName)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: output},
		},
	}, nil
}

func combineOutput(stdout, stderr []byte) string {
	var parts []string
	if len(stdout) > 0 {
		parts = append(parts, string(stdout))
	}
	if len(stderr) > 0 {
		parts = append(parts, "[stderr]\n"+string(stderr))
	}
	return strings.Join(parts, "\n")
}
