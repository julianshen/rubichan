package tui

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/terminal"
)

// mermaidBlock represents a detected fenced Mermaid code block in content.
type mermaidBlock struct {
	start  int    // byte offset of opening ```mermaid
	end    int    // byte offset past the closing ```\n (or end of closing ```)
	source string // the Mermaid source between the fences
}

// detectMermaidBlocks finds all fenced ```mermaid...``` blocks in content.
func detectMermaidBlocks(content string) []mermaidBlock {
	var blocks []mermaidBlock
	search := 0
	for search < len(content) {
		// Find opening fence
		idx := strings.Index(content[search:], "```mermaid\n")
		if idx == -1 {
			break
		}
		fenceStart := search + idx
		srcStart := fenceStart + len("```mermaid\n")

		// Find closing fence
		closeIdx := strings.Index(content[srcStart:], "\n```")
		if closeIdx == -1 {
			break // unclosed block
		}
		srcEnd := srcStart + closeIdx
		fenceEnd := srcEnd + len("\n```")

		// Advance past closing fence (include trailing newline if present)
		if fenceEnd < len(content) && content[fenceEnd] == '\n' {
			fenceEnd++
		}

		blocks = append(blocks, mermaidBlock{
			start:  fenceStart,
			end:    fenceEnd,
			source: content[srcStart:srcEnd],
		})
		search = fenceEnd
	}
	return blocks
}

// mmdcOnce guards the one-time exec.LookPath("mmdc") check so it
// isn't called on every viewport render (which can be 60+ times/second).
var (
	mmdcOnce      sync.Once
	mmdcAvailable bool
)

func mmdcAvailableCached() bool {
	mmdcOnce.Do(func() {
		mmdcAvailable = terminal.MmdcAvailable()
	})
	return mmdcAvailable
}

// replaceMermaidBlocks replaces Mermaid code blocks with rendered inline images
// when Kitty graphics and mmdc are available. Returns content unchanged when
// rendering is not possible.
func replaceMermaidBlocks(content string, caps *terminal.Caps) string {
	if caps == nil || !caps.KittyGraphics {
		return content
	}

	// Detect blocks before the (cached) mmdc check to avoid even
	// that work when there are no Mermaid blocks.
	blocks := detectMermaidBlocks(content)
	if len(blocks) == 0 {
		return content
	}

	if !mmdcAvailableCached() {
		return content
	}

	var result strings.Builder
	prev := 0
	for _, b := range blocks {
		result.WriteString(content[prev:b.start])

		rendered := renderMermaidInline(caps, b.source)
		if rendered != "" {
			result.WriteString(rendered)
		} else {
			// Rendering failed — keep the original code block
			result.WriteString(content[b.start:b.end])
		}
		prev = b.end
	}
	result.WriteString(content[prev:])
	return result.String()
}

// renderMermaidInline renders a Mermaid diagram as a Kitty graphics string.
// Returns the rendered content string, or "" if rendering is not possible.
func renderMermaidInline(caps *terminal.Caps, mermaidSrc string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pngData, err := terminal.RenderMermaid(ctx, mermaidSrc, caps.DarkBackground)
	if err != nil {
		log.Printf("mermaid inline render failed: %v", err)
		return ""
	}

	var buf bytes.Buffer
	terminal.KittyImage(&buf, pngData)
	return buf.String()
}
