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

Output is colorized when stdout is a terminal. Non-JSON lines are passed through
untouched, so mixed output is safe. Disable colors with `--no-color` or by setting
`NO_COLOR`.

### Flags

```
-f, --follow         follow the file, waiting for new lines (like tail -f)
    --no-color       disable ANSI colors
    --no-time        omit timestamps from the output
    --level          minimum level to display (debug|info|warn|error)
    --timezone       convert timestamps to this timezone (e.g. UTC, Local, America/New_York)
    --otel-resource  include OpenTelemetry resource attributes (OTEL logs only)
    --otel-func      include the function name in the caller (OTEL logs only)
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
