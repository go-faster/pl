# pl [![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8)](https://pkg.go.dev/github.com/go-faster/pl#section-documentation) [![codecov](https://img.shields.io/codecov/c/github/go-faster/pl?label=cover)](https://codecov.io/gh/go-faster/pl) [![experimental](https://img.shields.io/badge/-experimental-blueviolet)](https://go-faster.org/docs/projects/status#experimental)

`pl` tails and pretty-prints JSONL logs produced by [zap](https://github.com/uber-go/zap)
(the `zap.NewProductionConfig` JSON encoder, as used by [go-faster/sdk](https://github.com/go-faster/sdk)).

It also understands [OpenTelemetry](https://opentelemetry.io) log records in the
[logs data model](https://opentelemetry.io/docs/specs/otel/logs/data-model/) JSON
form (the `Severity`/`Body`/`Attributes`/`Scope` shape emitted by go-faster/sdk's
console log exporter). Both formats are detected per line and rendered the same
way, so a stream that mixes them — as oteldb emits — reads uniformly:

- `Body` becomes the message, the instrumentation `Scope` name the logger;
- `SeverityText` (or the numeric `SeverityNumber`) becomes the level;
- `Attributes` become `key=value` fields, with the `code.*` ones folded into the
  caller and `exception.message`/`exception.stacktrace` shown as the error;
- non-zero `TraceID`/`SpanID` are surfaced as `trace_id`/`span_id` for
  correlation, while resource attributes and zero ids are omitted as noise.

When go-faster/sdk's `zctx` runs in otelzap mode (`zctx.WithOpenTelemetryZap`),
zap lines carry the trace correlation as a reflected `ctx` object rather than
flat fields. `pl` flattens that object so `span_id`/`trace_id` (and any other
context-scoped members) read as ordinary fields — identical to zctx's default
mode — again dropping all-zero ids.

## Install

```bash
go install github.com/go-faster/pl/cmd/pl@latest
```

## Usage

Pipe logs in:

```bash
my-service 2>&1 | pl
```

Follow a file (`tail -f` style):

```bash
pl -f service.log
```

Read a file once and exit:

```bash
pl service.log
```

Output is colorized when stdout is a terminal. Lines in none of the formats below
are passed through untouched, so mixed output is safe. Disable colors with
`--no-color` or by setting `NO_COLOR`.

## Plain-text formats

Besides the two JSON formats, `pl` renders the plain-text logs that surround a
Kubernetes deployment, so `kubectl logs` output reads like the rest:

- **logfmt** (logrus, Grafana, Loki, `log15`, …) — `level`/`lvl`/`severity`,
  `t`/`ts`/`time`/`timestamp`, `msg`/`message`, `logger`, `caller`, `err`/`error`
  and the trace id keys map onto the same rendering as zap's; every other pair
  becomes a `key=value` field, with numbers and booleans kept as such. A line is
  only treated as logfmt when it carries at least two of level, timestamp and
  message — plain prose is passed through rather than mangled into fields.
  Text before the first pair — journalctl's `Jul 26 12:28:52 host unit[1063]:`
  prefix, say — is kept verbatim ahead of the entry instead of being scanned into
  fields.
- **klog** (kube-apiserver, kubelet, and everything else built on
  [`k8s.io/klog`](https://github.com/kubernetes/klog)) — the header supplies the
  level, timestamp, `thread.id` and caller; structured klog's quoted message and
  trailing `key="value"` pairs are parsed like logfmt, so `err=` shows up as the
  error. The year, which klog omits, is taken from the current date.

```console
$ kubectl logs kubelet | pl
01:22:17.141 E Startup probe already exists for container containerName=cilium-envoy pod=kube-system/cilium-envoy-vxgzg thread.id=1386 (prober_manager.go:197)
```

JSON lines from `log/slog`'s handler, which names its fields `time` and `msg`,
are rendered like zap's.

Timestamps in these formats are parsed from RFC3339 or from a numeric epoch,
whose unit (seconds, milliseconds, microseconds or nanoseconds) is deduced from
its magnitude.

Isolate a single trace with `--trace-id`; it keeps only lines whose `trace_id`
matches (case-insensitively), whichever format they arrive in — a flat zap
`trace_id`, zctx's reflected `ctx` object, or an OTEL `TraceID`:

```bash
pl --trace-id a30d8906e0e519424360816608e11188 service.log
```

### Flags

```
-f, --follow         follow the file, waiting for new lines (like tail -f)
    --no-color       disable ANSI colors
    --no-time        omit timestamps from the output
    --level          minimum level to display (debug|info|warn|error)
    --trace-id       show only lines whose trace_id matches (case-insensitive)
    --timezone       convert timestamps to this timezone (e.g. UTC, Local, America/New_York)
    --otel-resource  include OpenTelemetry resource attributes (OTEL logs only)
    --otel-func      include the function name in the caller (OTEL logs only)
    --no-stacktrace  omit the zap stacktrace block
    --no-error-verbose  omit the go-faster/errors verbose error block
    --no-stacks      omit both stack blocks
```

`--otel-resource` prints the resource attributes on their own indented lines
below the entry, in a color distinct from the inline fields, so they stay
readable instead of crowding the message:

```
20:02:07.563 I app ClickHouse disabled (oteldb/app.go:114)
	host.name=198f0b5f008f
	service.name=oteldb
	telemetry.sdk.version=1.44.0
```

### Level styles

Levels render as a single colored character by default — `D`, `I`, `W`, `E`,
and `C` for dpanic/panic/fatal:

```
03:00:00.099 D verbose detail
03:00:00.200 I metrics Starting attempt=3
03:00:00.299 W disk low note="needs attention"
03:00:00.400 E boom err=x
03:00:00.500 C giving up
```

When used as a library, override per-level label and color via
`Formatter.LevelStyles` (levels absent from the map keep their defaults):

```go
f := &pl.Formatter{
    Color: true,
    LevelStyles: map[zapcore.Level]pl.LevelStyle{
        zapcore.WarnLevel: {Label: "WARN", Color: "\033[33m"},
    },
}
```
