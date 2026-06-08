# pl [![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8)](https://pkg.go.dev/github.com/go-faster/pl#section-documentation) [![codecov](https://img.shields.io/codecov/c/github/go-faster/pl?label=cover)](https://codecov.io/gh/go-faster/pl) [![experimental](https://img.shields.io/badge/-experimental-blueviolet)](https://go-faster.org/docs/projects/status#experimental)

`pl` tails and pretty-prints JSONL logs produced by [zap](https://github.com/uber-go/zap)
(the `zap.NewProductionConfig` JSON encoder, as used by [go-faster/sdk](https://github.com/go-faster/sdk)).

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
-f, --follow      follow the file, waiting for new lines (like tail -f)
    --no-color    disable ANSI colors
    --no-time     omit timestamps from the output
    --level       minimum level to display (debug|info|warn|error)
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
