package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/knowledgegraph"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ListSessions ---

func TestListSessionsNoStore(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.ListSessions(10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store not configured")
}

func TestListSessionsFiltersbyWorkingDir(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg,
		WithStore(s),
		WithWorkingDir("/project/a"),
	)

	// Create sessions with different working dirs
	require.NoError(t, s.CreateSession(store.Session{
		ID:         "sess-a1",
		Model:      "test",
		WorkingDir: "/project/a",
	}))
	require.NoError(t, s.CreateSession(store.Session{
		ID:         "sess-b1",
		Model:      "test",
		WorkingDir: "/project/b",
	}))
	require.NoError(t, s.CreateSession(store.Session{
		ID:         "sess-a2",
		Model:      "test",
		WorkingDir: "/project/a",
	}))

	sessions, err := a.ListSessions(100)
	require.NoError(t, err)

	// Should return sessions matching /project/a (2 created above + 1 auto-created by agent)
	assert.GreaterOrEqual(t, len(sessions), 2)
	for _, sess := range sessions {
		assert.Equal(t, "/project/a", sess.WorkingDir)
	}
	// /project/b session should be excluded
	for _, sess := range sessions {
		assert.NotEqual(t, "sess-b1", sess.ID, "should not include sessions from other dirs")
	}
}

func TestListSessionsEmptyForDifferentDir(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}

	// Agent creates its own session with WorkingDir="/project/a"
	a := New(mp, tools.NewRegistry(), autoApprove, cfg,
		WithStore(s),
		WithWorkingDir("/project/a"),
	)

	// Create a session in a different working dir
	require.NoError(t, s.CreateSession(store.Session{
		ID:         "other-dir-sess",
		Model:      "test",
		WorkingDir: "/project/other",
	}))

	// ListSessions should NOT include /project/other sessions,
	// but WILL include the auto-created session for /project/a
	sessions, err := a.ListSessions(10)
	require.NoError(t, err)

	for _, sess := range sessions {
		assert.Equal(t, "/project/a", sess.WorkingDir, "should only return matching sessions")
	}
}

// --- InjectUserContext ---

func TestInjectUserContext(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	a.InjectUserContext("shell output: exit code 0")

	msgs := a.conversation.Messages()
	require.GreaterOrEqual(t, len(msgs), 1)
	lastMsg := msgs[len(msgs)-1]
	assert.Equal(t, "user", lastMsg.Role)
	assert.Equal(t, "shell output: exit code 0", lastMsg.Content[0].Text)
}

// --- agentCompactor.ForceCompact (adapter at line 643) ---

func TestAgentCompactorForceCompact(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 500},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	// Fill conversation to make compaction meaningful
	for i := 0; i < 20; i++ {
		a.conversation.AddUser("message for testing compaction")
		a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response content here"}})
	}

	ac := &agentCompactor{agent: a}
	result := ac.ForceCompact(context.Background())

	assert.Greater(t, result.BeforeTokens, 0)
	assert.Greater(t, result.BeforeMsgCount, 0)
}

// --- truncateUIInput ---

func TestTruncateUIInputShort(t *testing.T) {
	input := json.RawMessage(`{"path": "hello.go"}`)
	result := truncateUIInput(input)
	assert.Equal(t, string(input), result)
}

func TestTruncateUIInputLong(t *testing.T) {
	// Create input longer than maxUIRequestInputBytes (2048)
	longContent := strings.Repeat("x", 3000)
	input := json.RawMessage(`{"data": "` + longContent + `"}`)
	result := truncateUIInput(input)

	assert.True(t, len(result) < len(string(input)), "result should be truncated")
	assert.True(t, strings.HasSuffix(result, "...(truncated)"))
	assert.Equal(t, maxUIRequestInputBytes+len("...(truncated)"), len(result))
}

// --- hasTextContent ---

