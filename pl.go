// Package pl tails and pretty-prints JSONL logs produced by zap
// (the zap.NewProductionConfig JSON encoder used by go-faster/sdk) as well as
// OpenTelemetry log records in the logs data model JSON form. Each line is
// detected and normalized onto a shared rendering path, so a stream mixing the
// two formats reads uniformly.
package pl

import (
	"bytes"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"
	"go.uber.org/zap/zapcore"
)

// Well-known zap field keys (see zap.NewProductionEncoderConfig).
const (
	keyTime         = "ts"
	keyLevel        = "level"
	keyMessage      = "msg"
	keyLogger       = "logger"
	keyCaller       = "caller"
	keyStacktrace   = "stacktrace"
	keyError        = "error"
	keyErrorVerbose = "errorVerbose"
	// keyCtx is where go-faster/sdk's zctx logs a reflected context in otelzap
	// mode (see zctx.WithOpenTelemetryZap). It carries trace correlation
	// (span_id, trace_id) and any context-scoped fields as a nested object;
	// liftZapCtx flattens it so those read like ordinary fields.
	keyCtx = "ctx"
)

// reserved keys are rendered specially and excluded from the extra fields.
var reserved = map[string]struct{}{
	keyTime:         {},
	keyLevel:        {},
	keyMessage:      {},
	keyLogger:       {},
	keyCaller:       {},
	keyStacktrace:   {},
	keyError:        {},
	keyErrorVerbose: {},
	keyResource:     {},
}

// ANSI color codes.
const (
	colReset   = "\033[0m"
	colDim     = "\033[2m"
	colRed     = "\033[31m"
	colGreen   = "\033[32m"
	colYellow  = "\033[33m"
	colBlue    = "\033[34m"
	colMagenta = "\033[35m"
	colCyan    = "\033[36m"
	colBold    = "\033[1m"
	colItalic  = "\033[3m"
)

// LevelStyle controls how a single log level is rendered.
type LevelStyle struct {
	// Label is the text shown for the level (e.g. "I" for info).
	Label string
	// Color is the ANSI escape sequence applied to the label. Empty means no
	// color. Ignored when the Formatter has Color disabled.
	Color string
}

// DefaultLevelStyles returns the built-in level styles: a single-character
// label and a color per level (D/I/W/E, and C for dpanic/panic/fatal).
func DefaultLevelStyles() map[zapcore.Level]LevelStyle {
	return map[zapcore.Level]LevelStyle{
		zapcore.DebugLevel:  {Label: "D", Color: colDim},
		zapcore.InfoLevel:   {Label: "I", Color: colGreen},
		zapcore.WarnLevel:   {Label: "W", Color: colYellow},
		zapcore.ErrorLevel:  {Label: "E", Color: colRed},
		zapcore.DPanicLevel: {Label: "C", Color: colBold + colRed},
		zapcore.PanicLevel:  {Label: "C", Color: colBold + colRed},
		zapcore.FatalLevel:  {Label: "C", Color: colBold + colRed},
	}
}

// Formatter renders a single zap JSON log line into a human-readable form.
type Formatter struct {
	// Color enables ANSI colors in the output.
	Color bool
	// MinLevel, when set, drops lines below this level.
	MinLevel zapcore.Level
	// TimeFormat is the layout for the timestamp. Defaults to "15:04:05.000".
	TimeFormat string
	// NoTime omits the timestamp from the output entirely.
	NoTime bool
	// Location, when set, converts timestamps to this timezone before
	// formatting. A nil Location uses the timestamp's own location.
	Location *time.Location
	// LevelStyles overrides how levels are rendered. Levels absent from the map
	// fall back to DefaultLevelStyles; a nil map uses the defaults entirely.
	LevelStyles map[zapcore.Level]LevelStyle
	// OTELResource includes OpenTelemetry resource attributes (service.name,
	// host.name, ...) as fields. They are dropped by default because they repeat
	// identically on every line. Only affects OTEL log records.
	OTELResource bool
	// OTELFunc includes the function name (the code.function.name attribute) in
	// the caller of OpenTelemetry log records. Dropped by default as it
	// duplicates the source location.
	OTELFunc bool

	levelSet bool
}

