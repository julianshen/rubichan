package parser

import (
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGoFile(t *testing.T) {
	p := NewParser()
	source := []byte(`package main

func main() {
	println("hello")
}
`)
	tree, err := p.Parse("main.go", source)
	require.NoError(t, err)
	defer tree.Close()
	assert.NotNil(t, tree)
	assert.NotNil(t, tree.RootNode())
}

func TestParsePythonFile(t *testing.T) {
	p := NewParser()
	source := []byte(`def hello():
    print("hello")

def world():
    print("world")
`)
	tree, err := p.Parse("hello.py", source)
	require.NoError(t, err)
	defer tree.Close()
	assert.NotNil(t, tree)
	assert.NotNil(t, tree.RootNode())
}

func TestParseUnknownExtension(t *testing.T) {
	p := NewParser()
	source := []byte(`some content`)
	_, err := p.Parse("file.xyz", source)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unsupported"),
		"error should contain 'unsupported', got: %s", err.Error())
}

func TestFunctionsExtraction(t *testing.T) {
	p := NewParser()
	source := []byte(`package main

func hello() {
	println("hello")
}

func world(name string) string {
	return "world " + name
}
`)
	tree, err := p.Parse("main.go", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)

	assert.Equal(t, "hello", funcs[0].Name)
	assert.Equal(t, 3, funcs[0].StartLine)
	assert.Equal(t, 5, funcs[0].EndLine)

	assert.Equal(t, "world", funcs[1].Name)
	assert.Equal(t, 7, funcs[1].StartLine)
	assert.Equal(t, 9, funcs[1].EndLine)
}

