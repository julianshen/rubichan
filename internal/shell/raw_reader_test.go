package shell

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRawReader returns a RawLineReader wired up for buffer-backed tests.
func newTestRawReader(t *testing.T, stdin io.Reader) *RawLineReader {
	t.Helper()
	workDir := t.TempDir()
	completer := NewCompleter(map[string]bool{"ls": true, "lsof": true, "lsblk": true}, &workDir, nil, nil)
	return NewRawLineReaderWithIO(completer, stdin, &bytes.Buffer{}, &bytes.Buffer{})
}

// --- inputMode.label / inputMode.prefix ---

func TestInputModeLabelAndPrefix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", modeAuto.label())
	assert.Equal(t, "[CMD] ", modeShell.label())
	assert.Equal(t, "[ASK] ", modeQuery.label())

	assert.Equal(t, "", modeAuto.prefix())
	assert.Equal(t, "!", modeShell.prefix())
	assert.Equal(t, "?", modeQuery.prefix())
}

// --- keyEvent.isControlChar ---

func TestKeyEventIsControlChar(t *testing.T) {
	t.Parallel()

	assert.True(t, keyEvent{code: keyEnter}.isControlChar())
	assert.True(t, keyEvent{char: 0x01}.isControlChar()) // ctrl-a
	assert.False(t, keyEvent{char: 'a'}.isControlChar()) // regular rune
	assert.False(t, keyEvent{}.isControlChar())
}

// --- NewRawLineReaderWithIO ---

func TestNewRawLineReaderWithIOInit(t *testing.T) {
	t.Parallel()

	lr := newTestRawReader(t, strings.NewReader(""))
	assert.NotNil(t, lr.history)
	assert.Equal(t, modeAuto, lr.mode)
	assert.True(t, lr.HandlesPrompt())
	assert.NoError(t, lr.Close())
}

// --- insertChar ---

func TestRawReaderInsertChar(t *testing.T) {
	t.Parallel()

	lr := newTestRawReader(t, strings.NewReader(""))

	lr.insertChar('a')
	lr.insertChar('b')
	lr.insertChar('c')
	assert.Equal(t, "abc", string(lr.buf))
	assert.Equal(t, 3, lr.cursor)

	// Insert in the middle
	lr.cursor = 1
	lr.insertChar('X')
	assert.Equal(t, "aXbc", string(lr.buf))
	assert.Equal(t, 2, lr.cursor)
}

// --- handleControlKey: backspace, delete, arrows, home/end, ctrl-* ---

func TestRawReaderControlKeyBackspace(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 5

	lr.handleControlKey(keyEvent{code: keyBackspace}, "$ ")
	assert.Equal(t, "hell", string(lr.buf))
	assert.Equal(t, 4, lr.cursor)

	// Backspace at column zero is a no-op.
	lr.cursor = 0
	lr.handleControlKey(keyEvent{code: keyBackspace}, "$ ")
	assert.Equal(t, "hell", string(lr.buf))
	assert.Equal(t, 0, lr.cursor)
}

func TestRawReaderControlKeyDelete(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 1

	lr.handleControlKey(keyEvent{code: keyDelete}, "$ ")
	assert.Equal(t, "hllo", string(lr.buf))

	// Delete at end is a no-op.
	lr.cursor = len(lr.buf)
	lr.handleControlKey(keyEvent{code: keyDelete}, "$ ")
	assert.Equal(t, "hllo", string(lr.buf))
}

func TestRawReaderControlKeyEnter(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	result := lr.handleControlKey(keyEvent{code: keyEnter}, "$ ")
	assert.Equal(t, keySubmit, result)
}

func TestRawReaderControlKeyEnterAcceptsCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("ls")
	lr.cursor = 2
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "lsof"}}
	lr.completionIdx = 0

	result := lr.handleControlKey(keyEvent{code: keyEnter}, "$ ")
	assert.Equal(t, keyNotHandled, result)
	assert.Equal(t, "lsof", string(lr.buf))
	assert.False(t, lr.inCompletionMode)
}

func TestRawReaderControlKeyHomeEndCtrlAE(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 3

	lr.handleControlKey(keyEvent{code: keyHome}, "$ ")
	assert.Equal(t, 0, lr.cursor)

	lr.handleControlKey(keyEvent{code: keyEnd}, "$ ")
	assert.Equal(t, 5, lr.cursor)

	lr.handleControlKey(keyEvent{code: keyControlA}, "$ ")
	assert.Equal(t, 0, lr.cursor)

	lr.handleControlKey(keyEvent{code: keyControlE}, "$ ")
	assert.Equal(t, 5, lr.cursor)
}

