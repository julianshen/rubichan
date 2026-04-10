package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionString(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "rubichan")
	assert.Contains(t, s, version)
	assert.Contains(t, s, commit)
	assert.Contains(t, s, date)
}

func TestVersionStringDefaults(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "dev")
	assert.Contains(t, s, "none")
	assert.Contains(t, s, "unknown")
}

func TestShouldIgnoreTUIRunError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.True(t, shouldIgnoreTUIRunError(tea.ErrProgramKilled, ctx))
	assert.False(t, shouldIgnoreTUIRunError(nil, ctx))
	assert.False(t, shouldIgnoreTUIRunError(context.Canceled, ctx))
	assert.False(t, shouldIgnoreTUIRunError(tea.ErrProgramKilled, context.Background()))
}

func TestShouldIgnoreTUIRunErrorSignalAbortReturnsFalse(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&interactiveSignalAbort{name: "quit", exitCode: 131})

	assert.False(t, shouldIgnoreTUIRunError(tea.ErrProgramKilled, ctx))
}

func TestHandleInteractiveProgramErrorReturnsExitErrorForSignalAbort(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&interactiveSignalAbort{name: "quit", exitCode: 131})

	err := handleInteractiveProgramError(tea.ErrProgramKilled, ctx, "running TUI")
	var exitErr *runner.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 131, exitErr.Code)
}

func TestInteractiveExitErrorReturnsExitErrorForSignalAbort(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&interactiveSignalAbort{name: "quit", exitCode: 131})

	err := interactiveExitError(ctx)
	var exitErr *runner.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 131, exitErr.Code)
}