func TestImportsExtraction(t *testing.T) {
	p := NewParser()
	source := []byte(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println("hello")
}
`)
	tree, err := p.Parse("main.go", source)
	require.NoError(t, err)
	defer tree.Close()

	imports := tree.Imports()
	require.Len(t, imports, 3)
	assert.Contains(t, imports, "fmt")
	assert.Contains(t, imports, "os")
	assert.Contains(t, imports, "strings")
}

func TestParsePythonFunctions(t *testing.T) {
	p := NewParser()
	source := []byte(`def greet(name):
    print(f"Hello, {name}")

def farewell():
    print("Goodbye")
`)
	tree, err := p.Parse("script.py", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "greet", funcs[0].Name)
	assert.Equal(t, "farewell", funcs[1].Name)
}

func TestParsePythonImports(t *testing.T) {
	p := NewParser()
	source := []byte(`import os
import sys
from pathlib import Path

def main():
    pass
`)
	tree, err := p.Parse("script.py", source)
	require.NoError(t, err)
	defer tree.Close()

	imports := tree.Imports()
	assert.Contains(t, imports, "os")
	assert.Contains(t, imports, "sys")
	assert.Contains(t, imports, "pathlib")
}

func TestParseJavaScriptFile(t *testing.T) {
	p := NewParser()
	source := []byte(`function hello() {
  console.log("hello");
}

function world(name) {
  return "world " + name;
}
`)
	tree, err := p.Parse("app.js", source)
	require.NoError(t, err)
	defer tree.Close()
	assert.NotNil(t, tree)

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "hello", funcs[0].Name)
	assert.Equal(t, "world", funcs[1].Name)
}

func TestParseJSModernPatterns(t *testing.T) {
	p := NewParser()
	source := []byte(`const greet = (name) => {
  console.log(name);
};

const add = function(a, b) {
  return a + b;
};

class Calculator {
  multiply(a, b) {
    return a * b;
  }
}
`)
	tree, err := p.Parse("modern.js", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 3)
	assert.Equal(t, "greet", funcs[0].Name)
	assert.Equal(t, "add", funcs[1].Name)
	assert.Equal(t, "multiply", funcs[2].Name)
}

func TestParseJSSideEffectImport(t *testing.T) {
	p := NewParser()
	source := []byte(`import 'side-effect-module';
import "polyfill";
import { useState } from 'react';
`)
	tree, err := p.Parse("imports.js", source)
	require.NoError(t, err)
	defer tree.Close()

	imports := tree.Imports()
	assert.Contains(t, imports, "side-effect-module")
	assert.Contains(t, imports, "polyfill")
	assert.Contains(t, imports, "react")
}

func TestParseTypeScriptFile(t *testing.T) {
	p := NewParser()
	source := []byte(`function greet(name: string): void {
  console.log(name);
}
`)
	tree, err := p.Parse("app.ts", source)
	require.NoError(t, err)
	defer tree.Close()
	assert.NotNil(t, tree)

	funcs := tree.Functions()
	require.Len(t, funcs, 1)
	assert.Equal(t, "greet", funcs[0].Name)
}

func TestParseGoMethodDeclaration(t *testing.T) {
	p := NewParser()
	source := []byte(`package main

type Foo struct{}

func (f *Foo) Bar() {
}

func (f *Foo) Baz(x int) int {
	return x
}
`)
	tree, err := p.Parse("foo.go", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "Bar", funcs[0].Name)
	assert.Equal(t, "Baz", funcs[1].Name)
}

func TestParseSingleImport(t *testing.T) {
	p := NewParser()
	source := []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	tree, err := p.Parse("main.go", source)
	require.NoError(t, err)
	defer tree.Close()

	imports := tree.Imports()
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0])
}

func TestParseRustFunctionsAndImports(t *testing.T) {
	p := NewParser()
	source := []byte(`use std::io;
use std::collections::HashMap;

fn main() {
    println!("Hello");
}

fn add(a: i32, b: i32) -> i32 {
    a + b
}
`)
	tree, err := p.Parse("lib.rs", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "main", funcs[0].Name)
	assert.Equal(t, "add", funcs[1].Name)

	imports := tree.Imports()
	require.Len(t, imports, 2)
	assert.Contains(t, imports, "std::io")
	assert.Contains(t, imports, "std::collections::HashMap")
}

func TestParseCFunctionsAndIncludes(t *testing.T) {
	p := NewParser()
	source := []byte(`#include <stdio.h>
#include "myheader.h"

int main() {
    printf("Hello\n");
    return 0;
}

void greet(const char *name) {
    printf("Hello %s\n", name);
}
`)
	tree, err := p.Parse("main.c", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "main", funcs[0].Name)
	assert.Equal(t, "greet", funcs[1].Name)

	imports := tree.Imports()
	require.Len(t, imports, 2)
	assert.Contains(t, imports, "stdio.h")
	assert.Contains(t, imports, "myheader.h")
}

func TestParseCppFunctionsAndIncludes(t *testing.T) {
	p := NewParser()
	source := []byte(`#include <iostream>

int main() {
    std::cout << "Hello" << std::endl;
    return 0;
}
`)
	tree, err := p.Parse("main.cpp", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 1)
	assert.Equal(t, "main", funcs[0].Name)

	imports := tree.Imports()
	require.Len(t, imports, 1)
	assert.Contains(t, imports, "iostream")
}

func TestParseRubyFunctionsAndRequire(t *testing.T) {
	p := NewParser()
	source := []byte(`require 'json'
require_relative 'helper'

def greet(name)
  puts "Hello, #{name}"
end

def farewell
  puts "Goodbye"
end
`)
	tree, err := p.Parse("script.rb", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "greet", funcs[0].Name)
	assert.Equal(t, "farewell", funcs[1].Name)

	imports := tree.Imports()
	assert.Contains(t, imports, "json")
	assert.Contains(t, imports, "helper")
}

func TestParseJavaFunctions(t *testing.T) {
	p := NewParser()
	source := []byte(`import java.util.List;

public class Main {
    public static void main(String[] args) {
        System.out.println("Hello");
    }

    public int add(int a, int b) {
        return a + b;
    }
}
`)
	tree, err := p.Parse("Main.java", source)
	require.NoError(t, err)
	defer tree.Close()

	funcs := tree.Functions()
	require.Len(t, funcs, 2)
	assert.Equal(t, "main", funcs[0].Name)
	assert.Equal(t, "add", funcs[1].Name)

	imports := tree.Imports()
	require.Len(t, imports, 1)
	assert.Contains(t, imports, "java.util.List")
}

func TestWalkNilNode(t *testing.T) {
	// walk should handle nil nodes gracefully without panic
	var called bool
	walk(nil, func(_ *sitter.Node) {
		called = true
	})
	assert.False(t, called)
}

func TestExtractImportPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"fmt"`, "fmt"},
		{`'os'`, "os"},
		{`  "strings"  `, "strings"},
		{`"net/http";`, "net/http"},
		{``, ""},
	}
	for _, tc := range tests {
		got := extractImportPath(tc.input)
		assert.Equal(t, tc.want, got, "extractImportPath(%q)", tc.input)
	}
}

