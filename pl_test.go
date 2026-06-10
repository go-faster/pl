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

func TestFormat_Color(t *testing.T) {
	f := &Formatter{Color: true, NoTime: true}
	out, ok := f.Format([]byte(`{"level":"info","logger":"svc","msg":"hi","k":"v"}`))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, colReset) || !strings.Contains(out, colGreen) {
		t.Fatalf("expected ANSI colors in output: %q", out)
	}
}

func TestFormat_CustomTimeFormat(t *testing.T) {
	f := &Formatter{TimeFormat: "2006-01-02"}
	out, ok := f.Format([]byte(`{"level":"info","ts":1620000000,"msg":"m"}`))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, "2021-05-03") {
		t.Fatalf("custom time format not applied: %q", out)
	}
}

func TestFormat_EmptyAndBlankLines(t *testing.T) {
	f := &Formatter{}
	if _, ok := f.Format([]byte("   \n")); ok {
		t.Error("blank line should be dropped")
	}
	if _, ok := f.Format(nil); ok {
		t.Error("empty line should be dropped")
	}
}

func TestLevelStyle_UnknownLevel(t *testing.T) {
	f := &Formatter{}
	// A level absent from both custom and default maps falls back to its
	// uppercased name with no color.
	s := f.levelStyle(zapcore.Level(99))
	if s.Color != "" {
		t.Errorf("unknown level should have no color, got %q", s.Color)
	}
	if s.Label != strings.ToUpper(zapcore.Level(99).String()) {
		t.Errorf("unexpected label for unknown level: %q", s.Label)
	}
}

func TestPaint(t *testing.T) {
	on := &Formatter{Color: true}
	if got := on.paint(colRed, "x"); got != colRed+"x"+colReset {
		t.Errorf("colored paint = %q", got)
	}
	if got := on.paint("", "x"); got != "x" {
		t.Errorf("empty color should not wrap: %q", got)
	}
	off := &Formatter{Color: false}
	if got := off.paint(colRed, "x"); got != "x" {
		t.Errorf("color disabled should not wrap: %q", got)
	}
}

func TestParseLevel(t *testing.T) {
	if _, ok := parseLevel(nil); ok {
		t.Error("empty raw should fail")
	}
	if _, ok := parseLevel([]byte(`"bogus"`)); ok {
		t.Error("unknown level name should fail")
	}
	if l, ok := parseLevel([]byte(`"warn"`)); !ok || l != zapcore.WarnLevel {
		t.Errorf("warn = %v %v", l, ok)
	}
}

func TestParseTime(t *testing.T) {
	if _, ok := parseTime(nil); ok {
		t.Error("empty raw should fail")
	}
	if _, ok := parseTime([]byte(`"not a time"`)); ok {
		t.Error("unparseable string should fail")
	}
	if _, ok := parseTime([]byte(`not-a-number`)); ok {
		t.Error("non-numeric should fail")
	}
	if _, ok := parseTime([]byte(`"2021-05-03 01:02:03"`)); !ok {
		t.Error("plain datetime layout should parse")
	}
	if _, ok := parseTime([]byte(`"2021-05-03T01:02:03Z"`)); !ok {
		t.Error("RFC3339 should parse")
	}
	if _, ok := parseTime([]byte(`1620000000`)); !ok {
		t.Error("integer epoch should parse")
	}
}

func TestAsString(t *testing.T) {
	if asString(nil) != "" {
		t.Error("empty raw should be empty string")
	}
	if got := asString([]byte(`42`)); got != "42" {
		t.Errorf("non-string raw should pass through: %q", got)
	}
	if got := asString([]byte(`"hi"`)); got != "hi" {
		t.Errorf("quoted string should unquote: %q", got)
	}
}

func TestRenderValue(t *testing.T) {
	if renderValue(nil) != "" {
		t.Error("empty raw should render empty")
	}
	if got := renderValue([]byte(`123`)); got != "123" {
		t.Errorf("numeric value: %q", got)
	}
	if got := renderValue([]byte(`"plain"`)); got != "plain" {
		t.Errorf("plain string should be unquoted: %q", got)
	}
	if got := renderValue([]byte(`"a b"`)); got != `"a b"` {
		t.Errorf("string with space should be quoted: %q", got)
	}
}