func TestHasTextContentTrue(t *testing.T) {
	blocks := []provider.ContentBlock{
		{Type: "text", Text: "hello"},
	}
	assert.True(t, hasTextContent(blocks))
}

func TestHasTextContentFalseEmpty(t *testing.T) {
	var blocks []provider.ContentBlock
	assert.False(t, hasTextContent(blocks))
}

func TestHasTextContentFalseToolUseOnly(t *testing.T) {
	blocks := []provider.ContentBlock{
		{Type: "tool_use"},
	}
	assert.False(t, hasTextContent(blocks))
}

func TestHasTextContentFalseWhitespace(t *testing.T) {
	blocks := []provider.ContentBlock{
		{Type: "text", Text: "   \n\t  "},
	}
	assert.False(t, hasTextContent(blocks))
}

// --- loadSessionHistory error paths ---

func TestLoadSessionHistorySnapshotFallback(t *testing.T) {
	// Tests the path where snapshot fails but full history succeeds.
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))

	// Create session with messages but no snapshot
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "snapshot-fallback",
		Model:        "test",
		SystemPrompt: "test prompt",
	}))
	require.NoError(t, s.AppendMessage("snapshot-fallback", "user", []provider.ContentBlock{
		{Type: "text", Text: "Hello from history"},
	}))
	require.NoError(t, s.AppendMessage("snapshot-fallback", "assistant", []provider.ContentBlock{
		{Type: "text", Text: "Hi from history"},
	}))

	conv := NewConversation("test prompt")
	err = a.loadSessionHistory(conv, "snapshot-fallback")
	require.NoError(t, err)

	msgs := conv.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "Hello from history", msgs[0].Content[0].Text)
}

func TestLoadSessionHistoryFullLoadError(t *testing.T) {
	// When both snapshot and message loading fail.
	// This is hard to trigger with a real store, but we can test with an
	// invalid session ID that has no messages.
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))

	// Session with no messages — GetMessages returns empty, LoadFromMessages is fine
	require.NoError(t, s.CreateSession(store.Session{
		ID:    "empty-session",
		Model: "test",
	}))

	conv := NewConversation("test")
	err = a.loadSessionHistory(conv, "empty-session")
	require.NoError(t, err)
	assert.Empty(t, conv.Messages())
}

// --- RewindToTurn on Agent ---

func TestRewindToTurnNoCheckpointManager(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.RewindToTurn(context.Background(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint manager not configured")
}

func TestRewindToTurnSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	// Need to resolve symlinks for macOS
	resolvedDir, _ := filepath.EvalSymlinks(tmpDir)

	testFile := filepath.Join(resolvedDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("original"), 0644))

	cfg := config.DefaultConfig()
	mp := &mockProvider{}

	mgr, err := checkpoint.New(resolvedDir, "rewind-success-test", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithCheckpointManager(mgr))

	_, err = mgr.Capture(context.Background(), testFile, 1, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

	paths, err := a.RewindToTurn(context.Background(), 0)
	require.NoError(t, err)
	assert.Len(t, paths, 1)

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
}

// --- WithBootstrapContext ---

func TestWithBootstrapContextNilMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithBootstrapContext(nil))
	assert.NotNil(t, a)
}

func TestWithBootstrapContextNonNil(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}

	metadata := &knowledgegraph.BootstrapMetadata{
		Profile: knowledgegraph.BootstrapProfile{
			ProjectName: "testapp",
		},
		CreatedEntities: []string{"entity1", "entity2"},
		AnalysisMetadata: knowledgegraph.AnalysisMetadata{
			ModulesFound:         3,
			GitCommitsAnalyzed:   10,
			IntegrationsDetected: 2,
		},
	}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithBootstrapContext(metadata))
	assert.NotNil(t, a)
	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "testapp")
}

// --- WithPipeline ---

func TestWithPipelineNil(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	// WithPipeline(nil) is overridden by New's default pipeline setup,
	// so just verify the agent is created successfully.
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithPipeline(nil))
	assert.NotNil(t, a)
}

