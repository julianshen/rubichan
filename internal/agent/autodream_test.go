package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestConsolidationLockReadLastConsolidatedAt(t *testing.T) {
	dir := t.TempDir()
	lock := NewConsolidationLock(dir)

	last, err := lock.ReadLastConsolidatedAt()
	require.NoError(t, err)
	require.True(t, last.IsZero())

	_, err = lock.TryAcquire()
	require.NoError(t, err)

	last, err = lock.ReadLastConsolidatedAt()
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), last, time.Second)
}

func TestConsolidationLockTryAcquire(t *testing.T) {
	dir := t.TempDir()
	lock := NewConsolidationLock(dir)

	prior, err := lock.TryAcquire()
	require.NoError(t, err)
	require.Nil(t, prior)

	prior, err = lock.TryAcquire()
	require.NoError(t, err)
	require.NotNil(t, prior)
	require.WithinDuration(t, time.Now(), *prior, time.Second)
}

func TestConsolidationLockRollback(t *testing.T) {
	dir := t.TempDir()
	lock := NewConsolidationLock(dir)

	prior, err := lock.TryAcquire()
	require.NoError(t, err)
	require.Nil(t, prior)

	time.Sleep(10 * time.Millisecond)

	prior, err = lock.TryAcquire()
	require.NoError(t, err)
	require.NotNil(t, prior)

	err = lock.Rollback(prior)
	require.NoError(t, err)

	last, err := lock.ReadLastConsolidatedAt()
	require.NoError(t, err)
	require.WithinDuration(t, *prior, last, time.Millisecond)
}

func TestConsolidationLockRollbackRemove(t *testing.T) {
	dir := t.TempDir()
	lock := NewConsolidationLock(dir)

	_, err := lock.TryAcquire()
	require.NoError(t, err)

	err = lock.Rollback(nil)
	require.NoError(t, err)

	_, err = os.Stat(lock.lockPath())
	require.True(t, os.IsNotExist(err))
}

func TestConsolidationLockRecordConsolidation(t *testing.T) {
	dir := t.TempDir()
	lock := NewConsolidationLock(dir)

	err := lock.RecordConsolidation()
	require.NoError(t, err)

	last, err := lock.ReadLastConsolidatedAt()
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), last, time.Second)
}

func TestAutoDreamServiceIsGateOpen(t *testing.T) {
	s := NewAutoDreamService("/tmp", AutoDreamConfig{MinHours: 24, MinSessions: 5})
	require.True(t, s.IsGateOpen())

	s2 := NewAutoDreamService("/tmp", AutoDreamConfig{MinHours: 0, MinSessions: 5})
	require.False(t, s2.IsGateOpen())
}

func TestAutoDreamServiceShouldRun(t *testing.T) {
	s := NewAutoDreamService("/tmp", AutoDreamConfig{MinHours: 24, MinSessions: 2})

	sessions := []SessionInfo{
		{SessionID: "s1", MTime: time.Now().Add(-1 * time.Hour)},
		{SessionID: "s2", MTime: time.Now().Add(-2 * time.Hour)},
	}
	lastConsolidated := time.Now().Add(-12 * time.Hour)
	require.False(t, s.ShouldRun(sessions, lastConsolidated, ""))

	lastConsolidated = time.Now().Add(-48 * time.Hour)
	sessions = []SessionInfo{
		{SessionID: "s1", MTime: time.Now().Add(-1 * time.Hour)},
	}
	require.False(t, s.ShouldRun(sessions, lastConsolidated, ""))

	sessions = []SessionInfo{
		{SessionID: "s1", MTime: time.Now().Add(-1 * time.Hour)},
		{SessionID: "s2", MTime: time.Now().Add(-2 * time.Hour)},
		{SessionID: "s3", MTime: time.Now().Add(-3 * time.Hour)},
	}
	require.True(t, s.ShouldRun(sessions, lastConsolidated, ""))
	require.True(t, s.ShouldRun(sessions, lastConsolidated, "s1"))
}

func TestAutoDreamServiceExecuteDream(t *testing.T) {
	dir := t.TempDir()
	s := NewAutoDreamService(dir, AutoDreamConfig{MinHours: 24, MinSessions: 5})

	called := false
	callModel := func(ctx context.Context, prompt string) (string, error) {
		called = true
		require.Contains(t, prompt, "Dream: Memory Consolidation")
		require.Contains(t, prompt, "/tmp/memories")
		return "consolidated", nil
	}

	params := agentsdk.DreamParams{
		MemoryRoot:    "/tmp/memories",
		TranscriptDir: "/tmp/transcripts",
	}

	err := s.ExecuteDream(context.Background(), params, callModel)
	require.NoError(t, err)
	require.True(t, called)

	lock := NewConsolidationLock(dir)
	last, err := lock.ReadLastConsolidatedAt()
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), last, time.Second)
}

func TestAutoDreamServiceExecuteDreamModelError(t *testing.T) {
	dir := t.TempDir()
	s := NewAutoDreamService(dir, AutoDreamConfig{MinHours: 24, MinSessions: 5})

	callModel := func(ctx context.Context, prompt string) (string, error) {
		return "", errors.New("model failed")
	}

	lock := NewConsolidationLock(dir)
	_, _ = lock.TryAcquire()
	lastBefore, _ := lock.ReadLastConsolidatedAt()

	params := agentsdk.DreamParams{MemoryRoot: "/tmp/memories"}
	err := s.ExecuteDream(context.Background(), params, callModel)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dream model call failed")

	lastAfter, _ := lock.ReadLastConsolidatedAt()
	require.WithinDuration(t, lastBefore, lastAfter, time.Second)
}

func TestAutoDreamServiceExecuteDreamAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	s := NewAutoDreamService(dir, AutoDreamConfig{MinHours: 24, MinSessions: 5})

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	params := agentsdk.DreamParams{MemoryRoot: "/tmp/memories"}
	err := s.ExecuteDream(context.Background(), params, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dream already in progress")
}

func TestBuildConsolidationPrompt(t *testing.T) {
	prompt := BuildConsolidationPrompt("/mem", "/trans", "extra info")
	require.Contains(t, prompt, "Dream: Memory Consolidation")
	require.Contains(t, prompt, "/mem")
	require.Contains(t, prompt, "/trans")
	require.Contains(t, prompt, "extra info")
	require.Contains(t, prompt, "Phase 1")
	require.Contains(t, prompt, "Phase 2")
	require.Contains(t, prompt, "Phase 3")
	require.Contains(t, prompt, "Phase 4")
}

func TestBuildConsolidationPromptNoExtra(t *testing.T) {
	prompt := BuildConsolidationPrompt("/mem", "/trans", "")
	require.NotContains(t, prompt, "Additional context")
}

func TestListSessionsTouchedSince(t *testing.T) {
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "session1.jsonl"), []byte("data"), 0o644)
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "session2.jsonl"), []byte("data"), 0o644)

	since := time.Now().Add(-5 * time.Minute)
	sessions, err := ListSessionsTouchedSince(dir, since)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	sessions, err = ListSessionsTouchedSince("/nonexistent", since)
	require.NoError(t, err)
	require.Nil(t, sessions)
}

func TestListSessionsTouchedSinceFiltersOld(t *testing.T) {
	dir := t.TempDir()

	oldPath := filepath.Join(dir, "old.jsonl")
	_ = os.WriteFile(oldPath, []byte("data"), 0o644)
	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(oldPath, oldTime, oldTime)

	since := time.Now().Add(-24 * time.Hour)
	sessions, err := ListSessionsTouchedSince(dir, since)
	require.NoError(t, err)
	require.Len(t, sessions, 0)
}
