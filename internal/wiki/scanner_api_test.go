package wiki

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReader returns a readFile func that serves a map of path→content.
func fakeReader(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
}

func TestScanAPIPatterns_GoHTTP(t *testing.T) {
	src := `package main

import "net/http"

func main() {
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/api/users", usersHandler)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {}
func usersHandler(w http.ResponseWriter, r *http.Request)  {}
`
	files := []ScannedFile{{Path: "main.go", Language: "go"}}
	reader := fakeReader(map[string]string{"main.go": src})

	patterns := ScanAPIPatterns(files, reader)

	require.NotEmpty(t, patterns)
	// Should detect at least two HandleFunc registrations
	var httpPatterns []APIPattern
	for _, p := range patterns {
		if p.Kind == "http" {
			httpPatterns = append(httpPatterns, p)
		}
	}
	assert.GreaterOrEqual(t, len(httpPatterns), 2)
	for _, p := range httpPatterns {
		assert.Equal(t, "go", p.Language)
		assert.Equal(t, "main.go", p.File)
		assert.Greater(t, p.Line, 0)
	}
}

func TestScanAPIPatterns_PythonFlask(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.route('/users', methods=['GET'])
def get_users():
    return []

@app.route('/users', methods=['POST'])
def create_user():
    pass
`
	files := []ScannedFile{{Path: "app.py", Language: "python"}}
	reader := fakeReader(map[string]string{"app.py": src})

	patterns := ScanAPIPatterns(files, reader)

	require.NotEmpty(t, patterns)
	var httpPatterns []APIPattern
	for _, p := range patterns {
		if p.Kind == "http" {
			httpPatterns = append(httpPatterns, p)
		}
	}
	assert.GreaterOrEqual(t, len(httpPatterns), 2)
	for _, p := range httpPatterns {
		assert.Equal(t, "python", p.Language)
		assert.Equal(t, "app.py", p.File)
	}
}

func TestScanAPIPatterns_JSExpress(t *testing.T) {
	src := `const express = require('express');
const app = express();

app.get('/products', (req, res) => {
  res.json([]);
});

app.post('/products', (req, res) => {
  res.status(201).json({});
});

app.delete('/products/:id', (req, res) => {
  res.sendStatus(204);
});
`
	files := []ScannedFile{{Path: "server.js", Language: "javascript"}}
	reader := fakeReader(map[string]string{"server.js": src})

	patterns := ScanAPIPatterns(files, reader)

	require.NotEmpty(t, patterns)
	var httpPatterns []APIPattern
	for _, p := range patterns {
		if p.Kind == "http" {
			httpPatterns = append(httpPatterns, p)
		}
	}
	assert.GreaterOrEqual(t, len(httpPatterns), 3)
	for _, p := range httpPatterns {
		assert.Equal(t, "javascript", p.Language)
		assert.Equal(t, "server.js", p.File)
	}
}

func TestScanAPIPatterns_ProtoByExtension(t *testing.T) {
	src := `syntax = "proto3";

service UserService {
  rpc GetUser (GetUserRequest) returns (User);
  rpc ListUsers (ListUsersRequest) returns (ListUsersResponse);
}
`
	files := []ScannedFile{{Path: "api/user.proto", Language: "proto"}}
	reader := fakeReader(map[string]string{"api/user.proto": src})

	patterns := ScanAPIPatterns(files, reader)

	require.NotEmpty(t, patterns)
	for _, p := range patterns {
		assert.Equal(t, "grpc", p.Kind)
		assert.Equal(t, "api/user.proto", p.File)
	}
}

func TestScanAPIPatterns_NoPatterns(t *testing.T) {
	src := `package util

import "fmt"

func Add(a, b int) int {
	fmt.Println(a + b)
	return a + b
}
`
	files := []ScannedFile{{Path: "util/math.go", Language: "go"}}
	reader := fakeReader(map[string]string{"util/math.go": src})

	patterns := ScanAPIPatterns(files, reader)

	assert.Empty(t, patterns)
}

func TestScanAPIPatterns_GoRouteGroup(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	api := r.Group("/todos")
	api.GET("/:id", getHandler)
	api.POST("", createHandler)
	api.PUT("/:id", updateHandler)
	api.DELETE("/:id", deleteHandler)

	v2 := r.Group("/v2/items")
	v2.GET("/list", listItems)
}
`
	files := []ScannedFile{{Path: "main.go", Language: "go"}}
	reader := fakeReader(map[string]string{"main.go": src})

	patterns := ScanAPIPatterns(files, reader)

	var httpPatterns []APIPattern
	for _, p := range patterns {
		if p.Kind == "http" {
			httpPatterns = append(httpPatterns, p)
		}
	}
	require.Len(t, httpPatterns, 5, "expected 5 HTTP routes from two groups")

	// Build a set of method+path for verification.
	routes := make(map[string]bool)
	for _, p := range httpPatterns {
		routes[p.Method+" "+p.Path] = true
	}
	assert.True(t, routes["GET /todos/:id"], "missing GET /todos/:id")
	assert.True(t, routes["POST /todos"], "missing POST /todos")
	assert.True(t, routes["PUT /todos/:id"], "missing PUT /todos/:id")
	assert.True(t, routes["DELETE /todos/:id"], "missing DELETE /todos/:id")
	assert.True(t, routes["GET /v2/items/list"], "missing GET /v2/items/list")
}