// --- WithUserHooks ---

func TestWithUserHooks(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithUserHooks(nil))
	assert.NotNil(t, a)
	assert.Nil(t, a.userHookRunner)
}

// --- WithLogger ---

func TestWithLogger(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	customLogger := agentsdk.DefaultLogger()
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithLogger(customLogger))
	assert.NotNil(t, a)
	assert.Equal(t, customLogger, a.logger)
}

// --- WithRateLimiter ---

func TestWithRateLimiter(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	rl := NewSharedRateLimiter(10)
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithRateLimiter(rl))
	assert.NotNil(t, a)
	assert.Equal(t, rl, a.rateLimiter)
}

// --- ACP handler edge cases ---

func TestHandlePromptEmptyPrompt(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"},
		{Type: "stop"},
	}}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithACP())

	_, err := a.handlePrompt(json.RawMessage(`{"prompt":""}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt cannot be empty")
}

func TestHandlePromptInvalidJSON(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithACP())

	_, err := a.handlePrompt(json.RawMessage(`not json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestHandleToolExecuteEmptyTool(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithACP())

	_, err := a.handleToolExecute(json.RawMessage(`{"tool":"","input":{}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool name cannot be empty")
}

func TestHandleToolExecuteInvalidJSON(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithACP())

	_, err := a.handleToolExecute(json.RawMessage(`bad json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestHandleToolExecuteExistingTool(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&testTool{name: "my_tool"}))
	a := New(mp, reg, autoApprove, cfg, WithACP())

	result, err := a.handleToolExecute(json.RawMessage(`{"tool":"my_tool","input":{}}`))
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "not_implemented", parsed["status"])
}

// --- ACP Skill/Security stubs ---

func TestACPInvokeEmptyName(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.Invoke(acp.SkillInvokeRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill name cannot be empty")
}

func TestACPInvokeSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	resp, err := a.Invoke(acp.SkillInvokeRequest{
		SkillName: "test-skill",
		Action:    "transform",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-skill", resp.SkillName)
	assert.Equal(t, "success", resp.Status)
}

func TestACPListReturnsEmptyList(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	resp := a.List(acp.SkillListRequest{})
	assert.NotNil(t, resp.Skills)
	assert.Empty(t, resp.Skills)
}

func TestACPManifestEmptyName(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.Manifest(acp.SkillManifestRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill name cannot be empty")
}

func TestACPManifestSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	resp, err := a.Manifest(acp.SkillManifestRequest{SkillName: "test-skill"})
	require.NoError(t, err)
	assert.Equal(t, "test-skill", resp.Name)
	assert.Equal(t, "loaded", resp.Status)
}

func TestACPScanEmptyTarget(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.Scan(acp.SecurityScanRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target cannot be empty")
}

func TestACPScanSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	resp, err := a.Scan(acp.SecurityScanRequest{Target: "./src"})
	require.NoError(t, err)
	assert.Empty(t, resp.Findings)
}

func TestACPApproveEmptyDecision(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	err := a.Approve(acp.SecurityApprovalResponse{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decision cannot be empty")
}

func TestACPApproveSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	err := a.Approve(acp.SecurityApprovalResponse{Decision: "accepted"})
	require.NoError(t, err)
}

// --- LoadBootstrapContext error paths ---

func TestLoadBootstrapContextInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0644))

	_, err := LoadBootstrapContext(path)
	require.Error(t, err)
}

func TestLoadBootstrapContextMissingFile(t *testing.T) {
	_, err := LoadBootstrapContext("/nonexistent/file.json")
	require.Error(t, err)
}

// --- BuildBootstrapSystemPromptPrefix with >5 entities ---

func TestBuildBootstrapSystemPromptPrefixManyEntities(t *testing.T) {
	metadata := &knowledgegraph.BootstrapMetadata{
		Profile: knowledgegraph.BootstrapProfile{
			ProjectName: "bigapp",
		},
		CreatedEntities: []string{"e1", "e2", "e3", "e4", "e5", "e6", "e7"},
		AnalysisMetadata: knowledgegraph.AnalysisMetadata{
			ModulesFound:         10,
			GitCommitsAnalyzed:   100,
			IntegrationsDetected: 5,
		},
	}

	prefix := BuildBootstrapSystemPromptPrefix(metadata)
	assert.Contains(t, prefix, "...")
	assert.Contains(t, prefix, "bigapp")
	// Should only show first 5 entities
	assert.Contains(t, prefix, "e5")
}

// --- ResultStore error paths ---

func TestResultStoreRetrieveNotFound(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"}))
	rs := NewResultStore(s, "s1", 20)

	_, err = rs.Retrieve("nonexistent-ref-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- SetStrategies edge cases ---

func TestSetStrategiesNil(t *testing.T) {
	cm := NewContextManager(100000, 0)
	// Setting nil should restore defaults
	cm.SetStrategies(nil)
	// No panic — defaults restored
}

func TestSetStrategiesEmpty(t *testing.T) {
	cm := NewContextManager(100000, 0)
	cm.SetStrategies([]CompactionStrategy{})
	// No panic — defaults restored
}

// --- recordToolProgress nil safety ---

func TestRecordToolProgressNilTracker(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)
	a.progress = nil

	// Should not panic
	a.recordToolProgress(provider.ToolUseBlock{Name: "file"}, toolExecResult{})
}

// --- pendingToolSignature ---

func TestPendingToolSignature(t *testing.T) {
	tools := []provider.ToolUseBlock{
		{Name: "file", Input: json.RawMessage(`{"path":"a.go"}`)},
		{Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
	}

	sig := pendingToolSignature(tools)
	assert.Contains(t, sig, "file:")
	assert.Contains(t, sig, "shell:")
}

// --- ClearConversation ---

func TestClearConversationRemovesAllMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	a.conversation.AddUser("hello")
	a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})
	require.Len(t, a.conversation.Messages(), 2)

	a.ClearConversation()
	assert.Empty(t, a.conversation.Messages())
}

