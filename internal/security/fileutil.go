package security

import (
	"os"
	"path/filepath"
	"strings"
)

// CollectFiles walks the scan target and returns matching file paths (relative to RootDir).
// If target.Files is set, uses those directly (filtering by extensions if provided).
// Otherwise walks RootDir, skipping excluded patterns and filtering by extensions.
func CollectFiles(target ScanTarget, extensions []string) ([]string, error) {
	if len(target.Files) > 0 {
		if len(extensions) == 0 {
			return target.Files, nil
		}
		extSet := toExtSet(extensions)
		var filtered []string
		for _, f := range target.Files {
			if extSet[filepath.Ext(f)] {
				filtered = append(filtered, f)
			}
		}
		return filtered, nil
	}

	extSet := toExtSet(extensions)
	var files []string
	err := filepath.Walk(target.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(target.RootDir, path)
		if err != nil {
			return nil
		}

		if IsExcluded(relPath, target.ExcludePatterns) {
			return nil
		}

		if len(extSet) > 0 {
			if !extSet[filepath.Ext(relPath)] {
				return nil
			}
		}

		files = append(files, relPath)
		return nil
	})
	return files, err
}

// IsExcluded returns true if the relative path matches any of the exclude patterns.
func IsExcluded(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		if strings.Contains(pattern, "**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if strings.HasPrefix(relPath, prefix+"/") || relPath == prefix {
				return true
			}
		}
	}
	return false
}

// toExtSet converts a slice of extensions into a set for O(1) lookup.
func toExtSet(extensions []string) map[string]bool {
	if len(extensions) == 0 {
		return nil
	}
	set := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		set[ext] = true
	}
	return set
}