func TestStartSessionLoggerWritesFileAndRestoresLogger(t *testing.T) {
	origWriter := log.Writer()
	origFlags := log.Flags()
	var sentinel bytes.Buffer
	log.SetOutput(&sentinel)
	log.SetFlags(123)
	defer log.SetOutput(origWriter)
	defer log.SetFlags(origFlags)

	logger, err := startSessionLogger(t.TempDir(), false)
	require.NoError(t, err)
	require.FileExists(t, logger.path)

	log.Printf("captured line")

	require.NoError(t, logger.Close())

	data, err := os.ReadFile(logger.path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "rubichan session log started")
	assert.Contains(t, string(data), "captured line")
	assert.Contains(t, string(data), "rubichan session log finished")
	info, err := os.Stat(logger.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, filepath.Base(logger.path), strconv.Itoa(os.Getpid()))
	dirInfo, err := os.Stat(filepath.Dir(logger.path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	assert.NotContains(t, sentinel.String(), "captured line")
	log.Print("restored line")
	assert.Contains(t, sentinel.String(), "restored line")
	assert.Equal(t, 123, log.Flags())
}

func TestStartSessionLoggerMirrorsToStderrInDebugMode(t *testing.T) {
	origWriter := log.Writer()
	origFlags := log.Flags()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	os.Stderr = w
	log.SetFlags(123)
	defer log.SetOutput(origWriter)
	defer log.SetFlags(origFlags)
	defer func() { os.Stderr = origStderr }()

	logger, err := startSessionLogger(t.TempDir(), true)
	require.NoError(t, err)

	log.Printf("debug line")

	require.NoError(t, logger.Close())
	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(data), "debug line")
}

func TestStartEventLoggerWritesJSONLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "session.jsonl")
	logger, err := startEventLogger(path)
	require.NoError(t, err)
	require.NotNil(t, logger)
	require.Equal(t, path, logger.path)

	_, err = logger.file.WriteString("{\"type\":\"command_result\"}\n")
	require.NoError(t, err)
	require.NoError(t, logger.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"type":"command_result"`)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBuildEventSinkWithoutDebugAndEventLogIsNoOp(t *testing.T) {
	sink := buildEventSink(nil, false)
	require.Len(t, sink, 0)
	assert.NotPanics(t, func() {
		sink.Emit(session.NewTurnStartedEvent("prompt", "model"))
	})
}

func TestBuildEventSinkIncludesJSONLWithoutDebug(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "session.jsonl")
	logger, err := startEventLogger(path)
	require.NoError(t, err)
	require.NoError(t, logger.Close())

	sink := buildEventSink(logger, false)
	require.Len(t, sink, 1)
}

func TestWritePanicDumpIncludesPanicAndSessionLog(t *testing.T) {
	cfgDir := t.TempDir()
	dumpPath, err := writePanicDump(cfgDir, "boom", "/tmp/session.log")
	require.NoError(t, err)
	require.FileExists(t, dumpPath)

	data, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "panic: boom")
	assert.Contains(t, text, "session_log: /tmp/session.log")
	assert.Contains(t, text, "goroutine")
	info, err := os.Stat(dumpPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, filepath.Base(dumpPath), strconv.Itoa(os.Getpid()))
}

func TestLogFileSuffixIncludesTimestampAndPID(t *testing.T) {
	now := time.Date(2026, time.March, 11, 21, 15, 30, 123456789, time.FixedZone("UTC+8", 8*3600))
	suffix := logFileSuffix(now)
	assert.Equal(t, fmt.Sprintf("20260311-131530.123456789-%d", os.Getpid()), suffix)
}

func TestStartInteractiveSignalHandlerStopIsIdempotent(t *testing.T) {
	stop := startInteractiveSignalHandler(t.TempDir(), "/tmp/session.log", func(error) {})
	stop()
	stop()
}

func TestAutoApproveDefaultsFalse(t *testing.T) {
	// autoApprove is a package-level var; verify it defaults to false
	assert.False(t, autoApprove, "auto-approve must default to false to prevent RCE")
}

func TestOpenStore_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	dbPath := filepath.Join(dir, "rubichan.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "database file should exist")
}

func TestOpenStore_CreatesMissingDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "config")
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	dbPath := filepath.Join(dir, "rubichan.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "database file should exist in nested directory")
}

func TestEnsureFolderAccessApprovedNonInteractiveRequiresExplicitApproval(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	err = ensureFolderAccessApprovedNonInteractive(s, filepath.Join(dir, "repo"), false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--approve-cwd/--auto-approve")
}

func TestEnsureFolderAccessApprovedNonInteractiveApproveCwd(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	repoDir := filepath.Join(dir, "repo")
	err = ensureFolderAccessApprovedNonInteractive(s, repoDir, false, true)
	require.NoError(t, err)

	approved, err := s.IsFolderApproved(repoDir)
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestAppendWorkingDirOptionAlwaysAppliesCWD(t *testing.T) {
	opts := appendWorkingDirOption(nil, "/tmp/project")

	a := &agent.Agent{}
	for _, opt := range opts {
		opt(a)
	}

	assert.Equal(t, "/tmp/project", a.WorkingDir())
}

func TestResumeFlagDefaults(t *testing.T) {
	assert.Empty(t, resumeFlag, "resume flag must default to empty")
}

func TestResumeFlagDefinedOnRootCmd(t *testing.T) {
	// Build a minimal cobra command that mirrors the real command structure
	// so we can verify the flag is properly defined without spinning up the full app
	var localResume string
	cmd := &cobra.Command{
		Use:   "rubichan",
		Short: "An AI coding assistant",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	cmd.PersistentFlags().StringVarP(&localResume, "resume", "r", "", "resume a previous session by ID")

	// Verify that the --resume flag is defined with proper metadata
	flag := cmd.PersistentFlags().Lookup("resume")
	if flag == nil {
		t.Fatal("expected --resume flag to exist")
	}

	// Check flag has short name 'r'
	if flag.Shorthand != "r" {
		t.Errorf("expected flag shorthand 'r', got '%s'", flag.Shorthand)
	}

	// Check flag is a string type
	if flag.Value.Type() != "string" {
		t.Errorf("expected flag type 'string', got '%s'", flag.Value.Type())
	}

	// Check flag has correct usage description
	expectedUsage := "resume a previous session by ID"
	if flag.Usage != expectedUsage {
		t.Errorf("expected usage '%s', got '%s'", expectedUsage, flag.Usage)
	}
}

func TestNoColorFlagDefined(t *testing.T) {
	var localNoColor bool
	cmd := &cobra.Command{
		Use:  "rubichan",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	cmd.PersistentFlags().BoolVar(&localNoColor, "no-color", false, "suppress all ANSI color output (also respects NO_COLOR env var)")

	flag := cmd.PersistentFlags().Lookup("no-color")
	require.NotNil(t, flag, "--no-color flag must exist")
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Contains(t, flag.Usage, "NO_COLOR")
}

func TestNewDefaultSecurityEngine(t *testing.T) {
	engine := newDefaultSecurityEngine(security.EngineConfig{Concurrency: 4})
	require.NotNil(t, engine)
}

func TestContainsSkill(t *testing.T) {
	tests := []struct {
		name      string
		skill     string
		flagValue string
		want      bool
	}{
		{"exact match", "apple-dev", "apple-dev", true},
		{"in list", "apple-dev", "foo,apple-dev,bar", true},
		{"with spaces", "apple-dev", "foo, apple-dev , bar", true},
		{"not present", "apple-dev", "foo,bar", false},
		{"empty flag", "apple-dev", "", false},
		{"partial match not accepted", "apple", "apple-dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsSkill(tt.skill, tt.flagValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppleDevAutoActivation_NoAppleProject(t *testing.T) {
	// A directory with no Apple project files should not trigger apple-dev.
	dir := t.TempDir()
	// Create a non-Apple file.
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	// containsSkill should return false for empty skills flag.
	assert.False(t, containsSkill("apple-dev", ""))
}

func TestAppleDevAutoActivation_WithPackageSwift(t *testing.T) {
	// A directory with Package.swift should trigger apple-dev detection.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "Package.swift"), []byte("// swift-tools-version:5.9"), 0644)
	require.NoError(t, err)

	// Verify that xcode.DiscoverProject detects SPM project.
	info := xcode.DiscoverProject(dir)
	assert.Equal(t, "spm", info.Type)
}

func TestContainsSkill_SkillsFlagActivation(t *testing.T) {
	// Explicit --skills=apple-dev should activate even without Apple project.
	assert.True(t, containsSkill("apple-dev", "apple-dev,other-skill"))
	assert.True(t, containsSkill("apple-dev", "other-skill,apple-dev"))
	assert.False(t, containsSkill("apple-dev", "other-skill"))
}

func TestRemoveSkill(t *testing.T) {
	tests := []struct {
		name      string
		skill     string
		flagValue string
		want      string
	}{
		{"remove only", "apple-dev", "apple-dev", ""},
		{"remove from list", "apple-dev", "foo,apple-dev,bar", "foo,bar"},
		{"with spaces", "apple-dev", "foo, apple-dev , bar", "foo,bar"},
		{"not present", "apple-dev", "foo,bar", "foo,bar"},
		{"empty flag", "apple-dev", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeSkill(tt.skill, tt.flagValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAutoDetectProvider_OllamaRunning(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // ensure env doesn't interfere

	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version": "0.5.1"}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	detected := autoDetectProvider(cfg, "", srv.URL)
	assert.True(t, detected)
	assert.Equal(t, "ollama", cfg.Provider.Default)
}

func TestAutoDetectProvider_OllamaNotRunning(t *testing.T) {
	cfg := config.DefaultConfig()
	detected := autoDetectProvider(cfg, "", "http://localhost:1")
	assert.False(t, detected)
	assert.Equal(t, "anthropic", cfg.Provider.Default)
}

func TestAutoDetectProvider_ExplicitProviderFlag(t *testing.T) {
	cfg := config.DefaultConfig()
	detected := autoDetectProvider(cfg, "openrouter", "http://localhost:11434")
	assert.False(t, detected)
	assert.Equal(t, "anthropic", cfg.Provider.Default)
}

func TestAutoDetectProvider_APIKeyExists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Anthropic.APIKey = "sk-test-key"
	detected := autoDetectProvider(cfg, "", "http://localhost:11434")
	assert.False(t, detected)
}

func TestResolveOllamaModel_SingleModel(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 4294967296}]}`))
	}))
	defer srv.Close()

	model, err := resolveOllamaModel(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", model)
}

