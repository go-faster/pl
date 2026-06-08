package pl

import (
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestFormat_Production(t *testing.T) {
	// A line as produced by zap.NewProductionConfig.
	line := `{"level":"info","ts":1620000000.123,"logger":"metrics","caller":"app/app.go:42","msg":"Starting","attempt":3,"name":"svc"}`
	f := &Formatter{Color: false}
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected line to be printed")
	}
	for _, want := range []string{"I ", "metrics", "Starting", "attempt=3", "name=svc", "(app/app.go:42)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestFormat_NoTime(t *testing.T) {
	line := `{"level":"info","ts":1620000000.123,"msg":"hi"}`
	f := &Formatter{NoTime: true}
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	if strings.Contains(out, ":00") || !strings.HasPrefix(out, "I ") {
		t.Fatalf("timestamp not omitted: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("message missing: %q", out)
	}
}

func TestFormat_DefaultLevelLabels(t *testing.T) {
	f := &Formatter{NoTime: true}
	cases := map[string]string{
		"debug":  "D",
		"info":   "I",
		"warn":   "W",
		"error":  "E",
		"dpanic": "C",
		"panic":  "C",
		"fatal":  "C",
	}
	for lvl, want := range cases {
		out, ok := f.Format([]byte(`{"level":"` + lvl + `","msg":"m"}`))
		if !ok {
			t.Fatalf("%s: expected output", lvl)
		}
		// With NoTime the label is the first token.
		if got, _, _ := strings.Cut(out, " "); got != want {
			t.Errorf("level %s: label = %q, want %q (out=%q)", lvl, got, want, out)
		}
	}
}

func TestFormat_CustomLevelStyle(t *testing.T) {
	f := &Formatter{
		NoTime: true,
		LevelStyles: map[zapcore.Level]LevelStyle{
			zapcore.WarnLevel: {Label: "WARN"},
		},
	}
	// Overridden level uses the custom label.
	out, _ := f.Format([]byte(`{"level":"warn","msg":"m"}`))
	if !strings.HasPrefix(out, "WARN ") {
		t.Fatalf("custom warn label not used: %q", out)
	}
	// Non-overridden level still uses the single-letter default.
	out, _ = f.Format([]byte(`{"level":"info","msg":"m"}`))
	if !strings.HasPrefix(out, "I ") {
		t.Fatalf("default info label not used: %q", out)
	}
}

func TestFormat_PassThroughNonJSON(t *testing.T) {
	f := &Formatter{}
	const line = "plain text line"
	out, ok := f.Format([]byte(line))
	if !ok || out != line {
		t.Fatalf("non-JSON line not passed through: %q %v", out, ok)
	}
}

func TestFormat_MalformedJSON(t *testing.T) {
	f := &Formatter{}
	const line = `{"level":"info" broken`
	out, ok := f.Format([]byte(line))
	if !ok || out != line {
		t.Fatalf("malformed JSON not passed through: %q %v", out, ok)
	}
}

func TestFormat_LevelFilter(t *testing.T) {
	f := &Formatter{}
	f.SetMinLevel(zapcore.WarnLevel)

	if _, ok := f.Format([]byte(`{"level":"info","msg":"hi"}`)); ok {
		t.Error("info line should be dropped below warn threshold")
	}
	if _, ok := f.Format([]byte(`{"level":"error","msg":"boom"}`)); !ok {
		t.Error("error line should pass warn threshold")
	}
}

func TestFormat_StringTimestamp(t *testing.T) {
	f := &Formatter{}
	out, ok := f.Format([]byte(`{"level":"warn","ts":"2021-05-03T01:02:03.456Z","msg":"x"}`))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, "W ") || !strings.Contains(out, "x") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFormat_QuotesValuesWithSpaces(t *testing.T) {
	f := &Formatter{}
	out, _ := f.Format([]byte(`{"msg":"m","note":"has space"}`))
	if !strings.Contains(out, `note="has space"`) {
		t.Fatalf("value with space not quoted: %q", out)
	}
}

func TestFormat_Stacktrace(t *testing.T) {
	f := &Formatter{}
	out, _ := f.Format([]byte(`{"level":"error","msg":"boom","stacktrace":"line1\nline2"}`))
	if !strings.Contains(out, "\n\tline1") || !strings.Contains(out, "\n\tline2") {
		t.Fatalf("stacktrace not rendered on indented lines: %q", out)
	}
}