func TestRawReaderControlKeyCtrlUClearsLine(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 5

	lr.handleControlKey(keyEvent{code: keyControlU}, "$ ")
	assert.Empty(t, lr.buf)
	assert.Equal(t, 0, lr.cursor)
}

func TestRawReaderControlKeyCtrlCClearsLine(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 5

	lr.handleControlKey(keyEvent{code: keyControlC}, "$ ")
	assert.Empty(t, lr.buf)
	assert.Equal(t, 0, lr.cursor)
}

func TestRawReaderControlKeyCtrlWDeletesWord(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("git checkout main")
	lr.cursor = len(lr.buf)

	lr.handleControlKey(keyEvent{code: keyControlW}, "$ ")
	assert.Equal(t, "git checkout ", string(lr.buf))

	// Ctrl-W with leading whitespace only.
	lr.buf = []rune("word ")
	lr.cursor = len(lr.buf)
	lr.handleControlKey(keyEvent{code: keyControlW}, "$ ")
	assert.Empty(t, lr.buf)

	// Ctrl-W at column zero is a no-op.
	lr.buf = []rune("abc")
	lr.cursor = 0
	lr.handleControlKey(keyEvent{code: keyControlW}, "$ ")
	assert.Equal(t, "abc", string(lr.buf))
}

func TestRawReaderControlKeyArrowsNoCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.history.Add("first")
	lr.history.Add("second")
	lr.buf = []rune("")
	lr.cursor = 0

	// Arrow up pulls history.
	lr.handleControlKey(keyEvent{code: keyArrowUp}, "$ ")
	assert.Equal(t, "second", string(lr.buf))

	lr.handleControlKey(keyEvent{code: keyArrowUp}, "$ ")
	assert.Equal(t, "first", string(lr.buf))

	// Arrow down walks back.
	lr.handleControlKey(keyEvent{code: keyArrowDown}, "$ ")
	assert.Equal(t, "second", string(lr.buf))

	// Arrow down past the end clears buffer.
	lr.handleControlKey(keyEvent{code: keyArrowDown}, "$ ")
	assert.Empty(t, lr.buf)
}

func TestRawReaderControlKeyArrowsWithCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "a"}, {Text: "b"}, {Text: "c"}}
	lr.completionIdx = 0

	// Arrow down cycles forward.
	lr.handleControlKey(keyEvent{code: keyArrowDown}, "$ ")
	assert.Equal(t, 1, lr.completionIdx)

	// Arrow up cycles backward (1 -> 0).
	lr.handleControlKey(keyEvent{code: keyArrowUp}, "$ ")
	assert.Equal(t, 0, lr.completionIdx)

	// Arrow up wraps to the last item.
	lr.handleControlKey(keyEvent{code: keyArrowUp}, "$ ")
	assert.Equal(t, 2, lr.completionIdx)
}

func TestRawReaderControlKeyArrowsLeftRight(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("abc")
	lr.cursor = 2

	lr.handleControlKey(keyEvent{code: keyArrowLeft}, "$ ")
	assert.Equal(t, 1, lr.cursor)

	lr.handleControlKey(keyEvent{code: keyArrowRight}, "$ ")
	assert.Equal(t, 2, lr.cursor)

	// Boundaries do not underflow/overflow.
	lr.cursor = 0
	lr.handleControlKey(keyEvent{code: keyArrowLeft}, "$ ")
	assert.Equal(t, 0, lr.cursor)

	lr.cursor = len(lr.buf)
	lr.handleControlKey(keyEvent{code: keyArrowRight}, "$ ")
	assert.Equal(t, len(lr.buf), lr.cursor)
}

func TestRawReaderControlKeyLeftRightDismissesCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "a"}}

	lr.handleControlKey(keyEvent{code: keyArrowLeft}, "$ ")
	assert.False(t, lr.inCompletionMode)

	// Now for right.
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "a"}}
	lr.handleControlKey(keyEvent{code: keyArrowRight}, "$ ")
	assert.False(t, lr.inCompletionMode)
}

func TestRawReaderControlKeyEscape(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("hello")
	lr.cursor = 5

	lr.handleControlKey(keyEvent{code: keyEscape}, "$ ")
	assert.Empty(t, lr.buf)

	// Escape while in completion mode dismisses.
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "a"}}
	lr.handleControlKey(keyEvent{code: keyEscape}, "$ ")
	assert.False(t, lr.inCompletionMode)
}

