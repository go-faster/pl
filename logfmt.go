package pl

import (
	"strconv"
	"strings"

	"github.com/go-faster/jx"
)

// logfmtKeys maps well-known logfmt field names onto the zap keys the formatter
// renders specially (level, message, caller, ...). Every other key stays a plain
// field.
var logfmtKeys = map[string]string{
	"level": keyLevel, "lvl": keyLevel, "severity": keyLevel, "severity_text": keyLevel, "levelname": keyLevel,
	"t": keyTime, "ts": keyTime, "time": keyTime, "timestamp": keyTime, "@timestamp": keyTime,
	"msg": keyMessage, "message": keyMessage,
	"logger": keyLogger,
	"caller": keyCaller,
	"err":    keyError, keyError: keyError,
	"stacktrace": keyStacktrace, "stack": keyStacktrace,
	"trace_id": keyTraceID, "traceid": keyTraceID, "traceID": keyTraceID, "traceId": keyTraceID,
	"span_id": keySpanID, "spanid": keySpanID, "spanID": keySpanID, "spanId": keySpanID,
}

// keyLevelText holds a level token pl has no zap level for, so it is shown as a
// field instead of being dropped with the reserved "level" key.
const keyLevelText = "severity_text"

// seenFields is a bitset of the well-known fields met while parsing a line; it
// decides whether a line is logfmt at all (see parseLogfmt).
type seenFields uint8

const (
	seenLevel seenFields = 1 << iota
	seenTime
	seenMessage
)

func (s seenFields) count() int {
	var n int
	for ; s != 0; s >>= 1 {
		n += int(s & 1)
	}
	return n
}

// parseLogfmt parses a logfmt line — the format used by logrus, Grafana, Loki
// and friends — into the zap-keyed map the formatter renders.
//
// It reports false for lines that are not logfmt. Since almost any text scans as
// a sequence of bare keys, a line qualifies only when it carries at least two of
// the level, timestamp and message fields; that keeps ordinary prose (which pl
// passes through untouched) from being mangled into fields.
func parseLogfmt(line string) (map[string]jx.Raw, bool) {
	if line == "" || line[0] == '{' {
		return nil, false
	}
	m := make(map[string]jx.Raw)
	var seen seenFields
	if !scanLogfmt(line, func(key, val string, quoted, hasVal bool) {
		seen |= putLogfmtField(m, key, val, quoted, hasVal)
	}) {
		return nil, false
	}
	if seen.count() < 2 {
		return nil, false
	}
	return m, true
}

// putLogfmtField stores a single logfmt pair in m, mapping well-known names onto
// zap keys and deducing the value type, and reports which well-known fields it
// matched.
//
// quoted tells whether the value was a quoted string, in which case it is never
// re-interpreted as a number or bool. hasVal is false for a bare key, which
// logfmt reads as a true flag.
func putLogfmtField(m map[string]jx.Raw, key, val string, quoted, hasVal bool) seenFields {
	if !hasVal {
		m[key] = jx.Raw("true")
		return 0
	}
	switch logfmtKeys[key] {
	case keyLevel:
		lvl, ok := normalizeLevel(val)
		if !ok {
			// A level pl cannot render (a custom token, or an empty one as
			// logrus writes when the level is unset). Keep a non-empty token as
			// a field — "level" itself is reserved and would be dropped — so its
			// value is not lost.
			if val != "" {
				m[keyLevelText] = rawString(val)
			}
			return 0
		}
		m[keyLevel] = rawString(lvl)
		return seenLevel
	case keyTime:
		m[keyTime] = logfmtValue(val, quoted)
		return seenTime
	case keyMessage:
		m[keyMessage] = rawString(val)
		return seenMessage
	case keyLogger:
		m[keyLogger] = rawString(val)
		return 0
	case keyCaller:
		m[keyCaller] = rawString(val)
		return 0
	case keyError:
		m[keyError] = rawString(val)
		return 0
	case keyStacktrace:
		m[keyStacktrace] = rawString(val)
		return 0
	case keyTraceID:
		if !isZeroHex(val) {
			m[keyTraceID] = rawString(val)
		}
		return 0
	case keySpanID:
		if !isZeroHex(val) {
			m[keySpanID] = rawString(val)
		}
		return 0
	}
	m[key] = logfmtValue(val, quoted)
	return 0
}

