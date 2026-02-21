package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSASTScannerName(t *testing.T) {
	s := NewSASTScanner()
	assert.Equal(t, "sast", s.Name())
}

func TestSASTScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewSASTScanner()
}

func TestSASTDetectsSQLInjectionGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import "database/sql"

func GetUser(db *sql.DB, name string) {
	db.Query("SELECT * FROM users WHERE name = '" + name + "'")
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-89" {
			found = true
			assert.Equal(t, "sast", f.Scanner)
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryInjection, f.Category)
			assert.Equal(t, "GetUser", f.Location.Function)
			assert.Equal(t, "handler.go", f.Location.File)
		}
	}
	assert.True(t, found, "expected a SQL injection finding")
}

func TestSASTDetectsCommandInjectionGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "run.go", `package run

import "os/exec"

func RunCommand(input string) {
	exec.Command("sh", "-c", input)
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-78" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryInjection, f.Category)
			assert.Equal(t, "RunCommand", f.Location.Function)
		}
	}
	assert.True(t, found, "expected a command injection finding")
}

func TestSASTDetectsWeakCryptoGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hash.go", `package hash

import (
	"crypto/md5"
	"fmt"
)

func Hash(data []byte) string {
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum)
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-327" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
			assert.Equal(t, security.CategoryCryptography, f.Category)
			assert.Contains(t, f.Title, "weak cryptographic")
		}
	}
	assert.True(t, found, "expected a weak crypto finding")
}

func TestSASTDetectsPythonSQLInjection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "db.py", `import sqlite3

def get_user(name):
    conn = sqlite3.connect("test.db")
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM users WHERE name = '" + name + "'")
    return cursor.fetchall()
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-89" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryInjection, f.Category)
			assert.Equal(t, "get_user", f.Location.Function)
		}
	}
	assert.True(t, found, "expected a Python SQL injection finding")
}

func TestSASTDetectsJSXSS(t *testing.T) {
	dir := t.TempDir()
	// Test detects unsafe DOM manipulation patterns in JavaScript
	writeFile(t, dir, "app.js", "function renderContent(userInput) {\n"+
		"    const div = document.getElementById(\"output\");\n"+
		"    div.innerHTML = userInput;\n"+
		"}\n")

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-79" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryInjection, f.Category)
			assert.Equal(t, "renderContent", f.Location.Function)
		}
	}
	assert.True(t, found, "expected a JS XSS finding")
}

func TestSASTSkipsUnsupportedLanguage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.rb", `
def get_user(name)
  db.query("SELECT * FROM users WHERE name = '#{name}'")
end
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "Ruby is not in the supported SAST languages")
}

func TestSASTEmptyDir(t *testing.T) {
	dir := t.TempDir()

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSASTContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main
func main() {}
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewSASTScanner()
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestSASTDetectsGoPathTraversal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "files.go", `package files

import "os"

func ReadUserFile(path string) {
	os.Open(path)
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-22" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
			assert.Equal(t, security.CategoryInputValidation, f.Category)
			assert.Equal(t, "ReadUserFile", f.Location.Function)
		}
	}
	assert.True(t, found, "expected a path traversal finding")
}

func TestSASTDetectsPythonCommandInjection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "run.py", `import os
import subprocess

def run_command(cmd):
    os.system(cmd)

def run_shell(cmd):
    subprocess.call(cmd, shell=True)
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-78" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryInjection, f.Category)
		}
	}
	assert.True(t, found, "expected a Python command injection finding")
}

func TestSASTDetectsTSXXSS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "component.tsx", `function renderContent(userInput: string) {
    const div = document.getElementById("output");
    div.innerHTML = userInput;
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-79" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a TSX XSS finding")
}

func TestSASTDetectsJSXXSS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "component.jsx", `function renderContent(userInput) {
    const div = document.getElementById("output");
    div.innerHTML = userInput;
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-79" {
			found = true
		}
	}
	assert.True(t, found, "expected a JSX XSS finding")
}

func TestSASTDetectsJSSQLInjection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "db.js", `function getUser(name) {
    const result = db.query("SELECT * FROM users WHERE name = '" + name + "'");
    return result;
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-89" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a JS SQL injection finding")
}

func TestSASTDetectsTSSQLInjection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "db.ts", `function getUser(name: string) {
    const result = db.query("SELECT * FROM users WHERE name = '" + name + "'");
    return result;
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.CWE == "CWE-89" {
			found = true
		}
	}
	assert.True(t, found, "expected a TS SQL injection finding")
}

func TestSASTScannerUnreadableFile(t *testing.T) {
	dir := t.TempDir()

	s := NewSASTScanner()
	// Point to a non-existent Go file via Files.
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"nonexistent.go"},
	})
	require.NoError(t, err)
	assert.Empty(t, findings, "unreadable file should be skipped")
}

func TestSASTScannerCleanGoFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clean.go", `package clean

import "fmt"

func Hello() {
	fmt.Println("Hello, World!")
}
`)

	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "clean file should produce no findings")
}
