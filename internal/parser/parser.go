// Package parser provides tree-sitter-based multi-language source code parsing
// with automatic language detection from file extensions. It supports extracting
// function definitions and import statements from parsed syntax trees.
package parser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// FunctionDef represents a function or method definition found in source code.
type FunctionDef struct {
	Name      string
	StartLine int
	EndLine   int
}

// langInfo holds tree-sitter language metadata including which node types
// represent functions and imports for a given programming language.
type langInfo struct {
	lang           *sitter.Language
	funcNodeTypes  []string
	importNodeType []string
}

// registry maps file extensions to language info for auto-detection.
var registry = map[string]langInfo{
	".go": {
		lang:           golang.GetLanguage(),
		funcNodeTypes:  []string{"function_declaration", "method_declaration"},
		importNodeType: []string{"import_declaration"},
	},
	".py": {
		lang:           python.GetLanguage(),
		funcNodeTypes:  []string{"function_definition"},
		importNodeType: []string{"import_statement", "import_from_statement"},
	},
	".js": {
		lang:           javascript.GetLanguage(),
		funcNodeTypes:  []string{"function_declaration"},
		importNodeType: []string{"import_statement"},
	},
	".ts": {
		lang:           typescript.GetLanguage(),
		funcNodeTypes:  []string{"function_declaration"},
		importNodeType: []string{"import_statement"},
	},
	".java": {
		lang:           java.GetLanguage(),
		funcNodeTypes:  []string{"method_declaration", "constructor_declaration"},
		importNodeType: []string{"import_declaration"},
	},
	".rs": {
		lang:           rust.GetLanguage(),
		funcNodeTypes:  []string{"function_item"},
		importNodeType: []string{"use_declaration"},
	},
	".rb": {
		lang:           ruby.GetLanguage(),
		funcNodeTypes:  []string{"method"},
		importNodeType: []string{"call"}, // require/require_relative calls
	},
	".c": {
		lang:           c.GetLanguage(),
		funcNodeTypes:  []string{"function_definition"},
		importNodeType: []string{"preproc_include"},
	},
	".h": {
		lang:           c.GetLanguage(),
		funcNodeTypes:  []string{"function_definition"},
		importNodeType: []string{"preproc_include"},
	},
	".cc": {
		lang:           cpp.GetLanguage(),
		funcNodeTypes:  []string{"function_definition"},
		importNodeType: []string{"preproc_include"},
	},
	".cpp": {
		lang:           cpp.GetLanguage(),
		funcNodeTypes:  []string{"function_definition"},
		importNodeType: []string{"preproc_include"},
	},
}

// Parser wraps tree-sitter to parse source files with automatic language detection.
type Parser struct {
	inner *sitter.Parser
}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{
		inner: sitter.NewParser(),
	}
}

// Parse parses source code from the given filename, auto-detecting the language
// from the file extension. Returns an error for unsupported extensions.
func (p *Parser) Parse(filename string, source []byte) (*Tree, error) {
	ext := filepath.Ext(filename)
	info, ok := registry[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file extension %q: language not in registry", ext)
	}

	p.inner.SetLanguage(info.lang)
	sitterTree, err := p.inner.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	return &Tree{
		tree:   sitterTree,
		source: source,
		info:   info,
	}, nil
}

// Tree wraps a parsed tree-sitter syntax tree with convenience methods
// for extracting functions and imports.
type Tree struct {
	tree   *sitter.Tree
	source []byte
	info   langInfo
}

// RootNode returns the root node of the parsed syntax tree.
func (t *Tree) RootNode() *sitter.Node {
	return t.tree.RootNode()
}

// Functions extracts all function and method definitions from the syntax tree.
func (t *Tree) Functions() []FunctionDef {
	var funcs []FunctionDef
	funcTypes := make(map[string]bool, len(t.info.funcNodeTypes))
	for _, ft := range t.info.funcNodeTypes {
		funcTypes[ft] = true
	}

	walk(t.RootNode(), func(node *sitter.Node) {
		if !funcTypes[node.Type()] {
			return
		}
		name := extractFuncName(node, t.source)
		if name == "" {
			return
		}
		funcs = append(funcs, FunctionDef{
			Name:      name,
			StartLine: int(node.StartPoint().Row) + 1, // 0-indexed to 1-indexed
			EndLine:   int(node.EndPoint().Row) + 1,
		})
	})

	return funcs
}

// Imports extracts import paths/module names from the syntax tree.
func (t *Tree) Imports() []string {
	var imports []string
	importTypes := make(map[string]bool, len(t.info.importNodeType))
	for _, it := range t.info.importNodeType {
		importTypes[it] = true
	}

	walk(t.RootNode(), func(node *sitter.Node) {
		if !importTypes[node.Type()] {
			return
		}
		text := node.Content(t.source)
		paths := extractImportPaths(text, node, t.source)
		imports = append(imports, paths...)
	})

	return imports
}

// walk performs a depth-first traversal of the syntax tree, calling fn for each node.
func walk(node *sitter.Node, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			walk(child, fn)
		}
	}
}

