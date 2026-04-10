package tui

import (
	"bytes"
	"context"
	"os"
	"time"

	"github.com/julianshen/rubichan/internal/terminal"
)

// renderMermaidInline attempts to render a Mermaid diagram as an inline image.
// Returns true and writes the image to stderr if successful.
// Returns false if rendering is not available (no mmdc, no Kitty graphics).
func renderMermaidInline(caps *terminal.Caps, mermaidSrc string) bool {
	if caps == nil || !caps.KittyGraphics || !terminal.MmdcAvailable() {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pngData, err := terminal.RenderMermaid(ctx, mermaidSrc, caps.DarkBackground)
	if err != nil {
		return false
	}

	var buf bytes.Buffer
	terminal.KittyImage(&buf, pngData)
	buf.WriteTo(os.Stderr) //nolint:errcheck
	return true
}
