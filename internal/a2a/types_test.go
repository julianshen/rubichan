package a2a

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskJSONRoundTrip(t *testing.T) {
	task := Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		State:     TaskCompleted,
		Messages: []Message{
			{Role: "agent", Parts: []Part{{Kind: "text", Text: "hello"}}},
		},
		Artifacts: []Artifact{
			{Name: "result", Parts: []Part{{Kind: "text", Text: "42"}}},
		},
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var decoded Task
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, task, decoded)
	assert.Contains(t, string(data), `"state":"completed"`)
}

func TestTaskStateConstants(t *testing.T) {
	assert.Equal(t, TaskState("submitted"), TaskSubmitted)
	assert.Equal(t, TaskState("working"), TaskWorking)
	assert.Equal(t, TaskState("input-required"), TaskInputRequired)
	assert.Equal(t, TaskState("auth-required"), TaskAuthRequired)
	assert.Equal(t, TaskState("completed"), TaskCompleted)
	assert.Equal(t, TaskState("failed"), TaskFailed)
	assert.Equal(t, TaskState("canceled"), TaskCanceled)
	assert.Equal(t, TaskState("rejected"), TaskRejected)
}

func TestAgentCardJSONRoundTrip(t *testing.T) {
	card := AgentCard{
		Name:        "Code Review Agent",
		Description: "Performs code review and analysis",
		URL:         "https://example.com/agent",
		Skills: []Skill{
			{
				ID:          "skill-1",
				Name:        "Code Analysis",
				Description: "Analyzes code for issues",
				Tags:        []string{"review", "analysis"},
			},
			{
				ID:          "skill-2",
				Name:        "Documentation",
				Description: "Generates documentation",
				Tags:        []string{"docs"},
			},
		},
		Capabilities: Capabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		Auth: []AuthScheme{
			{Scheme: "bearer"},
		},
	}

	data, err := json.Marshal(card)
	require.NoError(t, err)

	var decoded AgentCard
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, card, decoded)
	assert.Contains(t, string(data), `"name":"Code Review Agent"`)
	assert.Contains(t, string(data), `"streaming":true`)
	assert.Len(t, decoded.Skills, 2)
	assert.Equal(t, "Code Analysis", decoded.Skills[0].Name)
}

func TestSkillJSONRoundTrip(t *testing.T) {
	skill := Skill{
		ID:          "test-skill",
		Name:        "Test Execution",
		Description: "Runs tests and reports results",
		Tags:        []string{"testing", "ci"},
	}

	data, err := json.Marshal(skill)
	require.NoError(t, err)

	var decoded Skill
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, skill, decoded)
	assert.Contains(t, string(data), `"id":"test-skill"`)
	assert.Contains(t, string(data), `"tags":["testing","ci"]`)
}

func TestCapabilitiesJSONRoundTrip(t *testing.T) {
	caps := Capabilities{
		Streaming:         true,
		PushNotifications: true,
	}

	data, err := json.Marshal(caps)
	require.NoError(t, err)

	var decoded Capabilities
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, caps, decoded)
	assert.Contains(t, string(data), `"streaming":true`)
	assert.Contains(t, string(data), `"pushNotifications":true`)
}

func TestAuthSchemeJSONRoundTrip(t *testing.T) {
	auth := AuthScheme{
		Scheme: "bearer",
	}

	data, err := json.Marshal(auth)
	require.NoError(t, err)

	var decoded AuthScheme
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, auth, decoded)
	assert.Contains(t, string(data), `"scheme":"bearer"`)
}

func TestRPCRequestJSONRoundTrip(t *testing.T) {
	params := json.RawMessage(`{"method":"executeTask","taskId":"task-123"}`)
	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "agent/prompt",
		Params:  params,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded RPCRequest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, req, decoded)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.Contains(t, string(data), `"method":"agent/prompt"`)
	assert.Contains(t, string(data), `"id":42`)
}

func TestRPCResponseWithResultJSONRoundTrip(t *testing.T) {
	result := json.RawMessage(`{"taskId":"task-123","state":"completed"}`)
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      42,
		Result:  result,
		Error:   nil,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded RPCResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, resp, decoded)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.Nil(t, decoded.Error)
	assert.NotNil(t, decoded.Result)
}

func TestRPCResponseWithErrorJSONRoundTrip(t *testing.T) {
	rpcErr := &RPCError{
		Code:    -32600,
		Message: "Invalid Request",
	}
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      42,
		Result:  nil,
		Error:   rpcErr,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded RPCResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, resp, decoded)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.NotNil(t, decoded.Error)
	assert.Equal(t, -32600, decoded.Error.Code)
	assert.Equal(t, "Invalid Request", decoded.Error.Message)
	assert.Nil(t, decoded.Result)
}

func TestRPCErrorJSONRoundTrip(t *testing.T) {
	rpcErr := RPCError{
		Code:    -32700,
		Message: "Parse error",
	}

	data, err := json.Marshal(rpcErr)
	require.NoError(t, err)

	var decoded RPCError
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, rpcErr, decoded)
	assert.Contains(t, string(data), `"code":-32700`)
	assert.Contains(t, string(data), `"message":"Parse error"`)
}
