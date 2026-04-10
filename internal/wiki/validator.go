package wiki

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// knownConflictPairs lists dependency pairs that are commonly conflicting.
var knownConflictPairs = [][2]string{
	{"mattn/go-sqlite3", "modernc.org/sqlite"},
	{"lib/pq", "jackc/pgx"},
	{"go-sql-driver/mysql", "go-mysql-org"},
}

// paramQueryRegex matches parameterized SQL query patterns.
var paramQueryRegex = regexp.MustCompile(`(Exec|Query|QueryRow)\s*\([\s\S]*?(\?|\$\d+|:\w+)`)

// ValidateDocs cross-checks factual claims in wiki documents against
// actual project data. Returns warnings for claims that appear false.
func ValidateDocs(docs []Document, projectFiles map[string]string) []ValidationWarning {
	var warnings []ValidationWarning

	for _, doc := range docs {
		lines := strings.Split(doc.Content, "\n")
		for i, line := range lines {
			lower := strings.ToLower(line)

			// Check 1: Dependency conflict claims.
			if strings.Contains(lower, "conflicting") &&
				(strings.Contains(lower, "driver") || strings.Contains(lower, "dependenc")) {
				if w := checkDependencyConflict(doc.Path, i+1, line, projectFiles); w != nil {
					warnings = append(warnings, *w)
				}
			}

			// Check 2: "Broken SQL" / "string concatenation" claims.
			if (strings.Contains(lower, "broken sql") || strings.Contains(lower, "string concatenat")) &&
				strings.Contains(lower, "sql") {
				if w := checkBrokenSQL(doc.Path, i+1, line, projectFiles); w != nil {
					warnings = append(warnings, *w)
				}
			}

			// Check 3: "Missing source" claims.
			if strings.Contains(lower, "missing source") || strings.Contains(lower, "missing code") {
				if w := checkMissingSource(doc.Path, i+1, line, projectFiles); w != nil {
					warnings = append(warnings, *w)
				}
			}
		}
	}

	return warnings
}

// checkDependencyConflict verifies whether a dependency conflict claim is real.
func checkDependencyConflict(docPath string, lineNum int, line string, projectFiles map[string]string) *ValidationWarning {
	lower := strings.ToLower(line)

	// Gather all manifest content.
	var manifestContent string
	for path, content := range projectFiles {
		base := filepath.Base(path)
		if base == "go.mod" || base == "package.json" || base == "Cargo.toml" || base == "requirements.txt" {
			manifestContent += content + "\n"
		}
	}
	if manifestContent == "" {
		return nil // no manifests to check against
	}

	for _, pair := range knownConflictPairs {
		// Check if either member of the pair is mentioned in the claim.
		mentionsFirst := strings.Contains(lower, strings.ToLower(pair[0]))
		mentionsSecond := strings.Contains(lower, strings.ToLower(pair[1]))
		if !mentionsFirst && !mentionsSecond {
			continue
		}

		// Check if both are actually present in manifests.
		hasFirst := strings.Contains(manifestContent, pair[0])
		hasSecond := strings.Contains(manifestContent, pair[1])
		if hasFirst && hasSecond {
			continue // real conflict
		}

		// Only one (or neither) present — false claim.
		var found string
		if hasFirst {
			found = pair[0] + " only"
		} else if hasSecond {
			found = pair[1] + " only"
		} else {
			found = "neither present"
		}
		return &ValidationWarning{
			Document: docPath,
			Line:     lineNum,
			Claim:    strings.TrimSpace(line),
			Check:    "dependency conflict: " + pair[0] + " vs " + pair[1],
			Result:   found,
		}
	}

	return nil
}

