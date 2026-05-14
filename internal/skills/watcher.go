package skills

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SkillWatcher watches skill directories for changes and triggers reloads.
type SkillWatcher struct {
	rt       *Runtime
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	debounce time.Duration
	mu       sync.Mutex
	pending  bool
	started  bool
}

// NewSkillWatcher creates a new watcher for the given runtime.
func NewSkillWatcher(rt *Runtime) (*SkillWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	return &SkillWatcher{
		rt:       rt,
		watcher:  watcher,
		stopCh:   make(chan struct{}),
		debounce: 500 * time.Millisecond,
	}, nil
}

// Start begins watching skill directories. It discovers all directories
// that contain skills and adds them to the watcher. Start is idempotent;
// subsequent calls are no-ops.
func (sw *SkillWatcher) Start() error {
	sw.mu.Lock()
	if sw.started {
		sw.mu.Unlock()
		return nil
	}
	sw.started = true
	sw.mu.Unlock()

	dirs := sw.rt.GetWatchedDirs()
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		// Watch the root directory recursively.
		sw.addWatch(dir, true)
	}

	sw.wg.Add(1)
	go sw.loop()
	return nil
}

// Stop stops the watcher and cleans up resources.
func (sw *SkillWatcher) Stop() {
	sw.stopOnce.Do(func() {
		close(sw.stopCh)
		sw.watcher.Close()
	})
	sw.wg.Wait()
}

// addWatch adds a directory to the watcher if it exists.
// If recursive is true, it also watches all subdirectories.
func (sw *SkillWatcher) addWatch(dir string, recursive bool) {
	if err := sw.watcher.Add(dir); err != nil {
		log.Printf("[skill-watcher] failed to watch %s: %v", dir, err)
		return
	}
	log.Printf("[skill-watcher] watching %s", dir)

	if !recursive {
		return
	}

	// Recursively watch all subdirectories.
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == dir {
			if err != nil {
				log.Printf("[skill-watcher] walk error in %s: %v", dir, err)
				return err
			}
			return nil
		}
		if err := sw.watcher.Add(path); err != nil {
			log.Printf("[skill-watcher] failed to watch %s: %v", path, err)
			return nil
		}
		log.Printf("[skill-watcher] watching %s", path)
		return nil
	}); err != nil {
		log.Printf("[skill-watcher] walk failed for %s: %v", dir, err)
	}
}

// loop processes fsnotify events with debouncing.
func (sw *SkillWatcher) loop() {
	defer sw.wg.Done()

	debounceTimer := time.NewTimer(0)
	<-debounceTimer.C // drain initial timer

	for {
		select {
		case <-sw.stopCh:
			debounceTimer.Stop()
			return
		case event, ok := <-sw.watcher.Events:
			if !ok {
				debounceTimer.Stop()
				return
			}
			// Auto-add watches for newly created directories.
			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err != nil {
					log.Printf("[skill-watcher] stat failed for %s: %v", event.Name, err)
				} else if info.IsDir() && isSkillDir(event.Name) {
					sw.addWatch(event.Name, true)
				}
			}
			if sw.isSkillFile(event.Name) {
				sw.mu.Lock()
				sw.pending = true
				debounceTimer.Reset(sw.debounce)
				sw.mu.Unlock()
			}
		case err, ok := <-sw.watcher.Errors:
			if !ok {
				debounceTimer.Stop()
				return
			}
			log.Printf("[skill-watcher] error: %v", err)
		case <-debounceTimer.C:
			sw.mu.Lock()
			pending := sw.pending
			sw.pending = false
			sw.mu.Unlock()
			if pending {
				// Non-blocking check: if new events arrived during reload,
				// reset the timer instead of reloading immediately.
				select {
				case event, ok := <-sw.watcher.Events:
					if !ok {
						return
					}
					if sw.isSkillFile(event.Name) {
						sw.mu.Lock()
						sw.pending = true
						debounceTimer.Reset(sw.debounce)
						sw.mu.Unlock()
					}
				default:
					sw.reload()
				}
			}
		}
	}
}

// isSkillFile checks if a path is a skill-related file or directory.
func (sw *SkillWatcher) isSkillFile(path string) bool {
	base := filepath.Base(path)
	// Watch SKILL.yaml, SKILL.md, and .md files.
	if base == "SKILL.yaml" || base == "SKILL.md" || strings.HasSuffix(base, ".md") {
		return true
	}
	// Watch skill directories.
	if isSkillDir(path) {
		return true
	}
	return false
}

// isSkillDir checks if a path is within a skill directory.
func isSkillDir(path string) bool {
	// Use filepath separators to avoid false matches like "foo.kilo/skillsbar".
	normalized := filepath.ToSlash(path)
	for _, subdir := range wellKnownSkillSubdirs {
		prefix := subdir + "/"
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
		infix := "/" + subdir + "/"
		if strings.Contains(normalized, infix) {
			return true
		}
	}
	return false
}

// reload triggers a skill rediscovery.
func (sw *SkillWatcher) reload() {
	log.Println("[skill-watcher] reloading skills...")
	if err := sw.rt.Discover(nil); err != nil {
		log.Printf("[skill-watcher] reload failed: %v", err)
		return
	}
	log.Println("[skill-watcher] reload complete")
}
