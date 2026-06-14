package pl

import (
	"strings"
	"testing"
	"time"

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

func TestFormat_ErrorUnescapesQuotes(t *testing.T) {
	f := &Formatter{NoTime: true}
	line := `{"level":"error","msg":"boom","error":"insert: null value in column \"attributes\" violates not-null"}`
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, `insert: null value in column "attributes" violates not-null`) {
		t.Fatalf("error not rendered bare: %q", out)
	}
	// Rendered without an explicit "error=" key.
	if strings.Contains(out, "error=") {
		t.Fatalf("error should not be a key=value field: %q", out)
	}
	if strings.Contains(out, `\"`) {
		t.Fatalf("error still contains escaped quotes: %q", out)
	}
}

func TestFormat_ErrorVerboseMultiline(t *testing.T) {
	f := &Formatter{NoTime: true}
	line := `{"level":"error","msg":"boom","errorVerbose":"insert:\n    pkg.SaveFile\n  - null value"}`
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	// Rendered across indented lines, not inline as an escaped value.
	for _, want := range []string{"\n\tinsert:", "\n\t    pkg.SaveFile", "\n\t  - null value"} {
		if !strings.Contains(out, want) {
			t.Fatalf("errorVerbose line %q missing: %q", want, out)
		}
	}
	if strings.Contains(out, `errorVerbose=`) {
		t.Fatalf("errorVerbose should not be an inline field: %q", out)
	}
}

func TestFormat_StacktraceFrames(t *testing.T) {
	f := &Formatter{NoTime: true}
	// The field value carries JSON-escaped newlines and tabs, as zap emits.
	line := `{"level":"error","msg":"boom","stacktrace":` +
		`"main.run\n\t/app/main.go:42\nruntime.main\n\t/usr/local/go/src/runtime/proc.go:267"}`
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	// The original two-line layout (function then indented location) is kept.
	for _, want := range []string{
		"\tmain.run",
		"\t\t/app/main.go:42",
		"\truntime.main",
		"\t\t/usr/local/go/src/runtime/proc.go:267",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("line %q missing from %q", want, out)
		}
	}
}

func TestFormat_StacktraceStyles(t *testing.T) {
	f := &Formatter{Color: true, NoTime: true}
	line := `{"level":"error","msg":"boom","stacktrace":"main.run\n\t/app/main.go:42"}`
	out, _ := f.Format([]byte(line))
	// Function name is blue; the location is dimmed and italicized.
	if !strings.Contains(out, colBlue+"\tmain.run") {
		t.Fatalf("function name not blue: %q", out)
	}
	if !strings.Contains(out, colDim+colItalic+"\t\t/app/main.go:42") {
		t.Fatalf("location not dim+italic: %q", out)
	}
}

func TestFormat_ErrorVerboseFrames(t *testing.T) {
	f := &Formatter{NoTime: true}
	line := `{"level":"error","msg":"boom","errorVerbose":` +
		`"insert: bad value\n    pkg.SaveFile\n\t/app/pkg/save.go:12\n  - root cause"}`
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	// Every line keeps its original indentation, none are merged.
	for _, want := range []string{
		"\tinsert: bad value",
		"\t    pkg.SaveFile",
		"\t\t/app/pkg/save.go:12",
		"\t  - root cause",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("line %q missing from %q", want, out)
		}
	}
}

func TestFormat_ErrorVerboseWarmPalette(t *testing.T) {
	f := &Formatter{Color: true, NoTime: true}
	line := `{"level":"error","msg":"boom","errorVerbose":` +
		`"bad value\n    pkg.SaveFile\n\t/app/pkg/save.go:12"}`
	out, _ := f.Format([]byte(line))
	// Error text is red and frames are yellow, distinct from the blue/dim
	// runtime stacktrace, so the two blocks are not confused.
	if !strings.Contains(out, colRed+"\tbad value") {
		t.Fatalf("error text not red: %q", out)
	}
	if !strings.Contains(out, colYellow+"\t    pkg.SaveFile") {
		t.Fatalf("errorVerbose function not yellow: %q", out)
	}
	if strings.Contains(out, colBlue) {
		t.Fatalf("errorVerbose should not use the stacktrace's blue: %q", out)
	}
}

func TestIsStackLocation(t *testing.T) {
	yes := []string{
		"/usr/local/go/src/runtime/proc.go:267",
		"\t/app/main.go:42",
		"        /app/pkg/save.go:12 +0x2a",
		"main.go:1",
	}
	for _, s := range yes {
		if !isStackLocation(s) {
			t.Errorf("expected %q to be a location", s)
		}
	}
	no := []string{
		"",
		"main.run",
		"github.com/foo/bar.(*T).Method",
		"insert: bad value",
		"  - root cause",
		":42",
	}
	for _, s := range no {
		if isStackLocation(s) {
			t.Errorf("expected %q not to be a location", s)
		}
	}
}

func TestFormat_ErrorVerboseDistinctColor(t *testing.T) {
	f := &Formatter{Color: true, NoTime: true}
	line := `{"level":"error","msg":"boom","errorVerbose":"e1\ne2","stacktrace":"s1\ns2"}`
	out, _ := f.Format([]byte(line))
	if !strings.Contains(out, colRed+"\te1") {
		t.Fatalf("errorVerbose not painted in its own color: %q", out)
	}
	if !strings.Contains(out, colDim+"\ts1") {
		t.Fatalf("stacktrace color changed: %q", out)
	}
}

func TestRenderError(t *testing.T) {
	if renderError(nil) != "" {
		t.Error("empty raw should render empty")
	}
	if got := renderError([]byte(`123`)); got != "123" {
		t.Errorf("numeric value: %q", got)
	}
	if got := renderError([]byte(`"plain"`)); got != "plain" {
		t.Errorf("plain string should be unquoted: %q", got)
	}
	if got := renderError([]byte(`"a \"b\" c"`)); got != `a "b" c` {
		t.Errorf("error should be bare with interior quotes intact: %q", got)
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

func TestFormat_Location(t *testing.T) {
	// 1620000000 is 2021-05-03 00:00:00 UTC.
	f := &Formatter{TimeFormat: "2006-01-02 15:04:05", Location: time.UTC}
	out, ok := f.Format([]byte(`{"level":"info","ts":1620000000,"msg":"m"}`))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, "2021-05-03 00:00:00") {
		t.Fatalf("location not applied: %q", out)
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
