package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if colorEnabled(true) {
		t.Error("--no-color should disable color")
	}

	t.Setenv("NO_COLOR", "1")
	if colorEnabled(false) {
		t.Error("NO_COLOR env should disable color")
	}

	// With NO_COLOR unset and stdout not a terminal (test pipe), color is off.
	t.Setenv("NO_COLOR", "")
	if colorEnabled(false) {
		t.Error("non-terminal stdout should disable color")
	}
}

func TestRoot_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(`{"level":"info","msg":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--no-color", "--no-time", path})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestRoot_ReadsStdin(t *testing.T) {
	cmd := root()
	cmd.SetIn(strings.NewReader(`{"level":"info","msg":"fromstdin"}` + "\n"))
	cmd.SetArgs([]string{"--no-color", "--no-time"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestRoot_InvalidLevel(t *testing.T) {
	cmd := root()
	cmd.SetArgs([]string{"--level", "bogus"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid --level") {
		t.Fatalf("expected invalid level error, got %v", err)
	}
}

func TestRoot_FollowRequiresFile(t *testing.T) {
	cmd := root()
	cmd.SetArgs([]string{"--follow"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "--follow requires a file") {
		t.Fatalf("expected follow error, got %v", err)
	}
}

func TestRoot_MissingFile(t *testing.T) {
	cmd := root()
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), "nope.log")})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestRoot_ValidLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(`{"level":"error","msg":"boom"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := root()
	cmd.SetArgs([]string{"--level", "warn", "--no-time", path})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestRoot_TraceIDFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	lines := `{"level":"info","msg":"keep","trace_id":"abc123"}` + "\n" +
		`{"level":"info","msg":"drop","trace_id":"def456"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := root()
	cmd.SetArgs([]string{"--trace-id", "ABC123", "--no-color", "--no-time", path})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestRoot_FollowCanceled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before run; Follow returns nil on ctx error

	cmd := root()
	cmd.SetArgs([]string{"--follow", path})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("canceled follow should exit cleanly, got %v", err)
	}
}
