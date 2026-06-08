package pl

import (
	"bufio"
	"context"
	"io"
	"os"
	"time"

	"github.com/go-faster/errors"
)

// pollInterval is how often follow mode checks a file for new data.
const pollInterval = 200 * time.Millisecond

// Process reads newline-delimited logs from r, formats each line with f and
// writes the result to w until r is exhausted or ctx is canceled.
func (f *Formatter) Process(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	// Allow long lines (stacktraces, large payloads): up to 8 MiB.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	bw := bufio.NewWriter(w)
	defer func() { _ = bw.Flush() }()

	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		out, ok := f.Format(sc.Bytes())
		if !ok {
			continue
		}
		if _, err := bw.WriteString(out); err != nil {
			return errors.Wrap(err, "write")
		}
		if err := bw.WriteByte('\n'); err != nil {
			return errors.Wrap(err, "write")
		}
		// Flush eagerly so piped output appears promptly.
		if err := bw.Flush(); err != nil {
			return errors.Wrap(err, "flush")
		}
	}
	if err := sc.Err(); err != nil {
		return errors.Wrap(err, "scan")
	}
	return nil
}

// Follow tails the file at path (like tail -f), formatting new lines as they
// are appended. It handles truncation/rotation by reopening when the file
// shrinks. It blocks until ctx is canceled.
func (f *Formatter) Follow(ctx context.Context, path string, w io.Writer) error {
	file, err := os.Open(path) // #nosec G304 -- path is a user-supplied log file to tail
	if err != nil {
		return errors.Wrap(err, "open")
	}
	defer func() { _ = file.Close() }()

	bw := bufio.NewWriter(w)
	defer func() { _ = bw.Flush() }()

	reader := bufio.NewReader(file)
	var pending []byte
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		line, err := reader.ReadBytes('\n')
		switch {
		case err == nil:
			full := line
			if len(pending) > 0 {
				full = append(pending, line...)
				pending = pending[:0]
			}
			out, ok := f.Format(full)
			if ok {
				if _, werr := bw.WriteString(out); werr != nil {
					return errors.Wrap(werr, "write")
				}
				_ = bw.WriteByte('\n')
				if ferr := bw.Flush(); ferr != nil {
					return errors.Wrap(ferr, "flush")
				}
			}
		case errors.Is(err, io.EOF):
			// Hold a partial line until its newline arrives.
			pending = append(pending, line...)
			if rotated, rerr := waitForData(ctx, file, ticker); rerr != nil {
				return rerr
			} else if rotated {
				if _, serr := file.Seek(0, io.SeekStart); serr != nil {
					return errors.Wrap(serr, "seek")
				}
				reader.Reset(file)
				pending = pending[:0]
			}
		default:
			return errors.Wrap(err, "read")
		}
	}
}

// waitForData blocks until the file has more data, ctx is canceled, or the
// file is truncated. It reports whether truncation/rotation was detected.
func waitForData(ctx context.Context, file *os.File, ticker *time.Ticker) (rotated bool, err error) {
	// Remember current position to detect truncation.
	pos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, errors.Wrap(err, "tell")
	}
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			info, err := file.Stat()
			if err != nil {
				return false, errors.Wrap(err, "stat")
			}
			if info.Size() < pos {
				// File was truncated; restart from the beginning.
				return true, nil
			}
			if info.Size() > pos {
				return false, nil
			}
		}
	}
}