// SetMinLevel sets the minimum level to display.
func (f *Formatter) SetMinLevel(l zapcore.Level) {
	f.MinLevel = l
	f.levelSet = true
}

func (f *Formatter) paint(color, s string) string {
	if !f.Color || color == "" {
		return s
	}
	return color + s + colReset
}

func (f *Formatter) timeFormat() string {
	if f.TimeFormat == "" {
		return "15:04:05.000"
	}
	return f.TimeFormat
}

// Format parses a single JSON log line and returns its pretty representation.
//
// The returned bool reports whether the line should be printed. Lines that are
// not valid zap JSON are returned unchanged (and ok is true) so that mixed
// output is preserved. Lines below MinLevel are dropped (ok is false).
func (f *Formatter) Format(line []byte) (out string, ok bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return "", false
	}
	if trimmed[0] != '{' {
		// Not a JSON object, pass through.
		return string(line), true
	}

	m := make(map[string]jx.Raw)
	d := jx.DecodeBytes(trimmed)
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		// RawAppend copies the value out of the decoder buffer so it stays
		// valid after the callback returns.
		raw, err := d.RawAppend(nil)
		if err != nil {
			return err
		}
		m[string(key)] = raw
		return nil
	}); err != nil {
		// Malformed JSON, pass through.
		return string(line), true
	}

	// OpenTelemetry log records use a different (capitalized) field layout;
	// normalize them onto zap's keys so the rest of the formatter is shared.
	if om, ok := f.normalizeOTEL(m); ok {
		m = om
	} else {
		// A zap line: flatten zctx's reflected "ctx" object (otelzap mode) so
		// span_id/trace_id surface as ordinary fields instead of a JSON blob.
		liftZapCtx(m)
	}

	lvl, lvlOK := parseLevel(m[keyLevel])
	if f.levelSet && lvlOK && lvl < f.MinLevel {
		return "", false
	}

	var b strings.Builder

	// Timestamp.
	if !f.NoTime {
		if ts, ok := parseTime(m[keyTime]); ok {
			if f.Location != nil {
				ts = ts.In(f.Location)
			}
			b.WriteString(f.paint(colDim, ts.Format(f.timeFormat())))
			b.WriteByte(' ')
		}
	}

	// Level.
	if lvlOK {
		b.WriteString(f.paintLevel(lvl))
		b.WriteByte(' ')
	}

	// Logger name.
	if name := asString(m[keyLogger]); name != "" {
		b.WriteString(f.paint(colMagenta, name))
		b.WriteByte(' ')
	}

	// Message.
	if msg := asString(m[keyMessage]); msg != "" {
		b.WriteString(f.paint(colBold, msg))
	}

	// Error, rendered bare and in red right after the message rather than as a
	// key=value field. Interior quotes are kept intact (see renderError) so
	// messages like SQL errors read naturally.
	if e := m[keyError]; len(e) > 0 {
		b.WriteByte(' ')
		b.WriteString(f.paint(colRed, renderError(e)))
	}

	// Extra fields, sorted for stable output.
	keys := make([]string, 0, len(m))
	for k := range m {
		if _, ok := reserved[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(f.paint(colCyan, k))
		b.WriteByte('=')
		b.WriteString(renderValue(m[k]))
	}

	// Caller, dimmed at the end.
	if caller := asString(m[keyCaller]); caller != "" {
		b.WriteByte(' ')
		b.WriteString(f.paint(colDim, "("+caller+")"))
	}

	// OpenTelemetry resource attributes, when enabled, on their own indented
	// lines in a distinct color so they read apart from the inline fields.
	if res := m[keyResource]; len(res) > 0 {
		f.writeResource(&b, res)
	}

	// Verbose error on following indented lines, in a warm palette (red error
	// text, yellow frames) so it is not confused with the runtime stacktrace.
	if ev := asString(m[keyErrorVerbose]); ev != "" {
		f.writeStack(&b, ev, colRed, colYellow)
	}

	// Stacktrace on following indented lines, in a cool palette (blue frames).
	if st := asString(m[keyStacktrace]); st != "" {
		f.writeStack(&b, st, colDim, colBlue)
	}

	return b.String(), true
}

