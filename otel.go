package pl

import (
	"sort"
	"strings"

	"github.com/go-faster/jx"
	"go.uber.org/zap/zapcore"
)

// keyResource holds the raw OTEL Resource array so it can be rendered as its own
// multi-line block rather than as inline fields. The leading NUL keeps it from
// colliding with any real JSON field name.
const keyResource = "\x00resource"

// OpenTelemetry log data model field keys, as emitted in the JSON form by the
// go-faster/sdk console log exporter (see the OpenTelemetry logs data model:
// specification/logs/data-model.md). Unlike zap's lower-case keys these are
// capitalized, which lets the two formats be told apart.
const (
	otelTimestamp    = "Timestamp"
	otelSeverity     = "Severity"
	otelSeverityText = "SeverityText"
	otelBody         = "Body"
	otelAttributes   = "Attributes"
	otelTraceID      = "TraceID"
	otelSpanID       = "SpanID"
	otelScope        = "Scope"
	otelResource     = "Resource"
)

// Well-known OTEL attribute keys that map onto zap's reserved fields rather than
// being shown as plain extra fields.
const (
	attrCodeFilePath        = "code.file.path"
	attrCodeLineNumber      = "code.line.number"
	attrCodeFunctionName    = "code.function.name"
	attrExceptionMessage    = "exception.message"
	attrExceptionStacktrace = "exception.stacktrace"
)

// Extra (non-reserved) keys the normalized OTEL record adds for trace
// correlation; populated only when the corresponding id is non-zero.
const (
	keyTraceID = "trace_id"
	keySpanID  = "span_id"
)

// zap level name tokens, the form parseLevel understands.
const (
	levelDebug  = "debug"
	levelInfo   = "info"
	levelWarn   = "warn"
	levelError  = "error"
	levelDPanic = "dpanic"
	levelPanic  = "panic"
	levelFatal  = "fatal"
)

// isOTEL reports whether the parsed object is an OpenTelemetry log record (the
// data model JSON) rather than a zap production log line. The two are told apart
// by OTEL's capitalized Body field paired with a severity field.
func isOTEL(m map[string]jx.Raw) bool {
	if _, ok := m[otelBody]; !ok {
		return false
	}
	if _, ok := m[otelSeverityText]; ok {
		return true
	}
	_, ok := m[otelSeverity]
	return ok
}

// normalizeOTEL converts an OpenTelemetry log record into a map keyed by the zap
// field names the formatter already understands, so a single rendering path
// serves both formats. It returns the rebuilt map and true when m is an OTEL
// record; otherwise it returns nil and false.
//
// Scope metadata other than the scope name is dropped: it repeats identically on
// every line and would drown the message. Resource attributes and the function
// name are likewise dropped unless f.OTELResource / f.OTELFunc opt them in.
func (f *Formatter) normalizeOTEL(m map[string]jx.Raw) (map[string]jx.Raw, bool) {
	if !isOTEL(m) {
		return nil, false
	}
	out := make(map[string]jx.Raw, len(m))

	if ts := m[otelTimestamp]; len(ts) > 0 {
		out[keyTime] = ts
	}
	out[keyLevel] = otelLevel(m)
	if body := otelInnerValue(m[otelBody]); len(body) > 0 {
		out[keyMessage] = body
	}
	if name := otelScopeName(m[otelScope]); name != "" {
		out[keyLogger] = rawString(name)
	}

	// Flatten attributes; a few well-known ones map onto zap's reserved fields,
	// the rest become plain extra fields.
	attrs := otelAttrs(m[otelAttributes])
	if caller := otelCaller(attrs, f.OTELFunc); caller != "" {
		out[keyCaller] = rawString(caller)
	}
	if msg := attrs[attrExceptionMessage]; len(msg) > 0 {
		out[keyError] = msg
	}
	if st := attrs[attrExceptionStacktrace]; len(st) > 0 {
		out[keyStacktrace] = st
	}
	for k, v := range attrs {
		switch k {
		case attrCodeFilePath, attrCodeLineNumber, attrCodeFunctionName,
			attrExceptionMessage, attrExceptionStacktrace:
			// Folded into caller / error / stacktrace above.
			continue
		}
		out[k] = v
	}

	// Resource attributes are dropped by default (they repeat on every line);
	// when asked, stash the raw array for the multi-line block in Format.
	if f.OTELResource {
		if res := m[otelResource]; len(res) > 0 {
			out[keyResource] = res
		}
	}

	// Trace correlation, shown only when populated; all-zero ids carry no
	// information and would be noise on every line.
	if id := asString(m[otelTraceID]); !isZeroHex(id) {
		out[keyTraceID] = m[otelTraceID]
	}
	if id := asString(m[otelSpanID]); !isZeroHex(id) {
		out[keySpanID] = m[otelSpanID]
	}

	return out, true
}

// otelLevel maps an OTEL severity onto a zap level token. SeverityText is used
// when it parses as a zap level (it usually carries names like "info"/"warn");
// otherwise the numeric SeverityNumber is bucketed per the data model severity
// ranges.
func otelLevel(m map[string]jx.Raw) jx.Raw {
	if t := asString(m[otelSeverityText]); t != "" {
		lower := strings.ToLower(t)
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(lower)); err == nil {
			return rawString(lower)
		}
	}
	if n, err := jx.DecodeBytes(m[otelSeverity]).Int(); err == nil {
		return rawString(severityName(n))
	}
	return rawString(levelInfo)
}

