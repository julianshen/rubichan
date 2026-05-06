package agentsdk

// DisplayMessage represents a message that can be displayed in an
// agent's tmux window. It carries the same content structure as
// provider messages but is decoupled from internal packages.
type DisplayMessage struct {
	Role    string
	Content []ContentBlock
}
