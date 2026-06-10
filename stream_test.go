package pl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcess(t *testing.T) {
	in := strings.Join([]string{
		`{"level":"info","msg":"first"}`,
		`plain passthrough`,
		`{"level":"info","msg":"second"}`,
	}, "\n") + "\n"

	f := &Formatter{NoTime: true}
	var out bytes.Buffer
	if err := f.Process(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("Process: %v", err)
	}
	got := out.String()
	for _, want := range []string{"first", "plain passthrough", "second"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
}

func TestProcess_DropsFiltered(t *testing.T) {
	in := `{"level":"info","msg":"dropme"}` + "\n" + `{"level":"error","msg":"keepme"}` + "\n"

	f := &Formatter{NoTime: true}
	f.SetMinLevel(2) // warn
	var out bytes.Buffer
	if err := f.Process(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("Process: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "dropme") {
		t.Errorf("filtered line leaked: %q", got)
	}
	if !strings.Contains(got, "keepme") {
		t.Errorf("kept line missing: %q", got)
	}
}

func TestProcess_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := &Formatter{NoTime: true}
	var out bytes.Buffer
	err := f.Process(ctx, strings.NewReader(`{"level":"info","msg":"x"}`+"\n"), &out)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestProcess_ScanError(t *testing.T) {
	f := &Formatter{NoTime: true}
	var out bytes.Buffer
	if err := f.Process(context.Background(), iotest{}, &out); err == nil {
		t.Fatal("expected scan error")
	}
}

// iotest is a reader that always errors.
type iotest struct{}

func (iotest) Read([]byte) (int, error) { return 0, errors.New("fake read error") }

func TestFollow_TailsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(`{"level":"info","msg":"one"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := &Formatter{NoTime: true}
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- f.Follow(ctx, path, &out) }()

	// Append a new line after Follow starts.
	waitFor(t, func() bool { return strings.Contains(out.String(), "one") })
	appendLine(t, path, `{"level":"warn","msg":"two"}`)
	waitFor(t, func() bool { return strings.Contains(out.String(), "two") })

	cancel()
	if err := <-done; err != nil && ctx.Err() == nil {
		t.Fatalf("Follow: %v", err)
	}
}

func TestFollow_HandlesTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(`{"level":"info","msg":"before"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := &Formatter{NoTime: true}
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- f.Follow(ctx, path, &out) }()

	waitFor(t, func() bool { return strings.Contains(out.String(), "before") })

	// Truncate and rewrite to trigger the rotation path.
	if err := os.WriteFile(path, []byte(`{"level":"info","msg":"after"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(out.String(), "after") })

	cancel()
	if err := <-done; err != nil && ctx.Err() == nil {
		t.Fatalf("Follow: %v", err)
	}
}

func TestFollow_OpenError(t *testing.T) {
	f := &Formatter{}
	err := f.Follow(context.Background(), filepath.Join(t.TempDir(), "nope.log"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected open error for missing file")
	}
}

func TestFollow_CanceledImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := &Formatter{}
	if err := f.Follow(ctx, path, &bytes.Buffer{}); err == nil {
		t.Fatal("expected context error")
	}
}

// helpers

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// syncBuffer is a concurrency-safe bytes.Buffer for use across goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