// severityName buckets an OTEL SeverityNumber into a zap level name following
// the logs data model ranges (1-4 TRACE, 5-8 DEBUG, 9-12 INFO, 13-16 WARN,
// 17-20 ERROR, 21-24 FATAL). zap has no trace level, so TRACE collapses to
// debug.
func severityName(n int) string {
	switch {
	case n <= 0:
		return levelInfo // Unspecified; the data model treats this as INFO.
	case n <= 8:
		return levelDebug
	case n <= 12:
		return levelInfo
	case n <= 16:
		return levelWarn
	case n <= 20:
		return levelError
	default:
		return levelFatal
	}
}

// otelInnerValue unwraps an OTEL typed value object, shaped as
// {"Type": "...", "Value": <v>}, returning the raw JSON of <v>. A value that is
// not such an object is returned unchanged.
func otelInnerValue(raw jx.Raw) jx.Raw {
	if len(raw) == 0 || raw.Type() != jx.Object {
		return raw
	}
	var val jx.Raw
	d := jx.DecodeBytes(raw)
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if string(key) == "Value" {
			v, err := d.RawAppend(nil)
			if err != nil {
				return err
			}
			val = v
			return nil
		}
		return d.Skip()
	}); err != nil {
		return raw
	}
	return val
}

// otelAttrs flattens an OTEL attribute list, shaped as
// [{"Key": "...", "Value": {"Type": "...", "Value": <v>}}, ...], into a map from
// key to the raw JSON of each unwrapped value.
func otelAttrs(raw jx.Raw) map[string]jx.Raw {
	out := make(map[string]jx.Raw)
	if len(raw) == 0 || raw.Type() != jx.Array {
		return out
	}
	d := jx.DecodeBytes(raw)
	_ = d.Arr(func(d *jx.Decoder) error {
		var (
			key string
			val jx.Raw
		)
		if err := d.ObjBytes(func(d *jx.Decoder, k []byte) error {
			switch string(k) {
			case "Key":
				s, err := d.Str()
				if err != nil {
					return err
				}
				key = s
			case "Value":
				v, err := d.RawAppend(nil)
				if err != nil {
					return err
				}
				val = otelInnerValue(v)
			default:
				return d.Skip()
			}
			return nil
		}); err != nil {
			return err
		}
		if key != "" {
			out[key] = val
		}
		return nil
	})
	return out
}

// otelScopeName extracts the instrumentation scope name, which plays the role of
// zap's logger name.
func otelScopeName(raw jx.Raw) string {
	if len(raw) == 0 || raw.Type() != jx.Object {
		return ""
	}
	var name string
	_ = jx.DecodeBytes(raw).ObjBytes(func(d *jx.Decoder, key []byte) error {
		if string(key) == "Name" {
			s, err := d.Str()
			if err != nil {
				return err
			}
			name = s
			return nil
		}
		return d.Skip()
	})
	return name
}

// otelCaller reconstructs a zap-style "file:line" caller from the OTEL source
// code attributes, optionally appending the function name. It returns an empty
// string when no file path is present.
func otelCaller(attrs map[string]jx.Raw, withFunc bool) string {
	path := asString(attrs[attrCodeFilePath])
	if path == "" {
		return ""
	}
	if line := asString(attrs[attrCodeLineNumber]); line != "" {
		path += ":" + line
	}
	if withFunc {
		if fn := asString(attrs[attrCodeFunctionName]); fn != "" {
			path += " " + fn
		}
	}
	return path
}

// rawString encodes s as a JSON string value, so values synthesized during
// normalization read back through asString exactly like parsed ones.
func rawString(s string) jx.Raw {
	var w jx.Writer
	w.Str(s)
	return jx.Raw(w.Buf)
}

// writeResource renders the OTEL resource attributes on their own indented
// lines below the log entry, each key painted in colBlue to set them apart from
// the cyan inline fields. Keys are sorted for stable output.
func (f *Formatter) writeResource(b *strings.Builder, raw jx.Raw) {
	attrs := otelAttrs(raw)
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte('\n')
		b.WriteByte('\t')
		b.WriteString(f.paint(colBlue, k))
		b.WriteByte('=')
		b.WriteString(renderValue(attrs[k]))
	}
}

// liftZapCtx flattens a zap "ctx" object field, produced by go-faster/sdk's
// zctx in otelzap mode (zctx.WithOpenTelemetryZap logs zap.Reflect("ctx", ctx)),
// onto the top-level map so its members — notably span_id and trace_id — render
// as ordinary fields rather than a nested JSON blob. This mirrors the OTEL path,
// so a stream mixing zctx's two modes reads uniformly. All-zero trace/span ids
// are dropped as noise; members that would shadow an existing top-level or
// reserved key are left in place. The ctx field is removed once flattened.
func liftZapCtx(m map[string]jx.Raw) {
	raw := m[keyCtx]
	if len(raw) == 0 || raw.Type() != jx.Object {
		return
	}
	inner := make(map[string]jx.Raw)
	if err := jx.DecodeBytes(raw).ObjBytes(func(d *jx.Decoder, key []byte) error {
		v, err := d.RawAppend(nil)
		if err != nil {
			return err
		}
		inner[string(key)] = v
		return nil
	}); err != nil {
		// Not a flat object we can read; leave ctx as-is to be shown verbatim.
		return
	}
	for k, v := range inner {
		if k == keySpanID || k == keyTraceID {
			if isZeroHex(asString(v)) {
				continue
			}
		}
		if _, ok := reserved[k]; ok {
			continue
		}
		if _, ok := m[k]; ok {
			continue
		}
		m[k] = v
	}
	delete(m, keyCtx)
}

// isZeroHex reports whether s is empty or consists solely of '0', i.e. an
// all-zero trace or span id that carries no correlation information.
func isZeroHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}
