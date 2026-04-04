package wiki

import (
	"bufio"
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// apiPatternDef describes a single regex-based API detection rule.
type apiPatternDef struct {
	Language    string
	Kind        string
	Regex       *regexp.Regexp
	MethodGroup int // submatch index for HTTP method (0 = none)
	PathGroup   int // submatch index for path/route (0 = none)
	HandlerGroup int // submatch index for handler name (0 = none)
}

// apiPatternDefs is the compiled table of detection rules.
var apiPatternDefs []apiPatternDef

func init() {
	type raw struct {
		language    string
		kind        string
		pattern     string
		methodGroup int
		pathGroup   int
		handlerGroup int
	}

	rawDefs := []raw{
		// Go HTTP — http.HandleFunc("/path", handler)
		{
			language:     "go",
			kind:         "http",
			pattern:      `(?:http\.HandleFunc|\.HandleFunc)\s*\(\s*"([^"]+)"\s*,\s*(\w+)`,
			pathGroup:    1,
			handlerGroup: 2,
		},
		// Go HTTP — mux.Handle("/path", handler)
		{
			language:     "go",
			kind:         "http",
			pattern:      `\.Handle\s*\(\s*"([^"]+)"\s*,\s*(\w+)`,
			pathGroup:    1,
			handlerGroup: 2,
		},
		// Go HTTP gin/echo/chi — .Get("/path", handler) etc.
		{
			language:     "go",
			kind:         "http",
			pattern:      `\.(?i:(Get|Post|Put|Delete|Patch|Options|Head))\s*\(\s*"([^"]+)"\s*,\s*(\w+)`,
			methodGroup:  1,
			pathGroup:    2,
			handlerGroup: 3,
		},
		// Go CLI — cobra.Command
		{
			language: "go",
			kind:     "cli",
			pattern:  `cobra\.Command\s*\{`,
		},
		// Python Flask/FastAPI — @app.route('/path')
		{
			language:  "python",
			kind:      "http",
			pattern:   `@(?:app|router|bp|blueprint)\.route\s*\(\s*['"]([^'"]+)['"]`,
			pathGroup: 1,
		},
		// Python FastAPI/Flask decorators — @router.get('/path')
		{
			language:    "python",
			kind:        "http",
			pattern:     `@(?:app|router|bp|blueprint)\.(get|post|put|delete|patch)\s*\(\s*['"]([^'"]+)['"]`,
			methodGroup: 1,
			pathGroup:   2,
		},
		// Python Click CLI
		{
			language: "python",
			kind:     "cli",
			pattern:  `@click\.command|add_parser\s*\(`,
		},
		// JavaScript/TypeScript Express — app.get('/path', handler)
		{
			language:    "javascript",
			kind:        "http",
			pattern:     `(?:app|router)\.(get|post|put|delete|patch)\s*\(\s*['"]([^'"]+)['"]`,
			methodGroup: 1,
			pathGroup:   2,
		},
		// TypeScript (same patterns as JS)
		{
			language:    "typescript",
			kind:        "http",
			pattern:     `(?:app|router)\.(get|post|put|delete|patch)\s*\(\s*['"]([^'"]+)['"]`,
			methodGroup: 1,
			pathGroup:   2,
		},
		// Java Spring — @GetMapping, @PostMapping etc.
		{
			language:    "java",
			kind:        "http",
			pattern:     `@(Get|Post|Put|Delete|Request)Mapping\s*(?:\(\s*(?:value\s*=\s*)?['"]([^'"]+)['"])?`,
			methodGroup: 1,
			pathGroup:   2,
		},
		// Rust Actix-web / Axum — #[get("/path")]
		{
			language:    "rust",
			kind:        "http",
			pattern:     `#\[(get|post|put|delete|patch)\s*\(\s*"([^"]+)"`,
			methodGroup: 1,
			pathGroup:   2,
		},
	}

	for _, r := range rawDefs {
		apiPatternDefs = append(apiPatternDefs, apiPatternDef{
			Language:     r.language,
			Kind:         r.kind,
			Regex:        regexp.MustCompile(r.pattern),
			MethodGroup:  r.methodGroup,
			PathGroup:    r.pathGroup,
			HandlerGroup: r.handlerGroup,
		})
	}
}

// ScanAPIPatterns scans the given files for API registration points and returns
// all detected APIPattern entries. readFile is used to load file contents,
// allowing callers to substitute a fake for testing.
func ScanAPIPatterns(files []ScannedFile, readFile func(string) ([]byte, error)) []APIPattern {
	var results []APIPattern

	for _, f := range files {
		patterns := scanFile(f, readFile)
		results = append(results, patterns...)
	}

	return results
}

// scanFile processes a single ScannedFile and returns its API patterns.
func scanFile(f ScannedFile, readFile func(string) ([]byte, error)) []APIPattern {
	var results []APIPattern

	// Detect by file extension for proto and graphql regardless of Language field.
	ext := strings.ToLower(filepath.Ext(f.Path))
	switch ext {
	case ".proto":
		results = append(results, scanProto(f, readFile)...)
		return results
	case ".graphql", ".gql":
		results = append(results, scanGraphQL(f, readFile)...)
		return results
	}

	// Collect matching pattern definitions for this language.
	var defs []apiPatternDef
	for _, def := range apiPatternDefs {
		if def.Language == f.Language {
			defs = append(defs, def)
		}
	}

	// For Go files in pkg/ directories, also look for exported functions.
	isGoExport := f.Language == "go" && strings.Contains(f.Path, "pkg/")

	// For JS/TS, also look for export function/class.
	isJSExport := (f.Language == "javascript" || f.Language == "typescript")

	// For Rust, also look for pub fn/struct.
	isRustExport := f.Language == "rust"

	if len(defs) == 0 && !isGoExport && !isJSExport && !isRustExport {
		return nil
	}

	content, err := readFile(f.Path)
	if err != nil {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Apply language-specific regex patterns.
		for _, def := range defs {
			m := def.Regex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			p := APIPattern{
				Kind:     def.Kind,
				File:     f.Path,
				Line:     lineNum,
				Language: f.Language,
			}
			if def.MethodGroup > 0 && def.MethodGroup < len(m) {
				p.Method = strings.ToUpper(m[def.MethodGroup])
			}
			if def.PathGroup > 0 && def.PathGroup < len(m) {
				p.Path = m[def.PathGroup]
			}
			if def.HandlerGroup > 0 && def.HandlerGroup < len(m) {
				p.Handler = m[def.HandlerGroup]
			}
			results = append(results, p)
		}

		// Export detection for Go pkg/ files.
		if isGoExport {
			if p, ok := detectGoExport(line, f.Path, lineNum); ok {
				results = append(results, p)
			}
		}

		// Export detection for JS/TS.
		if isJSExport {
			if p, ok := detectJSExport(line, f.Path, lineNum, f.Language); ok {
				results = append(results, p)
			}
		}

		// Export detection for Rust.
		if isRustExport {
			if p, ok := detectRustExport(line, f.Path, lineNum); ok {
				results = append(results, p)
			}
		}
	}

	return results
}

var (
	goExportRe   = regexp.MustCompile(`^func\s+([A-Z]\w*)`)
	jsExportRe   = regexp.MustCompile(`^export\s+(?:default\s+)?(?:function|class|const|async\s+function)\s+(\w+)`)
	rustExportRe = regexp.MustCompile(`^pub\s+(?:fn|struct|enum|trait)\s+(\w+)`)
)

func detectGoExport(line, path string, lineNum int) (APIPattern, bool) {
	m := goExportRe.FindStringSubmatch(line)
	if m == nil {
		return APIPattern{}, false
	}
	// Exclude test and init functions.
	name := m[1]
	if name == "init" || strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") {
		return APIPattern{}, false
	}
	return APIPattern{
		Kind:     "export",
		Handler:  name,
		File:     path,
		Line:     lineNum,
		Language: "go",
	}, true
}