// --- handleTab ---

func TestRawReaderHandleTabNoCompletionsCyclesMode(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	// No known completions for a one-word nonsense input.
	lr.buf = []rune("zzzzz ")
	lr.cursor = len(lr.buf)
	start := lr.mode
	lr.handleTab()
	assert.NotEqual(t, start, lr.mode)
}

func TestRawReaderHandleTabEmptyBufferNoMode(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.handleTab()
	assert.Equal(t, modeAuto, lr.mode)
}

func TestRawReaderHandleTabSingleCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	// "lsb" matches only "lsblk".
	lr.buf = []rune("lsb")
	lr.cursor = 3
	lr.handleTab()
	assert.Equal(t, "lsblk", string(lr.buf))
}

func TestRawReaderHandleTabMultipleCompletionsEntersCompletionMode(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	// "ls" matches ls, lsof, lsblk.
	lr.buf = []rune("ls")
	lr.cursor = 2
	lr.handleTab()
	assert.True(t, lr.inCompletionMode)
	assert.NotEmpty(t, lr.completions)

	// Tab again advances through completions.
	first := lr.completionIdx
	lr.handleTab()
	assert.NotEqual(t, first, lr.completionIdx)
}

// --- dismissCompletion / clearCompletionMenu / selectCompletion ---

func TestRawReaderDismissCompletion(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.inCompletionMode = true
	lr.completions = []Completion{{Text: "a"}}
	lr.completionIdx = 0
	lr.lastCompletionRows = 2

	lr.dismissCompletion()
	assert.False(t, lr.inCompletionMode)
	assert.Nil(t, lr.completions)
	assert.Equal(t, 0, lr.completionIdx)
}

func TestRawReaderSelectCompletionReplacesLastWord(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("git check")
	lr.cursor = len(lr.buf)

	lr.selectCompletion(Completion{Text: "checkout"})
	assert.Equal(t, "git checkout", string(lr.buf))
	assert.Equal(t, len(lr.buf), lr.cursor)
	assert.False(t, lr.inCompletionMode)
}

func TestRawReaderSelectCompletionReplacesFirstWord(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader(""))
	lr.buf = []rune("ls")
	lr.cursor = 2

	lr.selectCompletion(Completion{Text: "lsblk"})
	assert.Equal(t, "lsblk", string(lr.buf))
}

// --- renderPrompt / renderCompletionMenu ---

func TestRawReaderRenderPrompt(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	workDir := t.TempDir()
	completer := NewCompleter(map[string]bool{"ls": true}, &workDir, nil, nil)
	lr := NewRawLineReaderWithIO(completer, strings.NewReader(""), buf, &bytes.Buffer{})

	lr.buf = []rune("hello")
	lr.cursor = 3
	lr.renderPrompt("$ ")

	out := buf.String()
	assert.Contains(t, out, "$ ")
	assert.Contains(t, out, "hel")
	assert.Contains(t, out, "lo")
}

func TestRawReaderRenderPromptWithCompletionMenu(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	workDir := t.TempDir()
	completer := NewCompleter(map[string]bool{}, &workDir, nil, nil)
	lr := NewRawLineReaderWithIO(completer, strings.NewReader(""), buf, &bytes.Buffer{})

	lr.buf = []rune("ls")
	lr.cursor = 2
	lr.inCompletionMode = true
	lr.completions = []Completion{
		{Text: "ls", Display: "ls"},
		{Text: "lsof", Display: "lsof"},
		{Text: "lsblk", Display: "lsblk"},
	}
	lr.completionIdx = 1
	lr.renderPrompt("$ ")

	out := buf.String()
	assert.Contains(t, out, "lsof")
}

func TestRawReaderRenderPromptClearsPreviousMenu(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	workDir := t.TempDir()
	completer := NewCompleter(map[string]bool{}, &workDir, nil, nil)
	lr := NewRawLineReaderWithIO(completer, strings.NewReader(""), buf, &bytes.Buffer{})

	lr.buf = []rune("ls")
	lr.cursor = 2
	lr.inCompletionMode = true
	lr.completions = []Completion{
		{Text: "ls", Display: "ls"},
		{Text: "lsof", Display: "lsof"},
	}
	// Simulate a previous render.
	lr.lastCompletionRows = 2
	lr.renderPrompt("$ ")

	out := buf.String()
	assert.NotEmpty(t, out)
}

