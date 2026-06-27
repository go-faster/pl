package pl

import (
	"strconv"
	"strings"
	"testing"

	"github.com/go-faster/jx"
)

// decodeObj parses a JSON object into the raw-field map shape Format builds
// internally, so the normalization helpers can be tested directly.
func decodeObj(t *testing.T, s string) map[string]jx.Raw {
	t.Helper()
	m := make(map[string]jx.Raw)
	d := jx.DecodeStr(s)
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		raw, err := d.RawAppend(nil)
		if err != nil {
			return err
		}
		m[string(key)] = raw
		return nil
	}); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return m
}

// A log record in the OpenTelemetry logs data model JSON form, as emitted by the
// go-faster/sdk console exporter.
const otelLine = `{"Timestamp":"2026-06-27T20:02:07.563489947Z","ObservedTimestamp":"2026-06-27T20:02:07.563505326Z","Severity":9,"SeverityText":"info","Body":{"Type":"String","Value":"ClickHouse disabled"},"Attributes":[{"Key":"code.file.path","Value":{"Type":"String","Value":"oteldb/app.go"}},{"Key":"code.line.number","Value":{"Type":"Int64","Value":114}},{"Key":"code.function.name","Value":{"Type":"String","Value":"main.newApp"}},{"Key":"backend","Value":{"Type":"String","Value":"memory"}}],"TraceID":"00000000000000000000000000000000","SpanID":"0000000000000000","TraceFlags":"00","Resource":[{"Key":"service.name","Value":{"Type":"STRING","Value":"oteldb"}}],"Scope":{"Name":"github.com/go-faster/sdk/app","Version":"","SchemaURL":"","Attributes":{}},"DroppedAttributes":0}`

func TestFormat_OTEL(t *testing.T) {
	f := &Formatter{NoTime: true}
	out, ok := f.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected line to be printed")
	}
	for _, want := range []string{
		"I ",                           // severity -> level label
		"github.com/go-faster/sdk/app", // scope name -> logger
		"ClickHouse disabled",          // Body value -> message
		"backend=memory",               // attribute -> extra field
		"(oteldb/app.go:114)",          // code.* attrs -> caller
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
	// Resource attributes, scope metadata and the code.function.name attribute
	// are intentionally dropped to keep lines readable.
	for _, unwanted := range []string{"service.name", "SchemaURL", "code.function.name", "main.newApp"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output %q should not contain %q", out, unwanted)
		}
	}
	// All-zero trace/span ids carry no information and must be omitted.
	if strings.Contains(out, "trace_id") || strings.Contains(out, "span_id") {
		t.Errorf("zero trace/span ids should be omitted: %q", out)
	}
}

func TestFormat_OTELResourceFlag(t *testing.T) {
	// Off by default: resource attributes are omitted.
	off := &Formatter{NoTime: true}
	out, ok := off.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected output")
	}
	if strings.Contains(out, "service.name") {
		t.Errorf("resource attrs should be omitted by default: %q", out)
	}
	// On: resource attributes appear on their own indented lines.
	on := &Formatter{NoTime: true, OTELResource: true}
	out, ok = on.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, "\n\tservice.name=oteldb") {
		t.Errorf("resource attr not on its own indented line: %q", out)
	}

	// Colored: resource keys use a color distinct from the cyan inline fields.
	col := &Formatter{NoTime: true, Color: true, OTELResource: true}
	out, ok = col.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.Contains(out, colBlue+"service.name"+colReset) {
		t.Errorf("resource key not painted in the distinct color: %q", out)
	}
	if strings.Contains(out, colCyan+"service.name") {
		t.Errorf("resource key should not use the inline-field color: %q", out)
	}
}

