package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// tomlHookEntry is the TOML representation of a single hook in hooks.toml.
type tomlHookEntry struct {
	Event       string `toml:"event"`
	Command     string `toml:"command"`
	MatchTool   string `toml:"match_tool"`
	Timeout     string `toml:"timeout"`
	Description string `toml:"description"`
}

// tomlHooksFile is the top-level structure of a hooks.toml file.
type tomlHooksFile struct {
	Hooks []tomlHookEntry `toml:"hooks"`
}

// LoadHooksTOML reads .agent/hooks.toml from the given project root directory
// and converts entries into UserHookConfig values. Returns an empty slice if
// the file does not exist.
func LoadHooksTOML(projectRoot string) ([]UserHookConfig, error) {
	path := filepath.Join(projectRoot, ".agent", "hooks.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var file tomlHooksFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	configs := make([]UserHookConfig, 0, len(file.Hooks))
	for _, entry := range file.Hooks {
		timeout := defaultTimeout
		if entry.Timeout != "" {
			if parsed, parseErr := time.ParseDuration(entry.Timeout); parseErr == nil {
				timeout = parsed
			}
		}
		configs = append(configs, UserHookConfig{
			Event:       entry.Event,
			Pattern:     entry.MatchTool,
			Command:     entry.Command,
			Description: entry.Description,
			Timeout:     timeout,
			Source:      ".agent/hooks.toml",
		})
	}
	return configs, nil
}
