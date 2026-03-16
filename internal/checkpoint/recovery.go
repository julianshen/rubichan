package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type manifest struct {
	SessionID   string          `json:"session_id"`
	RootDir     string          `json:"root_dir"`
	Checkpoints []manifestEntry `json:"checkpoints"`
}

type manifestEntry struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	Turn      int    `json:"turn"`
	Operation string `json:"operation"`
	Size      int64  `json:"size"`
	Spilled   bool   `json:"spilled"`
}

// writeLock creates a PID lock file in the spill directory.
func (m *Manager) writeLock() error {
	lockPath := filepath.Join(m.spillDir, "session.lock")
	return os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// writeManifest writes the current checkpoint metadata to manifest.json.
// Only spilled checkpoints are included (in-memory ones don't need disk persistence).
func (m *Manager) writeManifest() error {
	m.mu.Lock()
	entries := make([]manifestEntry, 0)
	for _, cp := range m.stack {
		if cp.spilled {
			entries = append(entries, manifestEntry{
				ID: cp.ID, FilePath: cp.FilePath, Turn: cp.Turn,
				Operation: cp.Operation, Size: cp.Size, Spilled: true,
			})
		}
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(manifest{
		SessionID:   filepath.Base(m.spillDir),
		RootDir:     m.rootDir,
		Checkpoints: entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.spillDir, "manifest.json"), data, 0644)
}

// DetectOrphaned scans baseDir for session directories whose PID lock points to a dead process.
func DetectOrphaned(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var orphans []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		lockPath := filepath.Join(baseDir, e.Name(), "session.lock")
		data, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			orphans = append(orphans, e.Name())
			continue
		}
		if !isProcessAlive(pid) {
			orphans = append(orphans, e.Name())
		}
	}
	return orphans, nil
}

// RecoverSession reads the manifest for the given sessionID and restores all spilled checkpoints.
// Returns the list of file paths that were successfully restored.
func RecoverSession(baseDir, sessionID string) ([]string, error) {
	manifestPath := filepath.Join(baseDir, sessionID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var restored []string
	for _, entry := range m.Checkpoints {
		spillPath := filepath.Join(baseDir, sessionID, entry.ID+".bak")
		content, err := os.ReadFile(spillPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(entry.FilePath, content, 0644); err != nil {
			continue
		}
		restored = append(restored, entry.FilePath)
	}
	return restored, nil
}

// CleanupOrphaned removes all orphaned session directories under baseDir.
func CleanupOrphaned(baseDir string) error {
	orphans, err := DetectOrphaned(baseDir)
	if err != nil {
		return err
	}
	for _, id := range orphans {
		os.RemoveAll(filepath.Join(baseDir, id))
	}
	return nil
}

// isProcessAlive returns true if the given PID corresponds to a running process.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
