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

// ErrManagerShutdown is returned when ServerFor is called after Shutdown.
var ErrManagerShutdown = errors.New("manager has been shut down")

// InstallCmd describes one way to install a language server.
type InstallCmd struct {
	Method  string // prerequisite binary ("go", "npm", "pip", "cargo", "gem", "brew", "apt", "rustup", "composer", "")
	Command string // shell command (e.g., "go install golang.org/x/tools/gopls@latest")
	Hint    string // human-readable fallback if command fails
}

// ServerConfig describes how to launch a language server.
type ServerConfig struct {
	Language    string       // language ID (e.g. "go", "typescript")
	Command     string       // server binary name (e.g. "gopls")
	Args        []string     // command-line arguments
	Extensions  []string     // file extensions that map to this language
	InstallCmds []InstallCmd // install methods, tried in order
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
	{Language: "go", Command: "gopls", Args: []string{"serve"}, Extensions: []string{".go"},
		InstallCmds: []InstallCmd{{Method: "go", Command: "go install golang.org/x/tools/gopls@latest"}}},
	{Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
		InstallCmds: []InstallCmd{{Method: "npm", Command: "npm install -g typescript-language-server typescript"}}},
	{Language: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, Extensions: []string{".py"},
		InstallCmds: []InstallCmd{
			{Method: "pip", Command: "pip install pyright"},
			{Method: "npm", Command: "npm install -g pyright"},
		}},
	{Language: "rust", Command: "rust-analyzer", Extensions: []string{".rs"},
		InstallCmds: []InstallCmd{
			{Method: "rustup", Command: "rustup component add rust-analyzer"},
			{Method: "brew", Command: "brew install rust-analyzer"},
		}},
	{Language: "java", Command: "jdtls", Extensions: []string{".java"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install jdtls"}}},
	// "c" covers the C/C++ family; clangd handles both regardless of language ID.
	{Language: "c", Command: "clangd", Extensions: []string{".c", ".h", ".cpp", ".hpp", ".cc", ".cxx"},
		InstallCmds: []InstallCmd{
			{Method: "brew", Command: "brew install llvm"},
			{Method: "apt", Command: "apt install -y clangd", Hint: "May require sudo: sudo apt install clangd"},
		}},
	{Language: "ruby", Command: "solargraph", Args: []string{"stdio"}, Extensions: []string{".rb"},
		InstallCmds: []InstallCmd{{Method: "gem", Command: "gem install solargraph"}}},
	{Language: "php", Command: "phpactor", Args: []string{"language-server"}, Extensions: []string{".php"},
		InstallCmds: []InstallCmd{{Method: "composer", Command: "composer global require phpactor/phpactor"}}},
	{Language: "swift", Command: "sourcekit-lsp", Extensions: []string{".swift"},
		InstallCmds: []InstallCmd{{Hint: "Install Xcode Command Line Tools: xcode-select --install"}}},
	{Language: "kotlin", Command: "kotlin-language-server", Extensions: []string{".kt", ".kts"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install kotlin-language-server"}}},
	{Language: "zig", Command: "zls", Extensions: []string{".zig"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install zls"}}},
	{Language: "lua", Command: "lua-language-server", Extensions: []string{".lua"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install lua-language-server"}}},
	{Language: "elixir", Command: "elixir-ls", Extensions: []string{".ex", ".exs"},
		InstallCmds: []InstallCmd{{Method: "mix", Command: "mix archive.install hex elixir_ls"}}},
	{Language: "haskell", Command: "haskell-language-server-wrapper", Args: []string{"--lsp"}, Extensions: []string{".hs"},
		InstallCmds: []InstallCmd{
			{Method: "ghcup", Command: "ghcup install hls"},
			{Method: "brew", Command: "brew install haskell-language-server"},
		}},
	{Language: "csharp", Command: "OmniSharp", Args: []string{"--languageserver"}, Extensions: []string{".cs"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install omnisharp"}}},
	{Language: "dart", Command: "dart", Args: []string{"language-server"}, Extensions: []string{".dart"},
		InstallCmds: []InstallCmd{{Hint: "Install Dart SDK: https://dart.dev/get-dart"}}},
	{Language: "ocaml", Command: "ocamllsp", Extensions: []string{".ml", ".mli"},
		InstallCmds: []InstallCmd{{Method: "opam", Command: "opam install ocaml-lsp-server"}}},
}
