package pl

import (
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"
)

// klogTimeFormat is the header timestamp klog writes: month, day, then the time
// of day with microseconds. The year is absent, so it is taken from the current
// date (see parseKLog).
const klogTimeFormat = "0102 15:04:05.999999"

// keyThreadID holds the klog header's thread (goroutine) id.
const keyThreadID = "thread.id"

// parseKLog parses a klog line — the Kubernetes glog-derived format used by
// kube-apiserver, kubelet and friends — into the zap-keyed map the formatter
// renders:
//
//	I0223 01:11:35.888571     903 cidrallocator.go:278] updated ClusterIP allocator
//	E0223 01:22:17.141045    1386 prober_manager.go:197] "Startup probe already exists" pod="kube-system/cilium-envoy-vxgzg"
//
// The header carries the level, timestamp, thread id and source location; the
// message is either the rest of the line verbatim or, for structured klog, a
// quoted message followed by logfmt key=value pairs.
//
// now supplies the year missing from the header. It reports false for lines that
// are not klog.
func parseKLog(line string, now time.Time) (map[string]jx.Raw, bool) {
	rest, h, ok := parseKLogHeader(line, now)
	if !ok {
		return nil, false
	}
	msg := strings.TrimSpace(rest)
	if msg == "" {
		return nil, false
	}

	m := make(map[string]jx.Raw, 8)
	m[keyLevel] = rawString(h.level)
	m[keyTime] = rawString(h.ts.Format(time.RFC3339Nano))
	if h.caller != "" {
		m[keyCaller] = rawString(h.caller)
	}
	if h.threadID != 0 {
		m[keyThreadID] = jx.Raw(strconv.FormatInt(h.threadID, 10))
	}

	if msg[0] != '"' {
		// Unstructured klog: the rest of the line is the message as-is.
		m[keyMessage] = rawString(msg)
		return m, true
	}

	quoted, err := strconv.QuotedPrefix(msg)
	if err != nil {
		return nil, false
	}
	unquoted, err := strconv.Unquote(quoted)
	if err != nil {
		return nil, false
	}
	m[keyMessage] = rawString(unquoted)
	if !scanLogfmt(msg[len(quoted):], func(key, val string, quoted, hasVal bool) {
		putLogfmtField(m, key, val, quoted, hasVal)
	}) {
		return nil, false
	}
	return m, true
}

// klogHeader is the fixed prefix of a klog line.
type klogHeader struct {
	level    string
	ts       time.Time
	threadID int64
	caller   string
}

// parseKLogHeader parses the klog header and returns the remainder of the line.
// See https://github.com/kubernetes/klog/blob/v2.130.1/klog.go for the layout.
func parseKLogHeader(s string, now time.Time) (rest string, h klogHeader, ok bool) {
	if len(s) < len(klogTimeFormat)+2 {
		return "", h, false
	}
	switch s[0] {
	case 'D':
		h.level = levelDebug
	case 'I':
		h.level = levelInfo
	case 'W':
		h.level = levelWarn
	case 'E':
		h.level = levelError
	case 'F':
		h.level = levelFatal
	default:
		return "", h, false
	}
	s = s[1:]

	t, err := time.ParseInLocation(klogTimeFormat, s[:len(klogTimeFormat)], now.Location())
	if err != nil {
		return "", h, false
	}
	// klog writes local time without a year; assume the log is from the current
	// one.
	h.ts = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), now.Location())
	s = s[len(klogTimeFormat):]

	head, rest, ok := strings.Cut(s, "]")
	if !ok {
		return "", h, false
	}
	// The remaining header is "<thread id> <file>:<line>"; a line missing it is
	// still klog, but anything else in its place is not.
	if head = strings.TrimSpace(head); head != "" {
		threadID, caller, ok := strings.Cut(head, " ")
		if !ok {
			return "", h, false
		}
		id, err := strconv.ParseInt(threadID, 10, 64)
		if err != nil {
			return "", h, false
		}
		if !isStackLocation(caller) {
			return "", h, false
		}
		h.threadID, h.caller = id, strings.TrimSpace(caller)
	}
	return rest, h, true
}
