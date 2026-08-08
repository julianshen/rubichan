package acp

import (
	"encoding/json"
	"fmt"
)

// ContentBlockType names a ContentBlock variant. The wire discriminator is the
// "type" field, and the set is closed: a block whose type is none of these is
// not a block this agent can act on.
type ContentBlockType string

const (
	ContentText         ContentBlockType = "text"
	ContentImage        ContentBlockType = "image"
	ContentAudio        ContentBlockType = "audio"
	ContentResource     ContentBlockType = "resource"
	ContentResourceLink ContentBlockType = "resource_link"
)

// ContentBlock is one element of a session/prompt payload.
//
// ACP models this as a tagged union, so the fields carried depend on Type. It
// is flattened into one struct rather than five because the variants are small
// and a client sends them interleaved in a single array; the union is enforced
// by validation rather than by the type system.
//
// Note that audio has no uri field while image does — mirroring the two is an
// easy way to accept a payload the spec does not define.
type ContentBlock struct {
	Type ContentBlockType `json:"type"`

	// Text is required for the text variant.
	Text string `json:"text,omitempty"`

	// URI and Name are required for resource_link. URI is also optional on
	// image.
	URI  string `json:"uri,omitempty"`
	Name string `json:"name,omitempty"`

	MimeType    string `json:"mimeType,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Size        *int64 `json:"size,omitempty"`

	// Data carries base64 payload bytes for image and audio.
	Data string `json:"data,omitempty"`

	// Resource carries the embedded body of the resource variant.
	Resource json.RawMessage `json:"resource,omitempty"`

	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// DecodePromptContent parses a session/prompt content array and rejects any
// block this agent has not declared it can accept.
//
// The gate reads caps rather than a fixed list so that the handshake and this
// decoder cannot drift: declaring promptCapabilities.image is what makes an
// image block admissible, and nothing else is. Rejecting is the honest failure
// — silently dropping an attachment leaves the client believing the agent read
// something it never saw.
func DecodePromptContent(raw json.RawMessage, caps PromptCapabilities) ([]ContentBlock, error) {
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("%w: prompt: %v", ErrInvalidParams, err)
	}

	for i, b := range blocks {
		if err := checkAdmissible(b.Type, caps); err != nil {
			return nil, fmt.Errorf("%w: prompt[%d]: %v", ErrInvalidParams, i, err)
		}
		if err := checkRequiredFields(b); err != nil {
			return nil, fmt.Errorf("%w: prompt[%d]: %v", ErrInvalidParams, i, err)
		}
	}
	return blocks, nil
}

// checkRequiredFields enforces the fields the spec marks required on the
// variants this agent accepts. Absence cannot be detected after decoding —
// a missing string is indistinguishable from an empty one — so it is checked
// here or not at all.
//
// Only the ungated variants are covered: the others cannot reach this point
// while undeclared, and the slice that declares one is the slice that learns
// what its required fields mean.
func checkRequiredFields(b ContentBlock) error {
	switch b.Type {
	case ContentText:
		if b.Text == "" {
			return fmt.Errorf("text content requires a non-empty text field")
		}
	case ContentResourceLink:
		if b.URI == "" {
			return fmt.Errorf("resource_link requires a uri")
		}
		if b.Name == "" {
			return fmt.Errorf("resource_link requires a name")
		}
	}
	return nil
}

// checkAdmissible reports whether a variant may appear in a prompt given what
// the agent declared. text and resource_link carry no capability of their own
// and are always admissible.
func checkAdmissible(t ContentBlockType, caps PromptCapabilities) error {
	switch t {
	case ContentText, ContentResourceLink:
		return nil
	case ContentImage:
		if !caps.Image {
			return fmt.Errorf("image content requires promptCapabilities.image, which this agent does not declare")
		}
		return nil
	case ContentAudio:
		if !caps.Audio {
			return fmt.Errorf("audio content requires promptCapabilities.audio, which this agent does not declare")
		}
		return nil
	case ContentResource:
		if !caps.EmbeddedContext {
			return fmt.Errorf("resource content requires promptCapabilities.embeddedContext, which this agent does not declare")
		}
		return nil
	default:
		return fmt.Errorf("unknown content type %q", t)
	}
}