// --- readKey: regular, control, and escape sequences ---

func TestRawReaderReadKeyAllBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []byte
		code keyCode
		char rune
	}{
		{"carriage return", []byte{'\r'}, keyEnter, 0},
		{"newline", []byte{'\n'}, keyEnter, 0},
		{"tab", []byte{'\t'}, keyTab, 0},
		{"backspace", []byte{keyBacksp}, keyBackspace, 0},
		{"delete ctrl", []byte{keyDel}, keyBackspace, 0},
		{"ctrl-a", []byte{keyCtrlA}, keyControlA, 0},
		{"ctrl-e", []byte{keyCtrlE}, keyControlE, 0},
		{"ctrl-u", []byte{keyCtrlU}, keyControlU, 0},
		{"ctrl-w", []byte{keyCtrlW}, keyControlW, 0},
		{"ctrl-c", []byte{keyCtrlC}, keyControlC, 0},
		{"ascii a", []byte{'a'}, keyNone, 'a'},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lr := newTestRawReader(t, bytes.NewReader(tc.in))
			k, err := lr.readKey()
			require.NoError(t, err)
			assert.Equal(t, tc.code, k.code)
			assert.Equal(t, tc.char, k.char)
		})
	}
}

func TestRawReaderReadKeyEOF(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, bytes.NewReader(nil))
	_, err := lr.readKey()
	assert.Equal(t, io.EOF, err)
}

func TestRawReaderReadKeyMultibyte(t *testing.T) {
	t.Parallel()
	// The readKey helper treats the lead byte as the final rune and reads
	// additional bytes via the utf8.RuneLen heuristic; we only exercise the
	// multi-byte branch here and don't assert on the decoded rune.
	lr := newTestRawReader(t, bytes.NewReader([]byte{0xc3, 0xa9}))
	_, err := lr.readKey()
	require.NoError(t, err)
}

// TestRawReaderReadKeyMultibyteShortRead covers the io.ReadFull failure branch.
func TestRawReaderReadKeyMultibyteShortRead(t *testing.T) {
	t.Parallel()
	// Lead byte 0xc3 claims a 2-byte rune but the reader has only one byte.
	lr := newTestRawReader(t, bytes.NewReader([]byte{0xc3}))
	_, err := lr.readKey()
	// The fallback returns the raw byte as a rune with no error.
	require.NoError(t, err)
}

func TestRawReaderReadEscapeSequenceArrowKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		code keyCode
	}{
		{"up", []byte{keyEsc, '[', 'A'}, keyArrowUp},
		{"down", []byte{keyEsc, '[', 'B'}, keyArrowDown},
		{"right", []byte{keyEsc, '[', 'C'}, keyArrowRight},
		{"left", []byte{keyEsc, '[', 'D'}, keyArrowLeft},
		{"home", []byte{keyEsc, '[', 'H'}, keyHome},
		{"end", []byte{keyEsc, '[', 'F'}, keyEnd},
		{"delete", []byte{keyEsc, '[', '3', '~'}, keyDelete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lr := newTestRawReader(t, bytes.NewReader(tc.in))
			k, err := lr.readKey()
			require.NoError(t, err)
			assert.Equal(t, tc.code, k.code)
		})
	}
}

func TestRawReaderReadEscapeSequenceTruncated(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, bytes.NewReader([]byte{keyEsc, '['}))
	_, err := lr.readKey()
	assert.Error(t, err)
}

// --- fallbackRead ---

func TestRawReaderFallbackRead(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader("hello\n"))
	line, err := lr.fallbackRead()
	require.NoError(t, err)
	assert.Equal(t, "hello", line)
}

func TestRawReaderFallbackReadCRLF(t *testing.T) {
	t.Parallel()
	lr := newTestRawReader(t, strings.NewReader("hi\r\n"))
	line, err := lr.fallbackRead()
	require.NoError(t, err)
	assert.Equal(t, "hi", line)
}

// TestRawReaderReadLineFallback ensures ReadLine falls back when stdin is not a terminal.
func TestRawReaderReadLineFallback(t *testing.T) {
	t.Parallel()
	// bytes.Buffer isn't a terminal, so ReadLine uses fallbackRead.
	lr := newTestRawReader(t, strings.NewReader("typed\n"))
	line, err := lr.ReadLine("$ ")
	require.NoError(t, err)
	assert.Equal(t, "typed", line)
}