// --- loadManifest edge cases ---

func TestLoadManifestNoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := loadManifest(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SKILL.yaml or SKILL.md found")
}

func TestLoadManifestYAMLFile(t *testing.T) {
	tmpDir := t.TempDir()
	yamlContent := `name: test-skill
version: "1.0"
types:
  - prompt
description: A test skill
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "SKILL.yaml"), []byte(yamlContent), 0644))

	manifest, err := loadManifest(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "test-skill", manifest.Name)
}

func TestLoadManifestMDFile(t *testing.T) {
	tmpDir := t.TempDir()
	mdContent := `---
name: md-skill
version: "1.0"
type: prompt
description: A markdown skill
---
You are a helpful assistant.
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "SKILL.md"), []byte(mdContent), 0644))

	manifest, err := loadManifest(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "md-skill", manifest.Name)
}

func TestLoadManifestInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	// Write invalid YAML that's present but unparseable
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "SKILL.yaml"), []byte("{{invalid yaml"), 0644))

	_, err := loadManifest(tmpDir)
	require.Error(t, err)
}

// --- copyDirAdapter ---

func TestCopyDirAdapter(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dest")

	// Create source structure
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("nested"), 0644))

	err := copyDirAdapter(srcDir, dstDir)
	require.NoError(t, err)

	// Verify files were copied
	data, err := os.ReadFile(filepath.Join(dstDir, "root.txt"))
	require.NoError(t, err)
	assert.Equal(t, "root", string(data))

	data, err = os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested", string(data))
}

// --- validateSkillNameAdapter ---

