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

// SpawnRequest describes a teammate to spawn.
type SpawnRequest struct {
	AgentName string
	AgentType string
	Prompt    string
	Tools     []string
	Model     string
}

// TeammateID identifies a teammate in a team.
type TeammateID struct {
	AgentID   string
	AgentName string
	Color     string
}

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
