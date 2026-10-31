package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool starts srv on an in-memory transport and calls one tool by name.
func callTool(t *testing.T, srv *Server, name string) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(%q): %v", name, err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// Regression: a task whose own name contains "_" must run under its real name.
// sanitize maps ":" to "_", so the reverse mapping cannot be a string replace.
func TestCallToolRunsTaskWithUnderscoreInName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), `version: "3"
tasks:
  build_all:
    desc: "Underscore in the task name"
    cmds:
      - echo underscore-ok
  ns:inner_task:
    desc: "Namespace plus underscore"
    cmds:
      - echo ns-underscore-ok
`)
	srv := NewServer(dir, testLogger())
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ tool, want string }{
		{"build_all", "underscore-ok"},
		{"ns_inner_task", "ns-underscore-ok"},
	}
	for _, tc := range cases {
		res := callTool(t, srv, tc.tool)
		if res.IsError {
			t.Errorf("tool %q returned an error: %s", tc.tool, resultText(res))
			continue
		}
		if !strings.Contains(resultText(res), tc.want) {
			t.Errorf("tool %q output = %q, want it to contain %q", tc.tool, resultText(res), tc.want)
		}
	}
}

// Regression: a task that never exits must not block the tool call forever.
// Executor.Timeout only bounds Taskfile loading, so the run needs its own bound.
func TestCallToolTimesOutLongRunningTask(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), `version: "3"
tasks:
  slow:
    desc: "Never finishes in time"
    cmds:
      - sleep 30
`)
	srv := NewServer(dir, testLogger())
	srv.taskTimeout = 300 * time.Millisecond
	if err := srv.LoadTools(); err != nil {
		t.Fatal(err)
	}

	done := make(chan *mcp.CallToolResult, 1)
	go func() { done <- callTool(t, srv, "slow") }()

	select {
	case res := <-done:
		if !res.IsError {
			t.Fatalf("expected an error result, got %q", resultText(res))
		}
		if !strings.Contains(resultText(res), "timed out after 300ms") {
			t.Errorf("error text = %q, want it to name the timeout", resultText(res))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("tool call still running after 10s; task timeout not enforced")
	}
}

// Regression: "a:b" and "a_b" both sanitize to "a_b". The first task in
// task-list order keeps the name (go-task lists root tasks before namespaced
// ones, so the task literally named "a_b" wins); the other is reported.
func TestParseTaskfileReportsToolNameCollisions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), `version: "3"
tasks:
  a:b:
    desc: "colon"
    cmds: [echo colon]
  a_b:
    desc: "underscore"
    cmds: [echo underscore]
  other:
    desc: "unrelated"
    cmds: [echo other]
`)
	parsed, err := ParseTaskfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ti := range parsed.Tasks {
		names = append(names, ti.Name)
	}
	if got, want := strings.Join(names, ","), "a_b,other"; got != want {
		t.Errorf("tool names = %s, want %s", got, want)
	}
	want := []ToolNameCollision{{Tool: "a_b", Kept: "a_b", Dropped: "a:b"}}
	if len(parsed.Collisions) != 1 || parsed.Collisions[0] != want[0] {
		t.Errorf("collisions = %+v, want %+v", parsed.Collisions, want)
	}
}

func TestDedupeToolNames(t *testing.T) {
	in := []TaskInfo{
		{Name: "x", Task: "x"},
		{Name: "a_b", Task: "a:b"},
		{Name: "a_b", Task: "a_b"},
		{Name: "a_b", Task: "a__b"},
	}
	kept, dropped := dedupeToolNames(in)
	if len(kept) != 2 || kept[0].Task != "x" || kept[1].Task != "a:b" {
		t.Errorf("kept = %+v", kept)
	}
	wantDropped := []ToolNameCollision{
		{Tool: "a_b", Kept: "a:b", Dropped: "a_b"},
		{Tool: "a_b", Kept: "a:b", Dropped: "a__b"},
	}
	if len(dropped) != len(wantDropped) {
		t.Fatalf("dropped = %+v, want %+v", dropped, wantDropped)
	}
	for i := range wantDropped {
		if dropped[i] != wantDropped[i] {
			t.Errorf("dropped[%d] = %+v, want %+v", i, dropped[i], wantDropped[i])
		}
	}
	if k, d := dedupeToolNames(nil); k != nil || d != nil {
		t.Errorf("dedupeToolNames(nil) = %v, %v", k, d)
	}
}