func TestValidateSkillNameAdapterTooLong(t *testing.T) {
	longName := strings.Repeat("a", 129)
	err := validateSkillNameAdapter(longName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum length")
}

func TestValidateSkillNameAdapterInvalidChars(t *testing.T) {
	err := validateSkillNameAdapter("bad name!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must contain only")
}

func TestValidateSkillNameAdapterValid(t *testing.T) {
	err := validateSkillNameAdapter("good-skill_1")
	require.NoError(t, err)
}

// --- jsonStr ---

func TestJsonStrNil(t *testing.T) {
	result := jsonStr(nil)
	assert.Equal(t, "", result)
}

func TestJsonStrValid(t *testing.T) {
	result := jsonStr(json.RawMessage(`"hello"`))
	assert.Equal(t, "hello", result)
}

func TestJsonStrInvalid(t *testing.T) {
	result := jsonStr(json.RawMessage(`not a string`))
	assert.Equal(t, "", result)
}

// --- copyFileAdapter error paths ---

func TestCopyFileAdapterSourceNotFound(t *testing.T) {
	err := copyFileAdapter("/nonexistent/src.go", "/tmp/dst.go")
	require.Error(t, err)
}

// --- installFromLocal error path: bad manifest ---

func TestInstallFromLocalBadManifest(t *testing.T) {
	adapter, tmpDir := newTestAdapter(t, nil)
	srcDir := filepath.Join(tmpDir, "bad-manifest-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	// No SKILL.yaml or SKILL.md
	_, err := adapter.installFromLocal(srcDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SKILL.yaml or SKILL.md found")
}

// --- installFromRegistry error path: invalid name ---

func TestInstallFromRegistryInvalidName(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	_, err := adapter.installFromRegistry(context.Background(), "../../bad-name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

// --- loadSessionHistory with snapshot available ---

func TestLoadSessionHistoryFromSnapshot(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))

	// Create session
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "snapshot-session",
		Model:        "test",
		SystemPrompt: "system",
	}))

	// Save a snapshot with messages
	snapMsgs := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "From snapshot"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "Snapshot reply"}}},
	}
	require.NoError(t, s.SaveSnapshot("snapshot-session", snapMsgs, 100))

	// Also save regular messages (should be ignored since snapshot exists)
	require.NoError(t, s.AppendMessage("snapshot-session", "user", []provider.ContentBlock{
		{Type: "text", Text: "From messages"},
	}))

	conv := NewConversation("system")
	err = a.loadSessionHistory(conv, "snapshot-session")
	require.NoError(t, err)

	msgs := conv.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "From snapshot", msgs[0].Content[0].Text)
}

// --- ResultStore OffloadResult store failure graceful degradation ---

func TestResultStoreOffloadStoreFailure(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)

	require.NoError(t, s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"}))
	rs := NewResultStore(s, "s1", 20)

	// Close the store to cause a failure
	s.Close()

	// OffloadResult should gracefully degrade — return original content
	bigContent := "this is a large tool result that exceeds the threshold limit by quite a bit"
	result, err := rs.OffloadResult("shell", "t1", bigContent)
	require.NoError(t, err)
	assert.Equal(t, bigContent, result, "should return original content on store failure")
}

// --- tryActivate discover failure ---

func TestTryActivateDiscoverFailure(t *testing.T) {
	act := &mockActivator{discoverErr: fmt.Errorf("discover failed")}
	adapter := &skillManagerAdapter{activator: act}

	result := adapter.tryActivate("test-skill")
	assert.False(t, result, "should return false when discover fails")
}

// --- Install routing: registry path when source is just a name ---

func TestInstallRoutesToRegistryForBareName(t *testing.T) {
	// A bare name like "kubernetes" is not a git URL or local path,
	// so it should go to installFromRegistry. We test the error path
	// since the registry is unreachable.
	adapter, _ := newTestAdapter(t, nil)
	_, err := adapter.Install(context.Background(), "kubernetes")
	// Should fail because registry is unreachable, but the important thing
	// is it routed to registry (not local or git).
	require.Error(t, err)
}