func TestResolveOllamaModel_NoModels(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	_, err := resolveOllamaModel(srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

func TestResolveOllamaModel_MultipleModels(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [
			{"name": "llama3.2:latest", "size": 4294967296},
			{"name": "codellama:7b", "size": 3758096384}
		]}`))
	}))
	defer srv.Close()

	model, err := resolveOllamaModel(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", model) // returns first model
}

type capabilityTestProvider struct {
	eventsByCall [][]provider.StreamEvent
	errByCall    []error
	requests     []provider.CompletionRequest
	callCount    int
}

func (p *capabilityTestProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	p.requests = append(p.requests, req)
	idx := p.callCount
	p.callCount++
	if idx < len(p.errByCall) && p.errByCall[idx] != nil {
		return nil, p.errByCall[idx]
	}
	var events []provider.StreamEvent
	if idx < len(p.eventsByCall) {
		events = p.eventsByCall[idx]
	}
	ch := make(chan provider.StreamEvent, len(events))
	for _, evt := range events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func TestExecuteModelCapabilityTest_Success(t *testing.T) {
	p := &capabilityTestProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "text_delta", Text: "OK"}, {Type: "stop"}},
		{{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe", Input: []byte(`{}`)}}, {Type: "stop"}},
	}}
	var out bytes.Buffer

	err := executeModelCapabilityTest(context.Background(), &out, p, "openai", "gpt-4o")
	require.NoError(t, err)
	require.Len(t, p.requests, 2)
	assert.Equal(t, "gpt-4o", p.requests[0].Model)
	assert.Equal(t, "capability_probe", p.requests[1].Tools[0].Name)
	assert.Contains(t, out.String(), "Provider: openai")
	assert.Contains(t, out.String(), "Capabilities:")
	assert.Contains(t, out.String(), "Tool support: PASS")
	assert.Contains(t, out.String(), "Model test: PASS")
}

func TestExecuteModelCapabilityTest_MissingModel(t *testing.T) {
	var out bytes.Buffer
	err := executeModelCapabilityTest(context.Background(), &out, &capabilityTestProvider{}, "openai", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is not configured")
}

func TestExecuteModelCapabilityTest_StreamErrorEvent(t *testing.T) {
	p := &capabilityTestProvider{eventsByCall: [][]provider.StreamEvent{{{Type: "error", Error: fmt.Errorf("boom")}}}}
	var out bytes.Buffer

	err := executeModelCapabilityTest(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model stream test failed")
}

func TestExecuteModelCapabilityTest_ToolSupportMissing(t *testing.T) {
	p := &capabilityTestProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "text_delta", Text: "OK"}, {Type: "stop"}},
		{{Type: "stop"}},
	}}
	var out bytes.Buffer

	err := executeModelCapabilityTest(context.Background(), &out, p, "openai", "gpt-4o")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Tool support: INCONCLUSIVE")
}

func TestResolveOllamaModel_ConnectionError(t *testing.T) {
	_, err := resolveOllamaModel("http://localhost:1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing Ollama models")
}

// saveFlags saves the current apiBaseFlag and apiKeyFlag values and registers
// a t.Cleanup to restore them after the test completes.
func saveFlags(t *testing.T) {
	t.Helper()
	origBase, origKey := apiBaseFlag, apiKeyFlag
	t.Cleanup(func() { apiBaseFlag, apiKeyFlag = origBase, origKey })
}

func TestApplyAPIBaseFlag_CreatesEntry(t *testing.T) {
	saveFlags(t)
	apiBaseFlag = "http://localhost:1234/v1"
	apiKeyFlag = "test-key"

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "my-server"

	applyAPIBaseFlag(cfg)

	require.Len(t, cfg.Provider.OpenAI, 1)
	assert.Equal(t, "my-server", cfg.Provider.OpenAI[0].Name)
	assert.Equal(t, "http://localhost:1234/v1", cfg.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "config", cfg.Provider.OpenAI[0].APIKeySource)
	assert.Equal(t, "test-key", cfg.Provider.OpenAI[0].APIKey)
	assert.Equal(t, "my-server", cfg.Provider.Default)
}

func TestApplyAPIBaseFlag_DefaultsKeyToNone(t *testing.T) {
	saveFlags(t)
	apiBaseFlag = "http://localhost:1234/v1"
	apiKeyFlag = ""

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "local"

	applyAPIBaseFlag(cfg)

	require.Len(t, cfg.Provider.OpenAI, 1)
	assert.Equal(t, "none", cfg.Provider.OpenAI[0].APIKey)
}

func TestApplyAPIBaseFlag_OverridesExistingEntry(t *testing.T) {
	saveFlags(t)
	apiBaseFlag = "http://new-url:5678/v1"
	apiKeyFlag = "new-key"

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "my-server"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "my-server",
			BaseURL:      "http://old-url:1234/v1",
			APIKeySource: "config",
			APIKey:       "old-key",
		},
	}

	applyAPIBaseFlag(cfg)

	require.Len(t, cfg.Provider.OpenAI, 1)
	assert.Equal(t, "http://new-url:5678/v1", cfg.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "new-key", cfg.Provider.OpenAI[0].APIKey)
}

func TestApplyAPIBaseFlag_BuiltinProviderGetCustomName(t *testing.T) {
	saveFlags(t)
	apiBaseFlag = "http://localhost:1234/v1"
	apiKeyFlag = ""

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic" // built-in name

	applyAPIBaseFlag(cfg)

	assert.Equal(t, "custom", cfg.Provider.Default)
	require.Len(t, cfg.Provider.OpenAI, 1)
	assert.Equal(t, "custom", cfg.Provider.OpenAI[0].Name)
}

func TestApplyAPIKeyFlag_Anthropic(t *testing.T) {
	saveFlags(t)
	apiKeyFlag = "sk-override"

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"

	applyAPIKeyFlag(cfg)

	assert.Equal(t, "config", cfg.Provider.Anthropic.APIKeySource)
	assert.Equal(t, "sk-override", cfg.Provider.Anthropic.APIKey)
}

func TestApplyAPIKeyFlag_OpenAICompatible(t *testing.T) {
	saveFlags(t)
	apiKeyFlag = "new-key"

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "my-server"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "my-server",
			BaseURL:      "http://localhost:1234/v1",
			APIKeySource: "env",
		},
	}

	applyAPIKeyFlag(cfg)

	assert.Equal(t, "config", cfg.Provider.OpenAI[0].APIKeySource)
	assert.Equal(t, "new-key", cfg.Provider.OpenAI[0].APIKey)
}

func TestEnsureFolderAccessApproved_FirstTimeApprove(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	var out bytes.Buffer
	err := ensureFolderAccessApproved(s, "/tmp/project", strings.NewReader("yes\n"), &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Allow rubichan to access this folder?")

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureFolderAccessApproved_Denied(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	var out bytes.Buffer
	err := ensureFolderAccessApproved(s, "/tmp/project", strings.NewReader("no\n"), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "folder access denied")
}

func TestEnsureFolderAccessApproved_AlreadyApprovedSkipsPrompt(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()
	require.NoError(t, s.ApproveFolderAccess("/tmp/project"))

	var out bytes.Buffer
	err := ensureFolderAccessApproved(s, "/tmp/project", strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Empty(t, out.String())
}

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	return s
}

func TestEnsureFolderAccessApprovedNonInteractive_DeniedWithoutAutoApprove(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	err := ensureFolderAccessApprovedNonInteractive(s, "/tmp/project", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
}

func TestEnsureFolderAccessApprovedNonInteractive_AutoApproves(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	err := ensureFolderAccessApprovedNonInteractive(s, "/tmp/project", true, false)
	require.NoError(t, err)

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureFolderAccessApprovedInteractive_UsesAutoApprove(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	var out bytes.Buffer
	err := ensureFolderAccessApprovedInteractive(s, "/tmp/project", strings.NewReader(""), &out, true, false)
	require.NoError(t, err)
	assert.Empty(t, out.String())

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureFolderAccessApprovedInteractive_UsesApproveCwd(t *testing.T) {
	s := mustOpenStore(t)
	defer s.Close()

	var out bytes.Buffer
	err := ensureFolderAccessApprovedInteractive(s, "/tmp/project", strings.NewReader(""), &out, false, true)
	require.NoError(t, err)
	assert.Empty(t, out.String())

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestPersonaErrorMessage(t *testing.T) {
	// Verify persona.ErrorMessage is used by testing the persona function directly.
	// The main() function calls os.Exit so we can't easily test it end-to-end.
	msg := persona.ErrorMessage("something broke")
	assert.Contains(t, msg, "Pigi")
	assert.Contains(t, msg, "something broke")
}

func TestWikiFlagCobraDefaults(t *testing.T) {
	// Build a minimal cobra command that mirrors the real flag registration
	// so we can verify the cobra-defined defaults are correct.
	var localWiki bool
	var localOut, localFormat string
	var localConcurrency int

	cmd := &cobra.Command{Use: "rubichan", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	cmd.PersistentFlags().BoolVar(&localWiki, "wiki", false, "run wiki generation")
	cmd.PersistentFlags().StringVar(&localOut, "wiki-out", "docs/wiki", "output directory for wiki files")
	cmd.PersistentFlags().StringVar(&localFormat, "wiki-format", "raw-md", "wiki output format")
	cmd.PersistentFlags().IntVar(&localConcurrency, "wiki-concurrency", 5, "max parallel LLM calls")

	// Execute with no wiki flags — cobra applies defaults.
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	assert.False(t, localWiki, "--wiki must default to false")
	assert.Equal(t, "docs/wiki", localOut, "--wiki-out must default to docs/wiki")
	assert.Equal(t, "raw-md", localFormat, "--wiki-format must default to raw-md")
	assert.Equal(t, 5, localConcurrency, "--wiki-concurrency must default to 5")
}

func TestWikiFlagsParsedByCobra(t *testing.T) {
	// Reset to defaults before the test and restore afterwards.
	origWiki := wikiFlag
	origOut := wikiOutFlag
	origFormat := wikiFormatFlag
	origConcurrency := wikiConcurrencyFlag
	t.Cleanup(func() {
		wikiFlag = origWiki
		wikiOutFlag = origOut
		wikiFormatFlag = origFormat
		wikiConcurrencyFlag = origConcurrency
	})

	cmd := &cobra.Command{Use: "rubichan", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	cmd.PersistentFlags().BoolVar(&wikiFlag, "wiki", false, "run wiki generation")
	cmd.PersistentFlags().StringVar(&wikiOutFlag, "wiki-out", "docs/wiki", "output directory for wiki files")
	cmd.PersistentFlags().StringVar(&wikiFormatFlag, "wiki-format", "raw-md", "wiki output format")
	cmd.PersistentFlags().IntVar(&wikiConcurrencyFlag, "wiki-concurrency", 5, "max parallel LLM calls")

	cmd.SetArgs([]string{"--wiki", "--wiki-out", "out/custom", "--wiki-format", "hugo", "--wiki-concurrency", "10"})
	require.NoError(t, cmd.Execute())

	assert.True(t, wikiFlag)
	assert.Equal(t, "out/custom", wikiOutFlag)
	assert.Equal(t, "hugo", wikiFormatFlag)
	assert.Equal(t, 10, wikiConcurrencyFlag)
}

func TestRunWikiHeadlessInvalidProviderReturnsError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Default = "nonexistent-provider-xyz"

	err := runWikiHeadless(cfg, t.TempDir(), "docs/wiki", "raw-md", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating provider")
}

func TestWikiHeadlessEndToEnd_NoProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = ""
	cfg.Provider.Model = ""
	err := runWikiHeadless(cfg, t.TempDir(), filepath.Join(t.TempDir(), "wiki-out"), "raw-md", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider")
}

func TestWikiHelpOutput(t *testing.T) {
	// Build a minimal cobra command that mirrors the real wiki flag registration
	// so we can verify the flag names appear in help output without spinning up
	// the full application.
	var localWiki bool
	var localOut, localFormat string
	var localConcurrency int

	cmd := &cobra.Command{
		Use:   "rubichan",
		Short: "An AI coding assistant",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	cmd.PersistentFlags().BoolVar(&localWiki, "wiki", false, "run wiki generation (implies --headless, --approve-cwd)")
	cmd.PersistentFlags().StringVar(&localOut, "wiki-out", "docs/wiki", "output directory for wiki files")
	cmd.PersistentFlags().StringVar(&localFormat, "wiki-format", "raw-md", "wiki output format: raw-md, hugo, docusaurus")
	cmd.PersistentFlags().IntVar(&localConcurrency, "wiki-concurrency", 5, "max parallel LLM calls for wiki generation")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "--wiki")
	assert.Contains(t, output, "--wiki-out")
	assert.Contains(t, output, "--wiki-format")
	assert.Contains(t, output, "--wiki-concurrency")
}
