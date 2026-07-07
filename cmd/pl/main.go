// Command pl tails and pretty-prints JSONL logs produced by zap
// (the zap.NewProductionConfig JSON encoder used by go-faster/sdk).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"

	"github.com/go-faster/pl"
)

func root() *cobra.Command {
	var (
		follow       bool
		noColor      bool
		noTime       bool
		level        string
		timezone     string
		traceID      string
		otelResource bool
		otelFunc     bool
	)

	cmd := &cobra.Command{
		Use:   "pl [file]",
		Short: "Tail and pretty-print zap JSONL logs",
		Long: "pl reads JSONL logs produced by zap (go-faster/sdk) and renders them\n" +
			"in a human-readable, colorized form.\n\n" +
			"With no argument it reads from stdin. With a file argument it reads the\n" +
			"file; pass --follow to keep watching for new lines (like tail -f).",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := &pl.Formatter{
				Color:        colorEnabled(noColor),
				NoTime:       noTime,
				TraceID:      traceID,
				OTELResource: otelResource,
				OTELFunc:     otelFunc,
			}
			if timezone != "" {
				loc, err := time.LoadLocation(timezone)
				if err != nil {
					return fmt.Errorf("invalid --timezone %q: %w", timezone, err)
				}
				f.Location = loc
			}
			if level != "" {
				var l zapcore.Level
				if err := l.UnmarshalText([]byte(level)); err != nil {
					return fmt.Errorf("invalid --level %q: %w", level, err)
				}
				f.SetMinLevel(l)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			if len(args) == 0 {
				if follow {
					return fmt.Errorf("--follow requires a file argument")
				}
				err := f.Process(ctx, os.Stdin, os.Stdout)
				if ctx.Err() != nil {
					return nil // interrupted, clean exit
				}
				return err
			}

			path := args[0]
			if follow {
				err := f.Follow(ctx, path, os.Stdout)
				if ctx.Err() != nil {
					return nil // interrupted, clean exit
				}
				return err
			}

			file, err := os.Open(path) // #nosec G304 -- path is a user-supplied log file to read
			if err != nil {
				return err
			}
			defer func() { _ = file.Close() }()
			err = f.Process(ctx, file, os.Stdout)
			if ctx.Err() != nil {
				return nil // interrupted, clean exit
			}
			return err
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the file, waiting for new lines (like tail -f)")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors")
	cmd.Flags().BoolVar(&noTime, "no-time", false, "omit timestamps from the output")
	cmd.Flags().StringVar(&level, "level", "", "minimum level to display (debug|info|warn|error)")
	cmd.Flags().StringVar(&traceID, "trace-id", "", "show only lines whose trace_id matches (case-insensitive)")
	cmd.Flags().StringVar(&timezone, "timezone", "", "convert timestamps to this timezone (e.g. UTC, Local, America/New_York)")
	cmd.Flags().BoolVar(&otelResource, "otel-resource", false, "include OpenTelemetry resource attributes as fields (OTEL logs only)")
	cmd.Flags().BoolVar(&otelFunc, "otel-func", false, "include the function name in the caller (OTEL logs only)")

	return cmd
}

// colorEnabled decides whether to colorize output: honor --no-color and the
// NO_COLOR convention, and only color when stdout is a terminal.
func colorEnabled(noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func main() {
	if err := root().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
