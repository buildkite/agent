package job

import (
	"regexp"
	"strings"
	"sync"

	"github.com/buildkite/agent/v4/internal/shell"
)

// maxOutputTail is how much recent child-process output outputTail retains,
// comfortably more than a captured error can include.
const maxOutputTail = 4 << 10

// ansiEscape matches terminal control sequences, such as colours, which make
// captured output harder to read outside a terminal.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// outputTail retains the most recent child-process output after secret
// redaction, so captured errors can include what a failed hook or command
// printed. Because the output was redacted as a whole stream, keeping only
// its end cannot expose part of a registered secret.
type outputTail struct {
	mu   sync.Mutex
	data []byte
}

func (t *outputTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if over := len(t.data) - maxOutputTail; over > 0 {
		t.data = append(t.data[:0], t.data[over:]...)
	}
	return len(p), nil
}

// reset discards output, so a later tail only includes what follows.
func (t *outputTail) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = t.data[:0]
}

// tail returns at most limit bytes of the most recent output as readable text,
// starting at a line boundary when the output had to be shortened.
func (t *outputTail) tail(limit int) string {
	t.mu.Lock()
	text := string(t.data)
	t.mu.Unlock()

	text = ansiEscape.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ToValidUTF8(text, "")
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return ""
	}
	if len(text) > limit {
		text = text[len(text)-limit:]
		// Drop the partial first line, and any partial UTF-8 sequence with it.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = strings.ToValidUTF8(text, "")
		}
	}
	return strings.TrimSpace(text)
}

// teeRecentOutput copies a command's output into the redacted output tail.
func (e *Executor) teeRecentOutput() shell.RunCommandOpt {
	if e.outputTailRedactor == nil {
		return shell.TeeOutput(nil)
	}
	return shell.TeeOutput(e.outputTailRedactor)
}