// extractFuncName finds the name identifier within a function/method node.
// It checks "name" field first (Go, Python, JS, TS, Java, Rust, Ruby),
// then checks "declarator" field for C/C++ function_definition nodes.
func extractFuncName(node *sitter.Node, source []byte) string {
	// Try the "name" field first — works for Go, Python, JS, TS, Java, Rust, Ruby
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		return nameNode.Content(source)
	}

	// C/C++: function_definition -> declarator (function_declarator) -> declarator (identifier)
	declNode := node.ChildByFieldName("declarator")
	if declNode != nil {
		innerName := declNode.ChildByFieldName("declarator")
		if innerName != nil {
			return innerName.Content(source)
		}
	}

	return ""
}

// extractImportPaths parses import statement text to extract clean module/package paths.
func extractImportPaths(text string, node *sitter.Node, source []byte) []string {
	nodeType := node.Type()

	switch nodeType {
	case "import_declaration":
		// Go: import "fmt" or import ( "fmt"\n"os" )
		// Java: import java.util.List;
		return extractImportDeclaration(node, source)
	case "import_statement":
		// Python: import os, sys
		// JS/TS: import { foo } from 'bar'
		return extractGenericImport(text)
	case "import_from_statement":
		// Python: from pathlib import Path
		return extractPythonFromImport(text)
	case "use_declaration":
		// Rust: use std::io;
		return extractRustUse(text)
	case "preproc_include":
		// C/C++: #include <stdio.h> or #include "myheader.h"
		return extractCInclude(text)
	case "call":
		// Ruby: require 'foo' or require_relative 'bar'
		return extractRubyRequire(text)
	default:
		return []string{extractImportPath(text)}
	}
}

// extractImportDeclaration handles import declarations for Go and Java.
// For Go: walks children looking for import_spec or interpreted_string_literal.
// For Java: walks children looking for scoped_identifier or identifier.
func extractImportDeclaration(node *sitter.Node, source []byte) []string {
	var paths []string
	seen := make(map[string]bool)

	walk(node, func(n *sitter.Node) {
		var content string
		switch n.Type() {
		case "import_spec":
			// Go: import spec wrapping a string literal
			content = extractImportPath(n.Content(source))
		case "interpreted_string_literal":
			// Go: the actual string literal "fmt"
			content = extractImportPath(n.Content(source))
		case "scoped_identifier":
			// Java: java.util.List — only take the top-level scoped_identifier
			// (avoid duplicates from nested scoped_identifiers)
			if n.Parent() != nil && n.Parent().Type() == "scoped_identifier" {
				return
			}
			content = n.Content(source)
		default:
			return
		}
		if content != "" && !seen[content] {
			seen[content] = true
			paths = append(paths, content)
		}
	})
	return paths
}

// extractGenericImport handles Python "import x, y" and JS/TS "import ... from 'x'" statements.
func extractGenericImport(text string) []string {
	// JS/TS: import { foo } from 'bar'
	if strings.Contains(text, " from ") {
		parts := strings.SplitN(text, " from ", 2)
		if len(parts) == 2 {
			return []string{extractImportPath(parts[1])}
		}
	}

	// Python: import os, sys
	text = strings.TrimPrefix(text, "import ")
	text = strings.TrimSpace(text)
	parts := strings.Split(text, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Handle "import os as operating_system"
		if idx := strings.Index(p, " as "); idx >= 0 {
			p = p[:idx]
		}
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// extractPythonFromImport handles Python "from x import y" statements.
func extractPythonFromImport(text string) []string {
	// from pathlib import Path -> extract "pathlib"
	text = strings.TrimPrefix(text, "from ")
	parts := strings.SplitN(text, " import ", 2)
	if len(parts) >= 1 {
		module := strings.TrimSpace(parts[0])
		if module != "" {
			return []string{module}
		}
	}
	return nil
}

// extractRustUse handles Rust "use std::io;" statements.
func extractRustUse(text string) []string {
	text = strings.TrimPrefix(text, "use ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)
	if text != "" {
		return []string{text}
	}
	return nil
}

// extractCInclude handles C/C++ #include directives.
func extractCInclude(text string) []string {
	text = strings.TrimPrefix(text, "#include")
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "<>\"")
	text = strings.TrimSpace(text)
	if text != "" {
		return []string{text}
	}
	return nil
}

// extractRubyRequire handles Ruby require and require_relative calls.
func extractRubyRequire(text string) []string {
	if !strings.HasPrefix(text, "require") {
		return nil
	}
	// require 'foo' or require_relative 'bar'
	for _, prefix := range []string{"require_relative ", "require "} {
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimPrefix(text, prefix)
			cleaned := extractImportPath(rest)
			if cleaned != "" {
				return []string{cleaned}
			}
		}
	}
	return nil
}

// extractImportPath cleans an import path string by removing quotes, semicolons,
// and other surrounding syntax.
func extractImportPath(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\"'`();")
	text = strings.TrimSpace(text)
	return text
}
