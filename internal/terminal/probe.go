package terminal

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// StdioProber queries a terminal via escape sequences to detect capabilities.
type StdioProber struct {
	r       io.Reader
	w       io.Writer
	timeout time.Duration
}

// NewStdioProber constructs a StdioProber that communicates over r/w.
func NewStdioProber(r io.Reader, w io.Writer, timeout time.Duration) *StdioProber {
	return &StdioProber{r: r, w: w, timeout: timeout}
}

// ProbeBackground sends an OSC 11 query and interprets the response as dark or light.
// Returns (isDark, supported); supported is false when the terminal doesn't respond.
func (p *StdioProber) ProbeBackground() (isDark bool, supported bool) {
	fmt.Fprint(p.w, "\x1b]11;?\x1b\\")
	resp, ok := p.readWithTimeout()
	if !ok || resp == "" {
		return false, false
	}
	isDark = p.parseBackgroundResponse(resp)
	return isDark, true
}

// ProbeSyncRendering sends a DECRQM query for mode 2026 and checks for acknowledgement.
func (p *StdioProber) ProbeSyncRendering() bool {
	fmt.Fprint(p.w, "\x1b[?2026$p")
	resp, ok := p.readWithTimeout()
	if !ok {
		return false
	}
	return strings.Contains(resp, "2026") && strings.Contains(resp, "$y")
}

// ProbeKittyKeyboard sends the kitty keyboard query and checks for the expected response shape.
func (p *StdioProber) ProbeKittyKeyboard() bool {
	fmt.Fprint(p.w, "\x1b[?u")
	resp, ok := p.readWithTimeout()
	if !ok {
		return false
	}
	return strings.Contains(resp, "?") && strings.HasSuffix(strings.TrimSpace(resp), "u")
}

// readWithTimeout reads a response from the terminal, timing out after p.timeout.
// Note: if the timeout fires, the goroutine reading from p.r will leak until the
// process exits. This is acceptable because Detect() is called once at startup
// and at most 3 goroutines can leak (one per probe type).
func (p *StdioProber) readWithTimeout() (string, bool) {
	type result struct {
		data string
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := p.r.Read(buf)
		ch <- result{data: string(buf[:n])}
	}()

	select {
	case res := <-ch:
		return res.data, true
	case <-time.After(p.timeout):
		return "", false
	}
}

// parseBackgroundResponse parses an OSC 11 response like "rgb:RRRR/GGGG/BBBB"
// and returns true when the average component value is below the dark threshold.
func (p *StdioProber) parseBackgroundResponse(resp string) bool {
	idx := strings.Index(resp, "rgb:")
	if idx == -1 {
		return true // default to dark
	}
	rgb := resp[idx+4:]
	parts := strings.SplitN(rgb, "/", 3)
	if len(parts) != 3 {
		return true
	}
	r := parseHexComponent(parts[0])
	g := parseHexComponent(parts[1])
	b := parseHexComponent(parts[2])
	avg := (r + g + b) / 3
	return avg < 0x8000
}

// parseHexComponent extracts up to 4 hex digits from s, stripping surrounding
// non-hex characters (e.g., trailing escape sequences from terminal responses).
func parseHexComponent(s string) int64 {
	clean := strings.TrimFunc(s, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
	})
	if len(clean) > 4 {
		clean = clean[:4]
	}
	v, err := strconv.ParseInt(clean, 16, 64)
	if err != nil {
		return 0
	}
	return v
}