func TestFormat_OTELFuncFlag(t *testing.T) {
	// Off by default: the function name is dropped from the caller.
	off := &Formatter{NoTime: true}
	out, ok := off.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected output")
	}
	if want := "(oteldb/app.go:114)"; !strings.Contains(out, want) {
		t.Errorf("output %q missing %q", out, want)
	}
	if strings.Contains(out, "main.newApp") {
		t.Errorf("function name should be omitted by default: %q", out)
	}
	// On: the function name is appended inside the caller.
	on := &Formatter{NoTime: true, OTELFunc: true}
	out, ok = on.Format([]byte(otelLine))
	if !ok {
		t.Fatal("expected output")
	}
	if want := "(oteldb/app.go:114 main.newApp)"; !strings.Contains(out, want) {
		t.Errorf("output %q missing %q", out, want)
	}
}

func TestFormat_OTELTraceCorrelation(t *testing.T) {
	line := `{"Severity":9,"SeverityText":"info","Body":{"Type":"String","Value":"req"},"Attributes":[],"TraceID":"0af7651916cd43dd8448eb211c80319c","SpanID":"b7ad6b7169203331","Scope":{"Name":"svc"}}`
	f := &Formatter{NoTime: true}
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	for _, want := range []string{
		"trace_id=0af7651916cd43dd8448eb211c80319c",
		"span_id=b7ad6b7169203331",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestFormat_OTELException(t *testing.T) {
	line := `{"Severity":17,"SeverityText":"error","Body":{"Type":"String","Value":"request failed"},"Attributes":[{"Key":"exception.message","Value":{"Type":"String","Value":"connection refused"}},{"Key":"exception.stacktrace","Value":{"Type":"String","Value":"main.run\n\t/app/main.go:10"}}],"Scope":{"Name":"svc"}}`
	f := &Formatter{NoTime: true}
	out, ok := f.Format([]byte(line))
	if !ok {
		t.Fatal("expected output")
	}
	if !strings.HasPrefix(out, "E ") {
		t.Errorf("expected error level label: %q", out)
	}
	for _, want := range []string{"connection refused", "/app/main.go:10"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
	// The exception attributes are rendered as error/stacktrace, not as raw
	// key=value extra fields.
	if strings.Contains(out, "exception.message=") {
		t.Errorf("exception.message should not be a plain field: %q", out)
	}
}

func TestFormat_OTELSeverityNumber(t *testing.T) {
	// SeverityText absent: the level is derived from the numeric SeverityNumber.
	f := &Formatter{NoTime: true}
	cases := map[int]string{
		2:  "D", // TRACE range collapses to debug
		6:  "D",
		9:  "I",
		14: "W",
		18: "E",
		22: "C", // FATAL
	}
	for sev, want := range cases {
		line := `{"Severity":` + strconv.Itoa(sev) + `,"Body":{"Type":"String","Value":"m"}}`
		out, ok := f.Format([]byte(line))
		if !ok {
			t.Fatalf("sev %d: expected output", sev)
		}
		if got, _, _ := strings.Cut(out, " "); got != want {
			t.Errorf("sev %d: label = %q, want %q (out=%q)", sev, got, want, out)
		}
	}
}

func TestIsOTEL(t *testing.T) {
	f := &Formatter{}
	// A zap line must not be mistaken for an OTEL record.
	if _, ok := f.normalizeOTEL(decodeObj(t, `{"level":"info","ts":1620000000.0,"msg":"hi"}`)); ok {
		t.Error("zap line classified as OTEL")
	}
	if _, ok := f.normalizeOTEL(decodeObj(t, otelLine)); !ok {
		t.Error("OTEL line not classified as OTEL")
	}
}

func TestSeverityName(t *testing.T) {
	cases := map[int]string{
		0: "info", -1: "info", 3: "debug", 5: "debug", 9: "info",
		12: "info", 13: "warn", 17: "error", 20: "error", 21: "fatal", 24: "fatal",
	}
	for n, want := range cases {
		if got := severityName(n); got != want {
			t.Errorf("severityName(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestIsZeroHex(t *testing.T) {
	for s, want := range map[string]bool{
		"":                 true,
		"0":                true,
		"0000000000000000": true,
		"b7ad6b7169203331": false,
		"0001":             false,
	} {
		if got := isZeroHex(s); got != want {
			t.Errorf("isZeroHex(%q) = %v, want %v", s, got, want)
		}
	}
}