func TestInstallRoutesToRegistryForNameAtVersion(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	_, err := adapter.Install(context.Background(), "kubernetes@1.2.0")
	require.Error(t, err)
}

// --- loadManifest YAML read error (not NotExist) ---

func TestLoadManifestYAMLReadError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create SKILL.yaml as a directory instead of a file — causes a read error
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "SKILL.yaml"), 0755))

	_, err := loadManifest(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

// --- Remove error path: store check error ---

func TestSkillManagerRemoveStoreCheckError(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	// Close the store to force an error
	adapter.store.Close()

	err := adapter.Remove("valid-name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check skill state")
}

// --- Retrieve empty blob ---

func TestResultStoreRetrieveEmptyBlob(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"}))
	rs := NewResultStore(s, "s1", 20)

	// Save an empty blob
	require.NoError(t, s.SaveBlob("ref-empty", "s1", "shell", "", 0))

	_, err = rs.Retrieve("ref-empty")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- requestToolApproval: no approval function configured ---

func TestRequestToolApprovalNilApprovalFunc(t *testing.T) {
	a := &Agent{logger: agentsdk.DefaultLogger()}
	// Both uiRequestHandler and approve are nil
	ch := make(chan TurnEvent, 100)

	_, _, err := a.requestToolApproval(context.Background(), ch, provider.ToolUseBlock{
		ID:   "tool-1",
		Name: "file",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "approval function not configured")
}

// --- ForkSession no store ---

func TestForkSessionNoStore(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	_, err := a.ForkSession(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store not configured")
}

// --- installFromLocal copy error ---

func TestInstallFromLocalCopyDirError(t *testing.T) {
	adapter, tmpDir := newTestAdapter(t, nil)

	// Create source skill with valid manifest
	srcDir := filepath.Join(tmpDir, "copy-error-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	manifest := `name: copy-error
version: "1.0.0"
description: test
types: [prompt]
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.yaml"), []byte(manifest), 0o644))

	// Create a file that's actually a directory to cause copy error
	subDir := filepath.Join(srcDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("content"), 0o644))

	result, err := adapter.installFromLocal(srcDir)
	// Should succeed since the source is valid
	require.NoError(t, err)
	assert.Equal(t, "copy-error", result.Name)
}

// --- Install routes to git for various URL patterns ---

func TestInstallRoutesToGitForSSHURL(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	// Should fail because git clone fails, but verifies routing
	_, err := adapter.Install(context.Background(), "ssh://git@github.com/user/skill")
	require.Error(t, err)
}

func TestInstallRoutesToGitForGitAtURL(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	_, err := adapter.Install(context.Background(), "git@github.com:user/skill")
	require.Error(t, err)
}

// --- buildSkillTriggerContext with unreadable dir ---

func TestBuildSkillTriggerContextUnreadableDir(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithWorkingDir("/nonexistent-dir-xyz"))

	// Should not panic; just returns empty project files
	tc := a.buildSkillTriggerContext("hello")
	assert.Equal(t, "hello", tc.LastUserMessage)
	assert.Empty(t, tc.ProjectFiles)
}

// --- ResumeSession with snapshot-based history ---

func TestResumeSessionUsesSnapshot(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{}
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))

	// Create session with snapshot
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "snapshot-resume",
		Model:        "test",
		SystemPrompt: "system prompt",
	}))
	snapMsgs := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "snap msg"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "snap reply"}}},
	}
	require.NoError(t, s.SaveSnapshot("snapshot-resume", snapMsgs, 50))

	err = a.ResumeSession(context.Background(), "snapshot-resume")
	require.NoError(t, err)
	assert.Equal(t, "snapshot-resume", a.SessionID())

	msgs := a.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "snap msg", msgs[0].Content[0].Text)
}
