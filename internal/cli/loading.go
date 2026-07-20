package cli

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

const loadingFrameInterval = 80 * time.Millisecond

var loadingFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// loadingIndicator animates a single in-place status line on interactive
// terminals. Redirected output gets a durable plain-text status line instead.
type loadingIndicator struct {
	out      io.Writer
	message  string
	animated bool
	done     chan struct{}
	stopped  chan struct{}
	once     sync.Once
}

func (a *App) startLoading(message string) *loadingIndicator {
	out := a.Out
	if out == nil {
		out = io.Discard
	}
	return newLoadingIndicator(out, message, loadingAnimationEnabled(out), loadingFrameInterval)
}

// withLoading keeps error-returning operations concise and guarantees the
// terminal line and cursor are restored on success, failure, or cancellation.
func (a *App) withLoading(message string, operation func() error) error {
	indicator := a.startLoading(message)
	defer indicator.Stop()
	return operation()
}

func newLoadingIndicator(out io.Writer, message string, animated bool, interval time.Duration) *loadingIndicator {
	indicator := &loadingIndicator{out: out, message: message, animated: animated}
	if !animated {
		fmt.Fprintf(out, "  … %s\n", message)
		return indicator
	}

	indicator.done = make(chan struct{})
	indicator.stopped = make(chan struct{})
	fmt.Fprint(out, "\x1b[?25l") // hide the cursor while the spinner is active
	indicator.render(0)
	go indicator.animate(interval)
	return indicator
}

func (l *loadingIndicator) animate(interval time.Duration) {
	defer close(l.stopped)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	frame := 1
	for {
		select {
		case <-ticker.C:
			l.render(frame)
			frame = (frame + 1) % len(loadingFrames)
		case <-l.done:
			return
		}
	}
}

func (l *loadingIndicator) render(frame int) {
	fmt.Fprintf(l.out, "\r\x1b[2K  %s %s", loadingFrames[frame], l.message)
}

func (l *loadingIndicator) Stop() {
	l.once.Do(func() {
		if !l.animated {
			return
		}
		close(l.done)
		<-l.stopped
		fmt.Fprint(l.out, "\r\x1b[2K\x1b[?25h")
	})
}

func loadingAnimationEnabled(out io.Writer) bool {
	if os.Getenv("CI") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
