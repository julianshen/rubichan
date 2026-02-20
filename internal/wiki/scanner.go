package wiki

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/julianshen/rubichan/internal/parser"
)

// langExtensions maps file extensions to language names.
var langExtensions = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".jsx":  "javascript",
	".java": "java",
	".rs":   "rust",
	".rb":   "ruby",
	".c":    "c",
	".h":    "c",
	".cc":   "cpp",
	".cpp":  "cpp",
}

// skipDirs contains directory names that should be excluded from scanning.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"build":        true,
	"dist":         true,
	"__pycache__":  true,
}

// Scan discovers source files in dir and extracts function definitions and
// imports using the provided parser. It uses git ls-files when inside a git
// repository; otherwise it falls back to filepath.WalkDir.
func Scan(ctx context.Context, dir string, p *parser.Parser) ([]ScannedFile, error) {
	relPaths, err := listFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	var files []ScannedFile
	for _, rel := range relPaths {
		if shouldSkip(rel) {
			continue
		}

		absPath := filepath.Join(dir, rel)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}

		sf := ScannedFile{
			Path:   rel,
			Size:   info.Size(),
			Module: moduleFromPath(rel),
		}

		ext := filepath.Ext(rel)
		lang, supported := langExtensions[ext]
		if supported {
			sf.Language = lang
			source, err := os.ReadFile(absPath)
			if err == nil {
				sf.Functions, sf.Imports = parseFile(p, rel, source)
			}
		} else {
			sf.Language = "unknown"
		}

		files = append(files, sf)
	}

	return files, nil
}

// listFiles returns relative file paths under dir. It tries git ls-files
// first; if dir is not a git repo it falls back to filepath.WalkDir.
func listFiles(ctx context.Context, dir string) ([]string, error) {
	paths, err := gitLsFiles(ctx, dir)
	if err == nil {
		return paths, nil
	}
	return walkFiles(dir)
}

// gitLsFiles runs "git ls-files" in dir and returns the output lines.
// Returns an error if dir is not inside a git repository.
func gitLsFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, scanner.Err()
}

// walkFiles uses filepath.WalkDir to list all files under dir, skipping
// directories in the skipDirs set.
func walkFiles(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("wiki scanner: skipping path %q: %v", path, err)
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	return paths, err
}

// shouldSkip returns true if the relative path is inside a directory that
// should be excluded from scanning.
func shouldSkip(relPath string) bool {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range parts {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

// moduleFromPath infers a module grouping from the file's directory.
// Files at the root return "root".
func moduleFromPath(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return "root"
	}
	return filepath.ToSlash(dir)
}

// parseFile parses a single file and returns its functions and imports.
// The returned tree is closed before returning, avoiding use-after-free
// when called in a loop.
func parseFile(p *parser.Parser, filename string, source []byte) ([]parser.FunctionDef, []string) {
	tree, err := p.Parse(filename, source)
	if err != nil {
		return nil, nil
	}
	defer tree.Close()
	return tree.Functions(), tree.Imports()
}