func TestExtractRustUseEmpty(t *testing.T) {
	result := extractRustUse("use ;")
	assert.Empty(t, result)
}

func TestExtractCIncludeEmpty(t *testing.T) {
	result := extractCInclude("#include")
	assert.Empty(t, result)
}

func TestExtractRubyRequireNonRequire(t *testing.T) {
	result := extractRubyRequire("puts 'hello'")
	assert.Empty(t, result)
}

func TestExtractGenericImportWithFrom(t *testing.T) {
	result := extractGenericImport("import { useState } from 'react'")
	require.Len(t, result, 1)
	assert.Equal(t, "react", result[0])
}

func TestExtractGenericImportWithAlias(t *testing.T) {
	result := extractGenericImport("import os as operating_system")
	require.Len(t, result, 1)
	assert.Equal(t, "os", result[0])
}

func TestExtractPythonFromImportEmpty(t *testing.T) {
	result := extractPythonFromImport("from  import something")
	// "from " trimmed leaves " import something", split on " import " gives ["", "something"]
	// first part is empty after trim, so nil
	assert.Empty(t, result)
}

func TestLanguageDetectionByExtension(t *testing.T) {
	p := NewParser()
	extensions := []struct {
		filename string
		wantErr  bool
	}{
		{"main.go", false},
		{"script.py", false},
		{"app.js", false},
		{"app.ts", false},
		{"Main.java", false},
		{"lib.rs", false},
		{"script.rb", false},
		{"main.c", false},
		{"main.cc", false},
		{"main.cpp", false},
		{"header.h", false},
		{"file.xyz", true},
		{"noext", true},
	}

	for _, tc := range extensions {
		t.Run(tc.filename, func(t *testing.T) {
			tree, err := p.Parse(tc.filename, []byte(""))
			if tree != nil {
				defer tree.Close()
			}
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestQuery(t *testing.T) {
	src := `package main

func hello() {
	println("hello")
}

func add(a, b int) int {
	return a + b
}
`
	p := NewParser()
	tree, err := p.Parse("main.go", []byte(src))
	require.NoError(t, err)
	defer tree.Close()

	matches, err := tree.Query("(function_declaration name: (identifier) @name)")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "hello", matches[0].Text)
	assert.Equal(t, "add", matches[1].Text)
	assert.Greater(t, matches[0].StartLine, 0)
}

func TestQueryInvalidPattern(t *testing.T) {
	src := `package main`
	p := NewParser()
	tree, err := p.Parse("main.go", []byte(src))
	require.NoError(t, err)
	defer tree.Close()

	_, err = tree.Query("(invalid_query_that_wont_compile")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compile query")
}

func TestQueryNoMatches(t *testing.T) {
	src := `package main

var x = 42
`
	p := NewParser()
	tree, err := p.Parse("main.go", []byte(src))
	require.NoError(t, err)
	defer tree.Close()

	matches, err := tree.Query("(function_declaration name: (identifier) @name)")
	require.NoError(t, err)
	assert.Empty(t, matches)
}