func detectJSExport(line, path string, lineNum int, lang string) (APIPattern, bool) {
	m := jsExportRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return APIPattern{}, false
	}
	name := ""
	if len(m) > 1 {
		// Verify name starts with letter or underscore (not a keyword).
		if len(m[1]) > 0 && (unicode.IsLetter(rune(m[1][0])) || m[1][0] == '_') {
			name = m[1]
		}
	}
	return APIPattern{
		Kind:     "export",
		Handler:  name,
		File:     path,
		Line:     lineNum,
		Language: lang,
	}, true
}

func detectRustExport(line, path string, lineNum int) (APIPattern, bool) {
	m := rustExportRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return APIPattern{}, false
	}
	return APIPattern{
		Kind:     "export",
		Handler:  m[1],
		File:     path,
		Line:     lineNum,
		Language: "rust",
	}, true
}

// scanProto reads a .proto file and returns a grpc APIPattern per rpc definition.
func scanProto(f ScannedFile, readFile func(string) ([]byte, error)) []APIPattern {
	content, err := readFile(f.Path)
	if err != nil {
		return nil
	}

	rpcRe := regexp.MustCompile(`\brpc\s+(\w+)\s*\(`)
	serviceRe := regexp.MustCompile(`\bservice\s+(\w+)\s*\{`)

	var results []APIPattern
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0
	currentService := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if m := serviceRe.FindStringSubmatch(line); m != nil {
			currentService = m[1]
			continue
		}

		if m := rpcRe.FindStringSubmatch(line); m != nil {
			results = append(results, APIPattern{
				Kind:     "grpc",
				Path:     currentService,
				Handler:  m[1],
				File:     f.Path,
				Line:     lineNum,
				Language: "proto",
			})
		}
	}

	// If no rpc lines found but file is proto, emit one entry for the file.
	if len(results) == 0 {
		results = append(results, APIPattern{
			Kind:     "grpc",
			File:     f.Path,
			Line:     1,
			Language: "proto",
		})
	}

	return results
}

// scanGraphQL reads a .graphql/.gql file and returns graphql APIPatterns.
func scanGraphQL(f ScannedFile, readFile func(string) ([]byte, error)) []APIPattern {
	content, err := readFile(f.Path)
	if err != nil {
		return nil
	}

	typeRe := regexp.MustCompile(`^\s*(?:type|input|interface|enum|union)\s+(\w+)`)

	var results []APIPattern
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if m := typeRe.FindStringSubmatch(line); m != nil {
			results = append(results, APIPattern{
				Kind:     "graphql",
				Handler:  m[1],
				File:     f.Path,
				Line:     lineNum,
				Language: "graphql",
			})
		}
	}

	if len(results) == 0 {
		results = append(results, APIPattern{
			Kind:     "graphql",
			File:     f.Path,
			Line:     1,
			Language: "graphql",
		})
	}

	return results
}
