package session_test

import (
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/session"
	"github.com/stretchr/testify/assert"
)

func TestVerdictHistoryEmpty_OnInit(t *testing.T) {
	hist := session.NewVerdictHistory()
	assert.Empty(t, hist.Verdicts())
}

func TestVerdictHistoryRecordsSuccess(t *testing.T) {
	hist := session.NewVerdictHistory()
	hist.Record(session.Verdict{
		ToolName:  "shell",
		Command:   "ls",
		Status:    "success",
		Timestamp: time.Now(),
	})

	verdicts := hist.Verdicts()
	assert.Len(t, verdicts, 1)
	assert.Equal(t, "shell", verdicts[0].ToolName)
	assert.Equal(t, "success", verdicts[0].Status)
}

func TestVerdictHistoryLimitedSize(t *testing.T) {
	hist := session.NewVerdictHistory()
	// Record more than max verdicts
	for i := 0; i < 150; i++ {
		hist.Record(session.Verdict{
			ToolName:  "shell",
			Command:   "echo",
			Status:    "success",
			Timestamp: time.Now(),
		})
	}

	verdicts := hist.Verdicts()
	assert.LessOrEqual(t, len(verdicts), 100)
}

func TestVerdictHistorySummaryByTool(t *testing.T) {
	hist := session.NewVerdictHistory()
	hist.Record(session.Verdict{
		ToolName:  "shell",
		Command:   "ls",
		Status:    "success",
		Timestamp: time.Now(),
	})
	hist.Record(session.Verdict{
		ToolName:  "shell",
		Command:   "cd /nonexistent",
		Status:    "error",
		Timestamp: time.Now(),
	})
	hist.Record(session.Verdict{
		ToolName:  "read_file",
		Command:   "read",
		Status:    "success",
		Timestamp: time.Now(),
	})

	summary := hist.SummaryByTool()
	assert.Equal(t, 2, summary["shell"].Total)
	assert.Equal(t, 1, summary["shell"].Successful)
	assert.Equal(t, 1, summary["shell"].Failed)
	assert.Equal(t, 1, summary["read_file"].Total)
	assert.Equal(t, 1, summary["read_file"].Successful)
}
