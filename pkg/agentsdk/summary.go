package agentsdk

// SummaryCallback receives periodic activity summaries.
// taskID identifies the agent/task being summarized.
// summaryText is the 3-5 word activity description.
type SummaryCallback func(taskID string, summaryText string)