// checkBrokenSQL verifies whether a "broken SQL" claim is real by looking
// for parameterized queries in store/db directories.
func checkBrokenSQL(docPath string, lineNum int, line string, projectFiles map[string]string) *ValidationWarning {
	for path, content := range projectFiles {
		if content == "" {
			continue // presence marker only
		}
		if !isStoreOrDBPath(path) {
			continue
		}
		if paramQueryRegex.MatchString(content) {
			return &ValidationWarning{
				Document: docPath,
				Line:     lineNum,
				Claim:    strings.TrimSpace(line),
				Check:    "SQL parameterization in " + path,
				Result:   "parameterized queries found",
			}
		}
	}
	return nil
}

// checkMissingSource verifies whether a "missing source" claim is real by
// checking if the mentioned directory has files in projectFiles.
func checkMissingSource(docPath string, lineNum int, line string, projectFiles map[string]string) *ValidationWarning {
	// Extract directory name from backticks or quotes.
	dir := extractDirFromClaim(line)
	if dir == "" {
		return nil
	}

	for path := range projectFiles {
		if strings.HasPrefix(path, dir+"/") || strings.HasPrefix(path, dir+"\\") || path == dir {
			return &ValidationWarning{
				Document: docPath,
				Line:     lineNum,
				Claim:    strings.TrimSpace(line),
				Check:    "directory existence: " + dir,
				Result:   "files found in " + dir,
			}
		}
	}
	return nil
}

// extractDirFromClaim extracts a directory name from backticks or quotes in a line.
func extractDirFromClaim(line string) string {
	// Try backticks first.
	if idx := strings.Index(line, "`"); idx >= 0 {
		rest := line[idx+1:]
		if end := strings.Index(rest, "`"); end >= 0 {
			return strings.TrimRight(rest[:end], "/")
		}
	}
	// Try double quotes.
	if idx := strings.Index(line, `"`); idx >= 0 {
		rest := line[idx+1:]
		if end := strings.Index(rest, `"`); end >= 0 {
			return strings.TrimRight(rest[:end], "/")
		}
	}
	return ""
}

// isStoreOrDBPath checks if a file path is in a store or db directory.
func isStoreOrDBPath(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, p := range parts {
		if p == "store" || p == "db" || p == "database" || p == "repository" {
			return true
		}
	}
	return false
}

// stripFalseClaims removes lines flagged by validation from documents.
func stripFalseClaims(docs []Document, warnings []ValidationWarning) []Document {
	if len(warnings) == 0 {
		return docs
	}

	// Build map: docPath -> set of line numbers to remove.
	flagged := make(map[string]map[int]bool)
	for _, w := range warnings {
		if flagged[w.Document] == nil {
			flagged[w.Document] = make(map[int]bool)
		}
		flagged[w.Document][w.Line] = true
	}

	result := make([]Document, len(docs))
	for i, doc := range docs {
		linesToRemove, hasFlagged := flagged[doc.Path]
		if !hasFlagged {
			result[i] = doc
			continue
		}

		lines := strings.Split(doc.Content, "\n")
		var kept []string
		for j, line := range lines {
			if !linesToRemove[j+1] { // line numbers are 1-based
				kept = append(kept, line)
			}
		}

		result[i] = Document{
			Path:    doc.Path,
			Title:   doc.Title,
			Content: strings.Join(kept, "\n"),
		}
	}

	return result
}

// readProjectFiles reads key project files for validation.
func readProjectFiles(dir string, files []ScannedFile) map[string]string {
	projectFiles := make(map[string]string)

	// Read manifest files.
	manifests := []string{"go.mod", "package.json", "Cargo.toml", "requirements.txt"}
	for _, m := range manifests {
		path := filepath.Join(dir, m)
		data, err := os.ReadFile(path)
		if err == nil {
			projectFiles[m] = string(data)
		}
	}

	// Record all scanned files as presence markers and read store/db files.
	for _, f := range files {
		if _, exists := projectFiles[f.Path]; !exists {
			projectFiles[f.Path] = "" // presence marker
		}
		if isStoreOrDBPath(f.Path) {
			data, err := os.ReadFile(filepath.Join(dir, f.Path))
			if err == nil {
				projectFiles[f.Path] = string(data)
			}
		}
	}

	return projectFiles
}
