package shell

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// PackageManager represents a system package manager.
type PackageManager struct {
	Name       string // "brew", "apt", "dnf", "pacman", "apk", etc.
	InstallCmd string // "brew install", "sudo apt install -y", etc.
}

// PackageInstaller detects missing commands and offers to install them.
type PackageInstaller struct {
	pkgManager *PackageManager
	agentTurn  AgentTurnFunc
	shellExec  ShellExecFunc
	approvalFn func(ctx context.Context, action string) (bool, error)
	cache      map[string]string // command → package name
	pkgSearch  func(ctx context.Context, cmd string, pkgMgrName string) (string, error)
}

// NewPackageInstaller creates a package installer.
func NewPackageInstaller(
	pkgManager *PackageManager,
	agentTurn AgentTurnFunc,
	shellExec ShellExecFunc,
	approvalFn func(ctx context.Context, action string) (bool, error),
) *PackageInstaller {
	return &PackageInstaller{
		pkgManager: pkgManager,
		agentTurn:  agentTurn,
		shellExec:  shellExec,
		approvalFn: approvalFn,
		cache:      make(map[string]string),
	}
}

// HandleCommandNotFound checks if a command failure is due to a missing tool
// and offers to install it. Returns true if it handled the error.
func (pi *PackageInstaller) HandleCommandNotFound(
	ctx context.Context,
	fullCommand string,
	stderr string,
	exitCode int,
	stdout io.Writer,
	errWriter io.Writer,
) (bool, error) {
	if !IsCommandNotFound(stderr, exitCode) {
		return false, nil
	}

	if pi.pkgManager == nil {
		return false, nil
	}

	// Extract the command name (first word)
	cmdName := extractCommandName(fullCommand)
	if cmdName == "" {
		return false, nil
	}

	// Resolve package name
	pkg := pi.resolvePackage(ctx, cmdName)
	if pkg == "" {
		fmt.Fprintf(errWriter, "Could not determine package for %q\n", cmdName)
		return true, nil
	}

	installCmd := pi.pkgManager.InstallCmd + " " + pkg
	fmt.Fprintf(errWriter, "\n📦 %s is not installed. Install with: %s\n", cmdName, installCmd)

	// Ask for approval
	if pi.approvalFn == nil {
		return true, nil
	}

	approved, err := pi.approvalFn(ctx, installCmd)
	if err != nil {
		return true, err
	}
	if !approved {
		return true, nil
	}

	// Install
	if pi.shellExec == nil {
		return true, nil
	}

	fmt.Fprintf(errWriter, "[Installing: %s]\n", installCmd)
	_, installErr, installExit, err := pi.shellExec(ctx, installCmd, "")
	if err != nil {
		return true, fmt.Errorf("install failed: %w", err)
	}
	if installExit != 0 {
		fmt.Fprintf(errWriter, "Installation failed: %s\n", installErr)
		return true, nil
	}

	fmt.Fprintf(errWriter, "✓ %s installed successfully.\n", cmdName)

	// Re-run original command
	fmt.Fprintf(errWriter, "[Re-running: %s]\n", fullCommand)
	rerunStdout, rerunStderr, _, err := pi.shellExec(ctx, fullCommand, "")
	if err != nil {
		return true, fmt.Errorf("re-run failed: %w", err)
	}
	if rerunStdout != "" {
		fmt.Fprint(stdout, rerunStdout)
		if !strings.HasSuffix(rerunStdout, "\n") {
			fmt.Fprintln(stdout)
		}
	}
	if rerunStderr != "" {
		fmt.Fprint(errWriter, rerunStderr)
	}

	return true, nil
}

// resolvePackage resolves a command name to a package name using three-tier resolution.
func (pi *PackageInstaller) resolvePackage(ctx context.Context, cmdName string) string {
	// Check cache first
	if pkg, ok := pi.cache[cmdName]; ok {
		return pkg
	}

	pkgMgrName := ""
	if pi.pkgManager != nil {
		pkgMgrName = pi.pkgManager.Name
	}

	// Tier 1: Built-in lookup table
	if pkg := LookupPackage(cmdName, pkgMgrName); pkg != "" {
		pi.cache[cmdName] = pkg
		return pkg
	}

	// Tier 2: Package manager search
	if pi.pkgSearch != nil {
		if pkg, err := pi.pkgSearch(ctx, cmdName, pkgMgrName); err == nil && pkg != "" {
			pi.cache[cmdName] = pkg
			return pkg
		}
	}

	// Tier 3: LLM fallback
	if pi.agentTurn != nil {
		if pkg := pi.resolveLLM(ctx, cmdName, pkgMgrName); pkg != "" {
			pi.cache[cmdName] = pkg
			return pkg
		}
	}

	return ""
}

func (pi *PackageInstaller) resolveLLM(ctx context.Context, cmdName string, pkgMgrName string) string {
	prompt := fmt.Sprintf(
		"The command %q is not found. What is the package name to install it using %s? "+
			"Reply with ONLY the package name, nothing else.",
		cmdName, pkgMgrName,
	)

	events, err := pi.agentTurn(ctx, prompt)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(collectTurnText(events))
}

// IsCommandNotFound detects if a command failure is due to a missing executable.
func IsCommandNotFound(stderr string, exitCode int) bool {
	if exitCode == 127 {
		return true
	}
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "command not found") || strings.Contains(lower, "not found:")
}

