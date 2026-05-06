package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummaryCallback(t *testing.T) {
	var receivedTaskID, receivedSummary string
	cb := SummaryCallback(func(taskID, summary string) {
		receivedTaskID = taskID
		receivedSummary = summary
	})

	cb("task-1", "Reading runAgent.ts")
	require.Equal(t, "task-1", receivedTaskID)
	require.Equal(t, "Reading runAgent.ts", receivedSummary)
}