func TestScanAPIPatterns_PythonBlueprint(t *testing.T) {
	src := `from flask import Flask, Blueprint

bp = Blueprint('todos', __name__, url_prefix='/todos')

@bp.get('/active')
def get_active():
    pass

@bp.post('/create')
def create_todo():
    pass

@bp.route('/all')
def list_all():
    pass
`
	files := []ScannedFile{{Path: "app.py", Language: "python"}}
	reader := fakeReader(map[string]string{"app.py": src})

	patterns := ScanAPIPatterns(files, reader)

	var httpPatterns []APIPattern
	for _, p := range patterns {
		if p.Kind == "http" {
			httpPatterns = append(httpPatterns, p)
		}
	}
	require.Len(t, httpPatterns, 3, "expected 3 HTTP routes from blueprint")

	paths := make(map[string]bool)
	for _, p := range httpPatterns {
		paths[p.Path] = true
	}
	assert.True(t, paths["/todos/active"], "missing /todos/active")
	assert.True(t, paths["/todos/create"], "missing /todos/create")
	assert.True(t, paths["/todos/all"], "missing /todos/all")
}

func TestResolveRouteGroups_EmptyPath(t *testing.T) {
	src := []byte(`api := r.Group("/todos")
api.GET("", listHandler)
`)
	result := resolveRouteGroups(src)
	assert.Contains(t, string(result), `api.GET("/todos"`)
}

func TestResolveRouteGroups_NoGroups(t *testing.T) {
	src := []byte(`r.GET("/health", healthHandler)
r.POST("/items", createItem)
`)
	result := resolveRouteGroups(src)
	assert.Equal(t, string(src), string(result))
}

func TestScanAPIPatterns_MultipleInOneFile(t *testing.T) {
	src := `package main

import (
	"net/http"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{Use: "app"}
var serveCmd = &cobra.Command{Use: "serve"}

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/health", healthHandler)
	mux.HandleFunc("/api/v1/items", itemsHandler)
	mux.HandleFunc("/api/v1/users", usersHandler)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {}
func itemsHandler(w http.ResponseWriter, r *http.Request)  {}
func usersHandler(w http.ResponseWriter, r *http.Request)  {}
`
	files := []ScannedFile{{Path: "cmd/server/main.go", Language: "go"}}
	reader := fakeReader(map[string]string{"cmd/server/main.go": src})

	patterns := ScanAPIPatterns(files, reader)

	require.NotEmpty(t, patterns)
	assert.GreaterOrEqual(t, len(patterns), 4, "expected ≥3 HTTP + ≥1 CLI pattern")

	kindCounts := make(map[string]int)
	for _, p := range patterns {
		kindCounts[p.Kind]++
	}
	assert.GreaterOrEqual(t, kindCounts["http"], 3)
	assert.GreaterOrEqual(t, kindCounts["cli"], 1)
}
