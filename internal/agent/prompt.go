package agent

import "strings"

// PromptSection represents a named section of the system prompt.
type PromptSection struct {
	Name      string
	Content   string
	Cacheable bool // hint: content rarely changes between turns
}

// PromptBuilder assembles the system prompt from ordered sections,
// placing cacheable (static) sections first for better provider caching.
type PromptBuilder struct {
	sections []PromptSection
}

// NewPromptBuilder creates an empty PromptBuilder.
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{}
}

// AddSection appends a section. Sections are reordered at Build time.
func (pb *PromptBuilder) AddSection(s PromptSection) {
	pb.sections = append(pb.sections, s)
}

// Build returns the assembled prompt string and cache breakpoint byte offsets.
// Cacheable sections are placed first. A single breakpoint is inserted after
// the last cacheable section (only if both cacheable and dynamic sections exist).
func (pb *PromptBuilder) Build() (string, []int) {
	if len(pb.sections) == 0 {
		return "", nil
	}

	var cacheable, dynamic []PromptSection
	for _, s := range pb.sections {
		if s.Cacheable {
			cacheable = append(cacheable, s)
		} else {
			dynamic = append(dynamic, s)
		}
	}

	var sb strings.Builder
	var breakpoints []int

	for _, s := range cacheable {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(s.Name)
		sb.WriteString("\n\n")
		sb.WriteString(s.Content)
	}

	// Insert breakpoint after all cacheable sections (only if dynamic follows).
	if len(cacheable) > 0 && len(dynamic) > 0 {
		breakpoints = append(breakpoints, sb.Len())
	}

	for _, s := range dynamic {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(s.Name)
		sb.WriteString("\n\n")
		sb.WriteString(s.Content)
	}

	return sb.String(), breakpoints
}
