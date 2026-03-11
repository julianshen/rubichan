package lsp

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrServerNotInstalled is returned when a language server binary is not found on PATH.
var ErrServerNotInstalled = errors.New("language server not installed")

// ErrNoConfig is returned when no server config exists for a language.
var ErrNoConfig = errors.New("no language server configured for this language")

// ServerConfig describes how to launch a language server.
type ServerConfig struct {
	Language   string   // language ID (e.g. "go", "typescript")
	Command    string   // server binary name (e.g. "gopls")
	Args       []string // command-line arguments
	Extensions []string // file extensions that map to this language
}

// Registry maps file extensions to language server configurations.
// It combines built-in defaults with user-provided overrides.
type Registry struct {
	mu         sync.RWMutex
	byLanguage map[string]ServerConfig
	byExt      map[string]string // extension -> language ID
	lookPath   func(string) (string, error)
}

// NewRegistry creates a registry populated with built-in defaults.
func NewRegistry() *Registry {
	r := &Registry{
		byLanguage: make(map[string]ServerConfig),
		byExt:      make(map[string]string),
		lookPath:   exec.LookPath,
	}
	for _, cfg := range defaultConfigs {
		r.Register(cfg)
	}
	return r
}

// Register adds or overrides a server config. Later registrations for the
// same language replace earlier ones.
func (r *Registry) Register(cfg ServerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byLanguage[cfg.Language] = cfg
	for _, ext := range cfg.Extensions {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		r.byExt[ext] = cfg.Language
	}
}

// ConfigFor returns the server config for a language ID.
func (r *Registry) ConfigFor(languageID string) (ServerConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.byLanguage[languageID]
	if !ok {
		return ServerConfig{}, ErrNoConfig
	}
	return cfg, nil
}

// LanguageForExt maps a file extension (e.g. ".go") to a language ID.
func (r *Registry) LanguageForExt(ext string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	lang, ok := r.byExt[ext]
	return lang, ok
}

// LanguageForFile detects the language from a file path.
func (r *Registry) LanguageForFile(filePath string) (string, bool) {
	ext := filepath.Ext(filePath)
	if ext == "" {
		return "", false
	}
	return r.LanguageForExt(ext)
}

// IsInstalled checks whether the server binary for a language is on PATH.
func (r *Registry) IsInstalled(languageID string) bool {
	cfg, err := r.ConfigFor(languageID)
	if err != nil {
		return false
	}
	_, err = r.lookPath(cfg.Command)
	return err == nil
}

// Available returns language IDs whose server binary is on PATH.
func (r *Registry) Available() []string {
	r.mu.RLock()
	configs := make(map[string]ServerConfig, len(r.byLanguage))
	for k, v := range r.byLanguage {
		configs[k] = v
	}
	r.mu.RUnlock()

	var available []string
	for lang, cfg := range configs {
		if _, err := r.lookPath(cfg.Command); err == nil {
			available = append(available, lang)
		}
	}
	return available
}

// Languages returns all configured language IDs.
func (r *Registry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	langs := make([]string, 0, len(r.byLanguage))
	for lang := range r.byLanguage {
		langs = append(langs, lang)
	}
	return langs
}

// defaultConfigs are the built-in server configurations.
var defaultConfigs = []ServerConfig{
	{Language: "go", Command: "gopls", Args: []string{"serve"}, Extensions: []string{".go"}},
	{Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx"}},
	{Language: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, Extensions: []string{".py"}},
	{Language: "rust", Command: "rust-analyzer", Extensions: []string{".rs"}},
	{Language: "java", Command: "jdtls", Extensions: []string{".java"}},
	{Language: "c", Command: "clangd", Extensions: []string{".c", ".h", ".cpp", ".hpp", ".cc", ".cxx"}},
	{Language: "ruby", Command: "solargraph", Args: []string{"stdio"}, Extensions: []string{".rb"}},
	{Language: "php", Command: "phpactor", Args: []string{"language-server"}, Extensions: []string{".php"}},
	{Language: "swift", Command: "sourcekit-lsp", Extensions: []string{".swift"}},
	{Language: "kotlin", Command: "kotlin-language-server", Extensions: []string{".kt", ".kts"}},
	{Language: "zig", Command: "zls", Extensions: []string{".zig"}},
	{Language: "lua", Command: "lua-language-server", Extensions: []string{".lua"}},
	{Language: "elixir", Command: "elixir-ls", Extensions: []string{".ex", ".exs"}},
	{Language: "haskell", Command: "haskell-language-server-wrapper", Args: []string{"--lsp"}, Extensions: []string{".hs"}},
	{Language: "csharp", Command: "OmniSharp", Args: []string{"--languageserver"}, Extensions: []string{".cs"}},
	{Language: "dart", Command: "dart", Args: []string{"language-server"}, Extensions: []string{".dart"}},
	{Language: "ocaml", Command: "ocamllsp", Extensions: []string{".ml", ".mli"}},
}
