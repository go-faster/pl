// Package pl tails and pretty-prints JSONL logs produced by zap
// (the zap.NewProductionConfig JSON encoder used by go-faster/sdk).
package pl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// Well-known zap field keys (see zap.NewProductionEncoderConfig).
const (
	keyTime       = "ts"
	keyLevel      = "level"
	keyMessage    = "msg"
	keyLogger     = "logger"
	keyCaller     = "caller"
	keyStacktrace = "stacktrace"
)

// reserved keys are rendered specially and excluded from the extra fields.
var reserved = map[string]struct{}{
	keyTime:       {},
	keyLevel:      {},
	keyMessage:    {},
	keyLogger:     {},
	keyCaller:     {},
	keyStacktrace: {},
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
)

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

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		// Malformed JSON, pass through.
		return string(line), true
	}

	lvl, lvlOK := parseLevel(m[keyLevel])
	if f.levelSet && lvlOK && lvl < f.MinLevel {
		return "", false
	}

	var b strings.Builder

	// Timestamp.
	if !f.NoTime {
		if ts, ok := parseTime(m[keyTime]); ok {
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

	// Stacktrace on following indented lines.
	if st := asString(m[keyStacktrace]); st != "" {
		for _, l := range strings.Split(strings.TrimRight(st, "\n"), "\n") {
			b.WriteByte('\n')
			b.WriteString(f.paint(colDim, "\t"+l))
		}
	}

	return b.String(), true
}

func (f *Formatter) paintLevel(l zapcore.Level) string {
	label := strings.ToUpper(l.String())
	// Pad to a fixed width for alignment.
	label = fmt.Sprintf("%-5s", label)
	var color string
	switch {
	case l <= zapcore.DebugLevel:
		color = colDim
	case l == zapcore.InfoLevel:
		color = colGreen
	case l == zapcore.WarnLevel:
		color = colYellow
	case l == zapcore.ErrorLevel:
		color = colRed
	default: // dpanic, panic, fatal
		color = colBold + colRed
	}
	return f.paint(color, label)
}

func parseLevel(raw json.RawMessage) (zapcore.Level, bool) {
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
func parseTime(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	// String timestamp.
	if raw[0] == '"' {
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
	num, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return time.Time{}, false
	}
	sec, frac := math.Modf(num)
	return time.Unix(int64(sec), int64(frac*1e9)).Local(), true
}

// asString returns the string value of raw, unquoting JSON strings. For
// non-string JSON it returns the raw text.
func asString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return string(raw)
}

// renderValue renders an extra field value compactly: strings unquoted,
// everything else as its JSON text.
func renderValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		s := asString(raw)
		// Quote only when the value contains spaces.
		if strings.ContainsAny(s, " \t") {
			return strconv.Quote(s)
		}
		return s
	}
	return string(raw)
}
