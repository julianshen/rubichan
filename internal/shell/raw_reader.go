package shell

import (
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

// inputMode represents the input mode (auto-detect, force shell, force query).
type inputMode int

const (
	modeAuto inputMode = iota
	modeShell
	modeQuery
)

// modeLabel returns the display label for the current mode.
func (m inputMode) label() string {
	switch m {
	case modeShell:
		return "[CMD] "
	case modeQuery:
		return "[ASK] "
	default:
		return ""
	}
}

// modePrefix returns the prefix to prepend when the user submits in forced mode.
func (m inputMode) prefix() string {
	switch m {
	case modeShell:
		return "!"
	case modeQuery:
		return "?"
	default:
		return ""
	}
}

// RawLineReader provides interactive line editing with tab completion and mode switching.
// It uses golang.org/x/term to put stdin in raw mode and handles character-by-character input.
type RawLineReader struct {
	fd         int
	completer  *Completer
	history    *CommandHistory
	historyIdx int
	mode       inputMode

	// Current line buffer and cursor position
	buf    []rune
	cursor int

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// Original terminal state
	oldState *term.State
}

// NewRawLineReader creates a new line reader with tab completion and raw mode editing.
func NewRawLineReader(completer *Completer) *RawLineReader {
	return NewRawLineReaderWithIO(completer, os.Stdin, os.Stdout, os.Stderr)
}

// NewRawLineReaderWithIO creates a new line reader with custom I/O.
func NewRawLineReaderWithIO(completer *Completer, stdin io.Reader, stdout, stderr io.Writer) *RawLineReader {
	// Try to get the file descriptor if stdin is a terminal
	fd := 0 // default stdin fd
	if f, ok := stdin.(*os.File); ok {
		fd = int(f.Fd())
	}

	return &RawLineReader{
		fd:        fd,
		completer: completer,
		history:   NewCommandHistory(1000),
		mode:      modeAuto,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}
}

// ReadLine reads a line with raw mode editing. Returns the line and any error.
// If an error occurs (like EOF), it's returned directly.
func (lr *RawLineReader) ReadLine(prompt string) (string, error) {
	// Check if stdin is a terminal
	if !term.IsTerminal(lr.fd) {
		// Fall back to simple line reading if not a terminal
		return lr.fallbackRead()
	}

	// Enter raw mode
	oldState, err := term.MakeRaw(lr.fd)
	if err != nil {
		return lr.fallbackRead()
	}
	defer term.Restore(lr.fd, oldState)

	lr.oldState = oldState
	lr.buf = []rune{}
	lr.cursor = 0

	// Render the initial prompt
	lr.renderPrompt(prompt)

	// Main input loop
	for {
		key, err := lr.readKey()
		if err != nil {
			if err == io.EOF {
				// On EOF, return what we have
				result := string(lr.buf)
				lr.buf = nil
				io.WriteString(lr.stdout, "\n")
				return result, io.EOF
			}
			return "", err
		}

		// Handle special keys
		if key.isControlChar() {
			handled := lr.handleControlKey(key, prompt)
			if handled == keyHandled {
				lr.renderPrompt(prompt)
				continue
			} else if handled == keySubmit {
				break
			}
		} else if key.char > 0 {
			// Regular character input
			lr.insertChar(key.char)
			lr.renderPrompt(prompt)
		}
	}

	// Submit the line
	result := string(lr.buf)
	if result != "" {
		lr.history.Add(result)
	}

	// Prepend mode prefix if in forced mode
	if lr.mode == modeShell || lr.mode == modeQuery {
		result = lr.mode.prefix() + result
	}

	lr.buf = nil
	io.WriteString(lr.stdout, "\n")
	return result, nil
}

// handleControlKey processes special keys. Returns keyResult.
func (lr *RawLineReader) handleControlKey(key keyEvent, prompt string) keyResult {
	switch key.code {
	case keyEnter:
		return keySubmit
	case keyBackspace:
		if lr.cursor > 0 {
			lr.cursor--
			lr.buf = append(lr.buf[:lr.cursor], lr.buf[lr.cursor+1:]...)
		}
	case keyDelete:
		if lr.cursor < len(lr.buf) {
			lr.buf = append(lr.buf[:lr.cursor], lr.buf[lr.cursor+1:]...)
		}
	case keyTab:
		lr.handleTab()
	case keyControlA:
		lr.cursor = 0
	case keyControlE:
		lr.cursor = len(lr.buf)
	case keyControlU:
		// Clear line
		lr.buf = []rune{}
		lr.cursor = 0
	case keyControlW:
		// Delete word backwards
		if lr.cursor > 0 {
			// Skip whitespace
			for lr.cursor > 0 && lr.buf[lr.cursor-1] == ' ' {
				lr.cursor--
			}
			// Delete word
			for lr.cursor > 0 && lr.buf[lr.cursor-1] != ' ' {
				lr.cursor--
			}
			lr.buf = append(lr.buf[:lr.cursor], lr.buf[lr.cursor+len(lr.buf):]...)
		}
	case keyControlC:
		// Interrupt (Ctrl+C) — clear input and stay in prompt
		lr.buf = []rune{}
		lr.cursor = 0
	case keyArrowUp:
		entries := lr.history.Entries()
		lr.historyIdx++
		if lr.historyIdx > len(entries) {
			lr.historyIdx = len(entries)
		}
		if lr.historyIdx > 0 && len(entries) > 0 {
			item := entries[len(entries)-lr.historyIdx]
			lr.buf = []rune(item)
			lr.cursor = len(lr.buf)
		}
	case keyArrowDown:
		entries := lr.history.Entries()
		if lr.historyIdx > 0 && len(entries) > 0 {
			lr.historyIdx--
			if lr.historyIdx == 0 {
				lr.buf = []rune{}
				lr.cursor = 0
			} else {
				item := entries[len(entries)-lr.historyIdx]
				lr.buf = []rune(item)
				lr.cursor = len(lr.buf)
			}
		}
	case keyArrowLeft:
		if lr.cursor > 0 {
			lr.cursor--
		}
	case keyArrowRight:
		if lr.cursor < len(lr.buf) {
			lr.cursor++
		}
	case keyHome:
		lr.cursor = 0
	case keyEnd:
		lr.cursor = len(lr.buf)
	case keyEscape:
		// Clear input on Escape
		lr.buf = []rune{}
		lr.cursor = 0
	}

	return keyNotHandled
}

// handleTab processes Tab key for mode switching and completion.
func (lr *RawLineReader) handleTab() {
	// If buffer is empty, try to get completions
	if len(lr.buf) == 0 {
		return
	}

	// If buffer is non-empty, cycle mode on Tab
	if len(lr.buf) > 0 {
		lr.mode = (lr.mode + 1) % 3
		return
	}
}

// insertChar inserts a character at the cursor position.
func (lr *RawLineReader) insertChar(ch rune) {
	if lr.cursor >= len(lr.buf) {
		lr.buf = append(lr.buf, ch)
	} else {
		lr.buf = append(lr.buf[:lr.cursor+1], lr.buf[lr.cursor:]...)
		lr.buf[lr.cursor] = ch
	}
	lr.cursor++
}

// renderPrompt renders the current prompt and buffer with cursor.
func (lr *RawLineReader) renderPrompt(prompt string) {
	// Clear line
	io.WriteString(lr.stdout, "\r\033[K")

	// Write prompt with mode indicator
	fullPrompt := lr.mode.label() + prompt
	io.WriteString(lr.stdout, fullPrompt)

	// Write buffer up to cursor
	if lr.cursor > 0 {
		io.WriteString(lr.stdout, string(lr.buf[:lr.cursor]))
	}

	// Write buffer after cursor
	if lr.cursor < len(lr.buf) {
		io.WriteString(lr.stdout, string(lr.buf[lr.cursor:]))
		// Move cursor back to the right position
		for i := 0; i < len(lr.buf)-lr.cursor; i++ {
			io.WriteString(lr.stdout, "\033[D")
		}
	}
}

// readKey reads a single key event. Returns keyEvent and any error.
func (lr *RawLineReader) readKey() (keyEvent, error) {
	buf := make([]byte, 1)
	_, err := lr.stdin.Read(buf)
	if err != nil {
		return keyEvent{}, err
	}

	b := buf[0]

	// Check for escape sequences
	if b == 0x1b { // ESC
		return lr.readEscapeSequence()
	}

	// Control characters
	if b == '\r' || b == '\n' {
		return keyEvent{code: keyEnter}, nil
	}
	if b == '\t' {
		return keyEvent{code: keyTab}, nil
	}
	if b == 0x08 || b == 0x7f { // Backspace
		return keyEvent{code: keyBackspace}, nil
	}
	if b == 0x01 {
		return keyEvent{code: keyControlA}, nil
	}
	if b == 0x05 {
		return keyEvent{code: keyControlE}, nil
	}
	if b == 0x15 {
		return keyEvent{code: keyControlU}, nil
	}
	if b == 0x17 {
		return keyEvent{code: keyControlW}, nil
	}
	if b == 0x03 {
		return keyEvent{code: keyControlC}, nil
	}
	if b == 0x1b {
		return keyEvent{code: keyEscape}, nil
	}

	// Regular UTF-8 character
	if b < 0x80 {
		return keyEvent{char: rune(b)}, nil
	}

	// Multi-byte UTF-8 — read additional bytes
	n := utf8.RuneLen(rune(b))
	if n < 0 {
		return keyEvent{char: rune(b)}, nil
	}

	rest := make([]byte, n-1)
	_, err = io.ReadFull(lr.stdin, rest)
	if err != nil {
		return keyEvent{char: rune(b)}, nil
	}

	r, _ := utf8.DecodeRune(append([]byte{b}, rest...))
	return keyEvent{char: r}, nil
}

// readEscapeSequence handles escape sequences like arrow keys.
func (lr *RawLineReader) readEscapeSequence() (keyEvent, error) {
	buf := make([]byte, 2)
	_, err := io.ReadFull(lr.stdin, buf)
	if err != nil {
		return keyEvent{}, err
	}

	// Check for CSI sequences (e.g., [A for up arrow)
	if buf[0] == '[' {
		switch buf[1] {
		case 'A':
			return keyEvent{code: keyArrowUp}, nil
		case 'B':
			return keyEvent{code: keyArrowDown}, nil
		case 'C':
			return keyEvent{code: keyArrowRight}, nil
		case 'D':
			return keyEvent{code: keyArrowLeft}, nil
		case 'H':
			return keyEvent{code: keyHome}, nil
		case 'F':
			return keyEvent{code: keyEnd}, nil
		case '3':
			// Delete key (CSI 3 ~)
			nextBuf := make([]byte, 1)
			io.ReadFull(lr.stdin, nextBuf)
			if nextBuf[0] == '~' {
				return keyEvent{code: keyDelete}, nil
			}
		}
	}

	return keyEvent{}, nil
}

// fallbackRead falls back to simple line reading (non-raw mode).
func (lr *RawLineReader) fallbackRead() (string, error) {
	buf := make([]byte, 4096)
	n, err := lr.stdin.Read(buf)
	if err != nil {
		return "", err
	}

	// Remove trailing newline
	line := string(buf[:n])
	if line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line, nil
}

// Close cleans up resources.
func (lr *RawLineReader) Close() error {
	if lr.oldState != nil {
		return term.Restore(lr.fd, lr.oldState)
	}
	return nil
}

// HandlesPrompt returns true since RawLineReader renders the prompt itself.
func (lr *RawLineReader) HandlesPrompt() bool {
	return true
}

// Key event types and codes
type keyCode int

const (
	keyNone keyCode = iota
	keyEnter
	keyBackspace
	keyDelete
	keyTab
	keyControlA
	keyControlE
	keyControlU
	keyControlW
	keyControlC
	keyArrowUp
	keyArrowDown
	keyArrowLeft
	keyArrowRight
	keyHome
	keyEnd
	keyEscape
)

type keyEvent struct {
	code keyCode
	char rune
}

func (k keyEvent) isControlChar() bool {
	return k.code != keyNone || (k.char > 0 && k.char < 0x20)
}

type keyResult int

const (
	keyNotHandled keyResult = iota
	keyHandled
	keySubmit
)