// logfmtValue converts a logfmt value into JSON, deducing numbers and bools from
// bare (unquoted) tokens so they render like their JSON counterparts.
func logfmtValue(val string, quoted bool) jx.Raw {
	if !quoted {
		if raw, ok := scalarRaw(val); ok {
			return raw
		}
	}
	return rawString(val)
}

// scalarRaw returns s as raw JSON when it is a JSON number or boolean literal.
func scalarRaw(s string) (jx.Raw, bool) {
	switch s {
	case "true", "false", "null":
		return jx.Raw(s), true
	}
	if s == "" {
		return nil, false
	}
	// ParseFloat accepts forms JSON does not (hex floats, Inf, NaN, underscores);
	// restrict the alphabet before trusting it.
	if strings.TrimLeft(s, "0123456789+-.eE") != "" {
		return nil, false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return nil, false
	}
	// JSON is stricter than ParseFloat about leading zeroes, a leading '+' and a
	// bare fraction ("+1", ".5", "1.", "01"), so let jx have the final say.
	if !jx.Valid([]byte(s)) {
		return nil, false
	}
	return jx.Raw(s), true
}

// normalizeLevel maps a level token from an arbitrary logger onto the zap level
// name parseLevel understands, reporting false for tokens it does not recognize.
// zap has no trace level, so trace collapses onto debug, as it does for OTEL
// severities.
func normalizeLevel(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "trace", "d", levelDebug, "verbose":
		return levelDebug, true
	case "i", levelInfo, "information", "notice":
		return levelInfo, true
	case "w", levelWarn, "warning":
		return levelWarn, true
	case "e", "err", levelError:
		return levelError, true
	case levelDPanic:
		return levelDPanic, true
	case levelPanic:
		return levelPanic, true
	case "f", levelFatal, "c", "crit", "critical", "alert", "emerg", "emergency":
		return levelFatal, true
	default:
		return "", false
	}
}

// scanLogfmt walks the key=value pairs of a logfmt line, calling fn for each.
// A key without '=' is reported with hasVal false (logfmt reads it as a true
// flag), and quoted reports whether the value came from a quoted string.
// Characters that cannot start a key are skipped as garbage, mirroring how
// logfmt decoders tolerate free-form text mixed into a line.
//
// It returns false when the line contains a malformed quoted value, i.e. it is
// not logfmt at all.
func scanLogfmt(s string, fn func(key, val string, quoted, hasVal bool)) bool {
	isSep := func(c byte) bool { return c == ' ' || c == '\t' }
	for i := 0; i < len(s); {
		for i < len(s) && (isSep(s[i]) || s[i] == '=' || s[i] == '"') {
			i++
		}
		start := i
		for i < len(s) && !isSep(s[i]) && s[i] != '=' {
			i++
		}
		key := s[start:i]
		if key == "" {
			continue
		}
		if i >= len(s) || s[i] != '=' {
			fn(key, "", false, false)
			continue
		}
		i++ // Consume '='.
		if i < len(s) && s[i] == '"' {
			quoted, err := strconv.QuotedPrefix(s[i:])
			if err != nil {
				return false
			}
			val, err := strconv.Unquote(quoted)
			if err != nil {
				return false
			}
			fn(key, val, true, true)
			i += len(quoted)
			continue
		}
		valStart := i
		for i < len(s) && !isSep(s[i]) {
			i++
		}
		fn(key, s[valStart:i], false, true)
	}
	return true
}
