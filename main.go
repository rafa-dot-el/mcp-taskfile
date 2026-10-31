package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	var (
		dir         string
		showVersion bool
		taskTimeout time.Duration
	)

	flag.StringVar(&dir, "dir", "", "Working directory containing Taskfile.yml (default: CWD)")
	flag.DurationVar(&taskTimeout, "task-timeout", defaultTaskTimeout, "Maximum run time of one task (0 disables the bound)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println("mcp-taskfile", version)
		os.Exit(0)
	}

	// Resolve once, here: everything below works on absolute paths only.
	if dir == "" {
		dir = "."
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve --dir: %v\n", err)
		os.Exit(1)
	}

	logger, level, err := newComponentLogger("taskfile")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-taskfile: invalid MCP_TASKFILE_LOG_LEVEL: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	logger.Info("logger configured", "level", renderLogLevel(level))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := NewServer(dir, logger)
	srv.taskTimeout = taskTimeout

	if err := srv.LoadTools(); err != nil {
		logger.Warn("no taskfile loaded, serving 0 tools", "dir", dir, "err", err)
	}

	w := NewWatcher(srv, dir, logger)
	go w.Run(ctx)

	logger.Info("mcp-taskfile running", "dir", dir)

	if err := srv.Run(ctx); err != nil {
		logCritical(logger, "server error", "err", err)
		os.Exit(1)
	}
}
