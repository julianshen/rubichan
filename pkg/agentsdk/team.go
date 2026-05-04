package agentsdk

import "time"

// MessageType categorizes messages in the team mailbox.
type MessageType string

const (
	MessageTypeText             MessageType = "text"
	MessageTypeShutdownRequest  MessageType = "shutdown_request"
	MessageTypeShutdownApproved MessageType = "shutdown_approved"
	MessageTypeShutdownRejected MessageType = "shutdown_rejected"
	MessageTypeTaskUpdate       MessageType = "task_update"
	MessageTypeIdle             MessageType = "idle"
	MessageTypePlanApproval     MessageType = "plan_approval"
)

// MailboxMessage is a single message in an agent's inbox.
type MailboxMessage struct {
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Text      string                 `json:"text,omitempty"`
	Type      MessageType            `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Color     string                 `json:"color,omitempty"`
	Summary   string                 `json:"summary,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Read      bool                   `json:"read"`
}
