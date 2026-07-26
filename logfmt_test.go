package pl

import (
	"strings"
	"testing"
	"time"

	"github.com/go-faster/jx"
)

func TestFormat_Logfmt(t *testing.T) {
	for _, tt := range []struct {
		name string
		line string
		want []string
		skip []string
	}{
		{
			name: "grafana",
			line: `logger=tracing t=2023-12-19T06:57:42.301691427Z level=error msg="OpenTelemetry handler returned an error" err="traces export: context deadline exceeded"`,
			want: []string{"06:57:42.301", "E ", "tracing", "OpenTelemetry handler returned an error", "traces export: context deadline exceeded"},
			// The error is rendered bare, not as a key=value field.
			skip: []string{"err="},
		},
		{
			name: "grafana fields",
			line: `logger=tsdb.loki endpoint=callResource t=2026-02-21T21:45:58.39217982Z level=info msg="Response received from loki" status=ok statusCode=200 duration=10.045939ms`,
			want: []string{"I ", "tsdb.loki", "Response received from loki", "endpoint=callResource", "status=ok", "statusCode=200", "duration=10.045939ms"},
		},
		{
			name: "logrus",
			line: `time="2015-03-26T01:27:38-04:00" level=warning msg="The group's number increased tremendously!" number=122 omg=true`,
			want: []string{"W ", "The group's number increased tremendously!", "number=122", "omg=true"},
		},
		{
			name: "empty level",
			line: `time="2015-03-26T01:27:38-04:00" level= msg=kek`,
			want: []string{"kek"},
			skip: []string{"level"},
		},
		{
			name: "unknown level kept as field",
			line: `t=2023-12-19T06:57:42Z level=audit msg=hi`,
			want: []string{"severity_text=audit", "hi"},
		},
		{
			name: "quoted value with spaces",
			line: `level=debug time="2015-03-26T01:27:38-04:00" foo=bar baz="hello kitty" flag`,
			want: []string{"D ", "foo=bar", `baz="hello kitty"`, "flag=true"},
		},
		{
			name: "epoch millis",
			line: `ts=1620000000123 level=info msg=hi`,
			want: []string{"I ", "hi", "00:00:00.123"},
		},
		{
			name: "trace correlation",
			line: `t=2023-12-19T06:57:42Z level=info msg=hi traceID=91817a32ebeb6433d6602538b6478642 spanID=6433d66025386478`,
			want: []string{"trace_id=91817a32ebeb6433d6602538b6478642", "span_id=6433d66025386478"},
		},
		{
			name: "zero trace ids dropped",
			line: `t=2023-12-19T06:57:42Z level=info msg=hi traceID=00000000000000000000000000000000`,
			skip: []string{"trace_id"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formatter{TimeFormat: defaultTimeFormat, Location: time.UTC}
			out, ok := f.Format([]byte(tt.line))
			if !ok {
				t.Fatal("expected line to be printed")
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("output %q missing %q", out, want)
				}
			}
			for _, skip := range tt.skip {
				if strings.Contains(out, skip) {
					t.Errorf("output %q should not contain %q", out, skip)
				}
			}
		})
	}
}

// Lines that are not logfmt must survive untouched, so pl stays safe on mixed
// output.
func TestFormat_LogfmtPassthrough(t *testing.T) {
	for _, line := range []string{
		"hello world",
		"cool%story=bro f %^asdf",
		"go: downloading github.com/go-faster/jx v1.2.0",
		`--- FAIL: TestFoo (0.00s)`,
		"level=info",                              // Only one well-known field.
		`msg="unterminated`,                       // Malformed quoting.
		"http://example.com/a=b?c=d GET 200 12ms", // A URL is not a level/msg/ts.
	} {
		f := &Formatter{}
		out, ok := f.Format([]byte(line))
		if !ok {
			t.Fatalf("%q: expected line to be printed", line)
		}
		if out != line {
			t.Errorf("%q: passed through as %q", line, out)
		}
	}
}

func TestNormalizeLevel(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"Warning", levelWarn},
		{"WARN", levelWarn},
		{"\tError ", levelError},
		{"E", levelError},
		{"trace", levelDebug},
		{"critical", levelFatal},
	} {
		in, want := tt.in, tt.want
		got, ok := normalizeLevel(in)
		if !ok || got != want {
			t.Errorf("normalizeLevel(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "nope", "12"} {
		if got, ok := normalizeLevel(in); ok {
			t.Errorf("normalizeLevel(%q) = %q, want no match", in, got)
		}
	}
}

func TestScalarRaw(t *testing.T) {
	for _, in := range []string{"1", "-1", "1.5", "1e9", "true", "false", "null"} {
		if _, ok := scalarRaw(in); !ok {
			t.Errorf("scalarRaw(%q) = false, want true", in)
		}
	}
	// Values ParseFloat accepts but JSON does not, plus plain strings.
	for _, in := range []string{"", "01", "+1", ".5", "1.", "Inf", "NaN", "0x1p-2", "10ms", "ok"} {
		if raw, ok := scalarRaw(in); ok {
			t.Errorf("scalarRaw(%q) = %q, want false", in, raw)
		}
	}
}

func FuzzParseLogfmt(f *testing.F) {
	for _, s := range []string{
		`logger=tracing t=2023-12-19T06:57:42.301691427Z level=error msg="OpenTelemetry handler returned an error"`,
		`time="2015-03-26T01:27:38-04:00" level=warning msg="The group's number increased tremendously!" number=122 omg=true`,
		`level=debug time="2015-03-26T01:27:38-04:00" foo=bar a=14 baz="hello kitty" cool%story=bro f %^asdf`,
		`ts=1620000000123 level=info msg=hi`,
		"hello world",
		`msg="unterminated`,
		"=",
		`"`,
	} {
		f.Add(s)
	}
	formatter := &Formatter{}
	f.Fuzz(func(t *testing.T, line string) {
		m, ok := parseLogfmt(line)
		if !ok {
			return
		}
		// Every value must be valid JSON, as the renderer assumes.
		for k, v := range m {
			if !jx.Valid(v) {
				t.Fatalf("key %q: invalid JSON value %q", k, v)
			}
		}
		if _, ok := formatter.render(m); !ok {
			t.Fatal("expected rendered line to be printed")
		}
	})
}
