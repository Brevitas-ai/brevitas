package cli

import (
	"context"
	"flag"
	"io"
	"os"
	"time"
)

func (a *App) cmdLogs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	follow := fs.Bool("follow", false, "follow the log (like tail -f)")
	fs.BoolVar(follow, "f", false, "shorthand for --follow")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := a.Dirs.ProxyLog()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			a.say("No logs yet at %s", path)
			return nil
		}
		return err
	}
	defer f.Close()

	if _, err := io.Copy(a.Out, f); err != nil {
		return err
	}
	if !*follow {
		return nil
	}

	// Follow: poll for appended bytes until the context is cancelled.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		buf := make([]byte, 4096)
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = a.Out.Write(buf[:n])
			continue
		}
		if err != nil && err != io.EOF {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