// writeStack renders a zap stacktrace or go-faster/errors verbose error block
// onto b, each line indented under the log entry on its own line so the
// original layout is preserved.
//
// Both formats interleave Go stack frames — a function name followed by an
// indented "file:line" location — with free-form message text. Each line is
// classified and styled so the three read apart at a glance:
//
//   - source locations are dimmed and italicized (the least important "where");
//   - function names, which introduce a location, are painted with funcColor;
//   - everything else is message text, painted with msgColor (the error chain
//     in errorVerbose).
//
// The errorVerbose and stacktrace blocks pass different palettes (warm vs cool)
// so the error chain is not mistaken for the runtime stacktrace.
func (f *Formatter) writeStack(b *strings.Builder, block, msgColor, funcColor string) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for i, l := range lines {
		b.WriteByte('\n')
		switch {
		case isStackLocation(l):
			b.WriteString(f.paint(colDim+colItalic, "\t"+l))
		case i+1 < len(lines) && isStackLocation(lines[i+1]):
			// A function name introducing the following location line.
			b.WriteString(f.paint(funcColor, "\t"+l))
		default:
			b.WriteString(f.paint(msgColor, "\t"+l))
		}
	}
}

// isStackLocation reports whether s is a stack-frame source location, i.e. a
// "path:line" pair such as "/usr/local/go/src/runtime/proc.go:267", optionally
// followed by a program-counter offset ("+0x2a"). Leading indentation, used by
// both zap and go-faster/errors to set locations apart from function names, is
// ignored.
func isStackLocation(s string) bool {
	s = strings.TrimSpace(s)
	i := strings.LastIndexByte(s, ':')
	if i <= 0 {
		return false
	}
	path, rest := s[:i], s[i+1:]
	// Drop an optional " +0x..." offset following the line number.
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		rest = rest[:sp]
	}
	if rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	// The part before the line number should look like a file path.
	return strings.ContainsAny(path, "./")
}

func (f *Formatter) levelStyle(l zapcore.Level) LevelStyle {
	if s, ok := f.LevelStyles[l]; ok {
		return s
	}
	if s, ok := DefaultLevelStyles()[l]; ok {
		return s
	}
	// Unknown level: fall back to its uppercased name without color.
	return LevelStyle{Label: strings.ToUpper(l.String())}
}

func (f *Formatter) paintLevel(l zapcore.Level) string {
	s := f.levelStyle(l)
	return f.paint(s.Color, s.Label)
}

func parseLevel(raw jx.Raw) (zapcore.Level, bool) {
	s := asString(raw)
	if s == "" {
		return 0, false
	}
	var l zapcore.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, false
	}
	return l, true
}

// parseTime decodes the zap timestamp, which may be an epoch float/int (the
// production default) or an RFC3339/ISO8601 string.
func parseTime(raw jx.Raw) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	// String timestamp.
	if raw.Type() == jx.String {
		s := asString(raw)
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05.000Z0700",
			"2006-01-02 15:04:05",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	// Numeric epoch (seconds, possibly fractional).
	num, err := jx.DecodeBytes(raw).Float64()
	if err != nil {
		return time.Time{}, false
	}
	sec, frac := math.Modf(num)
	return time.Unix(int64(sec), int64(frac*1e9)).Local(), true
}

// asString returns the string value of raw, unquoting JSON strings. For
// non-string JSON it returns the raw text.
func asString(raw jx.Raw) string {
	if len(raw) == 0 {
		return ""
	}
	if raw.Type() == jx.String {
		if s, err := jx.DecodeBytes(raw).Str(); err == nil {
			return s
		}
	}
	return string(raw)
}

// renderError renders an error field value as its bare string, without
// surrounding or escaping quotes, so messages like SQL errors read naturally
// (e.g. error=insert: null value in column "attributes" ...).
func renderError(raw jx.Raw) string {
	if len(raw) == 0 {
		return ""
	}
	if raw.Type() == jx.String {
		return asString(raw)
	}
	return string(raw)
}

// renderValue renders an extra field value compactly: strings unquoted,
// everything else as its JSON text.
func renderValue(raw jx.Raw) string {
	if len(raw) == 0 {
		return ""
	}
	if raw.Type() == jx.String {
		s := asString(raw)
		// Quote only when the value contains spaces.
		if strings.ContainsAny(s, " \t") {
			return strconv.Quote(s)
		}
		return s
	}
	return string(raw)
}