// extractCommandName gets the command name from a full command string.
func extractCommandName(fullCommand string) string {
	fields := strings.Fields(fullCommand)
	for _, f := range fields {
		// Skip env var assignments
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
			continue
		}
		return f
	}
	return ""
}

// knownPackages maps command names to package names per package manager.
// Format: command → { pkgManager → packageName }
// If a command has the same package name across all managers, use "*" as key.
var knownPackages = map[string]map[string]string{
	"jq":      {"*": "jq"},
	"rg":      {"*": "ripgrep"},
	"fd":      {"brew": "fd", "apt": "fd-find", "dnf": "fd-find", "*": "fd"},
	"bat":     {"apt": "batcat", "*": "bat"},
	"htop":    {"*": "htop"},
	"tree":    {"*": "tree"},
	"wget":    {"*": "wget"},
	"curl":    {"*": "curl"},
	"tmux":    {"*": "tmux"},
	"make":    {"apt": "build-essential", "*": "make"},
	"gcc":     {"apt": "build-essential", "*": "gcc"},
	"g++":     {"apt": "build-essential", "*": "gcc"},
	"python3": {"brew": "python3", "apt": "python3", "*": "python3"},
	"pip":     {"apt": "python3-pip", "*": "pip"},
	"pip3":    {"apt": "python3-pip", "*": "pip3"},
	"node":    {"brew": "node", "apt": "nodejs", "*": "nodejs"},
	"npm":     {"brew": "node", "apt": "npm", "*": "npm"},
	"fzf":     {"*": "fzf"},
	"ag":      {"apt": "silversearcher-ag", "brew": "the_silver_searcher", "*": "the_silver_searcher"},
	"delta":   {"brew": "git-delta", "apt": "git-delta", "*": "git-delta"},
	"exa":     {"*": "exa"},
	"eza":     {"*": "eza"},
	"dust":    {"*": "dust"},
	"duf":     {"*": "duf"},
	"procs":   {"*": "procs"},
	"sd":      {"*": "sd"},
	"hyperfine": {"*": "hyperfine"},
	"tokei":   {"*": "tokei"},
	"xh":     {"*": "xh"},
	"zoxide":  {"*": "zoxide"},
	"shellcheck": {"*": "shellcheck"},
	"shfmt":   {"brew": "shfmt", "apt": "shfmt", "*": "shfmt"},
	"yq":      {"*": "yq"},
	"ffmpeg":  {"*": "ffmpeg"},
	"imagemagick": {"brew": "imagemagick", "apt": "imagemagick", "*": "imagemagick"},
	"convert": {"brew": "imagemagick", "apt": "imagemagick", "*": "imagemagick"},
	"nmap":    {"*": "nmap"},
	"nc":      {"apt": "netcat-openbsd", "*": "netcat"},
	"telnet":  {"apt": "telnet", "*": "telnet"},
	"dig":     {"apt": "dnsutils", "brew": "bind", "*": "bind-utils"},
	"nslookup": {"apt": "dnsutils", "brew": "bind", "*": "bind-utils"},
	"watch":   {"brew": "watch", "*": "procps"},
	"pv":      {"*": "pv"},
	"socat":   {"*": "socat"},
	"strace":  {"*": "strace"},
	"lsof":    {"*": "lsof"},
	"rsync":   {"*": "rsync"},
	"zip":     {"*": "zip"},
	"unzip":   {"*": "unzip"},
	"pigz":    {"*": "pigz"},
	"xclip":   {"*": "xclip"},
	"xsel":    {"*": "xsel"},
	"go":      {"brew": "go", "apt": "golang", "*": "golang"},
	"rustc":   {"brew": "rust", "apt": "rustc", "*": "rust"},
	"cargo":   {"brew": "rust", "apt": "cargo", "*": "rust"},
	"cmake":   {"*": "cmake"},
	"clang":   {"apt": "clang", "brew": "llvm", "*": "clang"},
}

// LookupPackage looks up a command in the built-in table for the given package manager.
func LookupPackage(cmdName string, pkgManager string) string {
	entry, ok := knownPackages[cmdName]
	if !ok {
		return ""
	}

	// Try specific package manager first
	if pkg, ok := entry[pkgManager]; ok {
		return pkg
	}
	// Fall back to wildcard
	if pkg, ok := entry["*"]; ok {
		return pkg
	}
	return ""
}

// DetectPackageManager identifies the available package manager on the system.
func DetectPackageManager() *PackageManager {
	return detectPackageManagerWith(func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
}

// detectPackageManagerWith uses a custom lookup function (for testability).
func detectPackageManagerWith(lookup func(string) bool) *PackageManager {
	managers := []PackageManager{
		{Name: "brew", InstallCmd: "brew install"},
		{Name: "apt", InstallCmd: "sudo apt install -y"},
		{Name: "dnf", InstallCmd: "sudo dnf install -y"},
		{Name: "yum", InstallCmd: "sudo yum install -y"},
		{Name: "pacman", InstallCmd: "sudo pacman -S --noconfirm"},
		{Name: "apk", InstallCmd: "sudo apk add"},
		{Name: "zypper", InstallCmd: "sudo zypper install -y"},
		{Name: "nix-env", InstallCmd: "nix-env -iA nixpkgs."},
	}

	for _, m := range managers {
		if lookup(m.Name) {
			return &PackageManager{
				Name:       m.Name,
				InstallCmd: m.InstallCmd,
			}
		}
	}
	return nil
}
