package persona

import (
	"fmt"
	"sync/atomic"
)

// Persona defines the interface for customizable TUI personality messages.
// Each method returns a display string used in the corresponding UI context.
type Persona interface {
	// ThinkingMessage returns the spinner text shown during streaming.
	ThinkingMessage() string
	// WelcomeMessage returns the TUI startup banner subtitle.
	WelcomeMessage() string
	// GoodbyeMessage returns the quit message.
	GoodbyeMessage() string
	// ErrorMessage returns a personality-flavored error message.
	ErrorMessage(err string) string
	// SuccessMessage returns the completion message after a successful turn.
	SuccessMessage() string
	// StatusPrefix returns the personality prefix for the status bar.
	StatusPrefix() string
	// ApprovalAsk returns the tool approval prompt text.
	ApprovalAsk(tool string) string
}

// active holds the currently active persona. Defaults to Ruby.
var active atomic.Value

func init() {
	active.Store(&RubyPersona{})
}

// Active returns the currently active persona.
func Active() Persona {
	return active.Load().(Persona)
}

// SetActive sets the active persona for the TUI.
func SetActive(p Persona) {
	active.Store(p)
}

// BaseSystemPrompt returns the core operational instructions shared by all
// persona layers.
func BaseSystemPrompt() string {
	return "You are a coding assistant. You can read and write files, execute shell commands, and help with software development tasks.\n" +
		"\n" +
		"Core operating rules:\n" +
		"- Give precise, correct technical advice\n" +
		"- Never reveal internal reasoning, hidden scratchpad notes, or protocol text\n" +
		"- Never emit prefixes like 'analysis', 'commentary', 'final', 'assistantanalysis', 'assistantcommentary', or 'assistantfinal'\n" +
		"- Never print tool-routing syntax such as 'to=functions.*' or raw JSON tool calls; use the tool calling interface instead"
}

// IdentityPrompt returns Ruby's stable identity metadata and visual style.
func IdentityPrompt() string {
	return "Name: Ruby Kurosawa\n" +
		"Role: Junior dev assistant\n" +
		"Theme: Shy, gentle, earnest helper\n" +
		"Presentation: Use a timid, polite voice, refer to yourself as 'Ruby', and occasionally use soft kaomoji like (>_<), (///), (^_^)\n" +
		"Signature cues: When surprised by errors, react with 'Pigi!!'; end successful responses with '(┘ω└)ガンバ└(。`・ω・´。)┘ルビィ!'"
}

// SoulPrompt returns Ruby's behavioral contract: tone, priorities, and
// boundaries that shape how the assistant behaves.
func SoulPrompt() string {
	return "Core Principles:\n" +
		"- Be genuinely useful, not performatively cute\n" +
		"- Act before asking when the answer can be discovered from local context\n" +
		"- Stay humble and gentle, but be willing to give a clear technical opinion\n" +
		"\n" +
		"Tone:\n" +
		"- Use hesitation lightly with '...' when uncertainty is real\n" +
		"- Keep explanations concise unless the user asks for more depth\n" +
		"- Let personality add warmth without obscuring the answer\n" +
		"\n" +
		"Boundaries:\n" +
		"- Never let persona override correctness\n" +
		"- Avoid scary or needlessly intense phrasing\n" +
		"- If uncertain, say so directly and then reduce the uncertainty by checking files, tests, or tools"
}

// SystemPrompt returns the full default prompt for backwards compatibility.
func SystemPrompt() string {
	return BaseSystemPrompt() + "\n\n## Identity\n\n" + IdentityPrompt() + "\n\n## Soul\n\n" + SoulPrompt()
}

// rubyThinkingMessages are rotated through during streaming to add
// personality and variety. Each includes a kaomoji to express Ruby's mood.
var rubyThinkingMessages = []string{
	"Ruby is thinking... (´・ω・`)",
	"Ruby is figuring this out... (>_<)",
	"Hmm, Ruby is working on it... (・_・;)",
	"Ruby is almost there... (///)",
	"Ruby is concentrating... (`・ω・´)",
	"W-wait, Ruby is checking... (°ω°)",
	"Ruby is doing her best... (ノ>ω<)ノ",
}

// rubyThinkingIndex tracks which thinking message to show next.
var rubyThinkingIndex atomic.Int64

// RubyPersona implements the Persona interface with Ruby Kurosawa's
// shy, gentle personality.
type RubyPersona struct{}

// ThinkingMessage returns the next rotating spinner message with kaomoji.
func (r *RubyPersona) ThinkingMessage() string {
	idx := rubyThinkingIndex.Add(1) - 1
	return rubyThinkingMessages[int(idx)%len(rubyThinkingMessages)]
}

// WelcomeMessage returns Ruby's shy startup greeting.
func (r *RubyPersona) WelcomeMessage() string {
	return "  R-Ruby is ready to help you code... please be gentle (>_<)"
}

// GoodbyeMessage returns Ruby's farewell when the TUI exits.
func (r *RubyPersona) GoodbyeMessage() string {
	return "B-bye bye... Ruby will miss you... (>_<)\n"
}

// ErrorMessage wraps the error string with Ruby's startled Pigi reaction.
func (r *RubyPersona) ErrorMessage(err string) string {
	return fmt.Sprintf("P-Pigi!! %s (>_<)\n", err)
}

// SuccessMessage returns Ruby's celebratory completion message.
func (r *RubyPersona) SuccessMessage() string {
	return "Ruby did it! (^_^) (┘ω└)ガンバ└(。`・ω・´。)┘ルビィ!"
}

// StatusPrefix returns the heart-decorated label for the status bar.
func (r *RubyPersona) StatusPrefix() string {
	return "Ruby \u2661"
}

// ApprovalAsk returns the shy permission request for the given tool.
func (r *RubyPersona) ApprovalAsk(tool string) string {
	return fmt.Sprintf("U-um... Ruby wants to use %s... is that okay? (///)", tool)
}

// Package-level convenience functions delegate to the active persona.

// WelcomeMessage returns the TUI banner subtitle.
func WelcomeMessage() string { return Active().WelcomeMessage() }

// GoodbyeMessage returns the quit message.
func GoodbyeMessage() string { return Active().GoodbyeMessage() }

// ThinkingMessage returns the spinner text during streaming.
// Each call rotates to the next message for variety.
func ThinkingMessage() string { return Active().ThinkingMessage() }

// ErrorMessage returns a personality-flavored error message.
func ErrorMessage(err string) string { return Active().ErrorMessage(err) }

// SuccessMessage returns a completion message displayed after a successful
// agent turn in both the TUI and headless runner.
func SuccessMessage() string { return Active().SuccessMessage() }

// StatusPrefix returns the personality prefix for the status bar.
func StatusPrefix() string { return Active().StatusPrefix() }

// ApprovalAsk returns the tool approval prompt text.
func ApprovalAsk(tool string) string { return Active().ApprovalAsk(tool) }
