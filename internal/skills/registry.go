package skills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/store"
)

// RegistrySearchResult represents a single result from the registry search endpoint.
type RegistrySearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// RegistryClient communicates with a remote skill registry API and
// caches manifest lookups in a local SQLite store.
type RegistryClient struct {
	baseURL    string
	store      *store.Store
	cacheTTL   time.Duration
	httpClient *http.Client
}

// NewRegistryClient creates a RegistryClient pointing at the given registry
// base URL. The store and cacheTTL are used for caching manifest lookups;
// pass nil for store to disable caching.
func NewRegistryClient(baseURL string, s *store.Store, cacheTTL time.Duration) *RegistryClient {
	return &RegistryClient{
		baseURL:    baseURL,
		store:      s,
		cacheTTL:   cacheTTL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Search queries the registry search endpoint and returns matching skills.
func (c *RegistryClient) Search(ctx context.Context, query string) ([]RegistrySearchResult, error) {
	url := fmt.Sprintf("%s/api/v1/search?%s", c.baseURL, url.Values{"q": {query}}.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search: unexpected status %d", resp.StatusCode)
	}

	// Limit response body to 1 MB to prevent OOM from malicious registries.
	const maxSearchResponseBytes = 1 << 20
	var results []RegistrySearchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSearchResponseBytes)).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	return results, nil
}

// GetManifest fetches the YAML manifest for the given skill name and version.
// If a store is configured, it checks the cache first and only hits the
// network when there is no valid (non-expired) cache entry.
func (c *RegistryClient) GetManifest(ctx context.Context, name, version string) (*SkillManifest, error) {
	// Check cache first.
	if c.store != nil {
		entry, err := c.store.GetCachedRegistry(name)
		if err != nil {
			return nil, fmt.Errorf("check cache: %w", err)
		}
		if entry != nil && time.Since(entry.CachedAt) < c.cacheTTL {
			// Cache hit and not expired -- fetch manifest from server still
			// needed for full manifest data, but we can use the cached metadata
			// to avoid the request. For now, we need to re-parse the manifest
			// from the cached description which stores the raw YAML.
			m, err := ParseManifest([]byte(entry.Description))
			if err == nil {
				return m, nil
			}
			// If cache parse fails, fall through to fetch from server.
		}
	}

	// Fetch from server.
	url := fmt.Sprintf("%s/api/v1/skills/%s/%s/manifest", c.baseURL, url.PathEscape(name), url.PathEscape(version))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get manifest: unexpected status %d", resp.StatusCode)
	}

	// Limit manifest response body to 1 MB.
	const maxManifestBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if err != nil {
		return nil, fmt.Errorf("read manifest body: %w", err)
	}

	manifest, err := ParseManifest(body)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Cache the raw YAML in the store's description field so we can
	// reconstruct the manifest on cache hit without a network request.
	if c.store != nil {
		_ = c.store.CacheRegistryEntry(store.RegistryEntry{
			Name:        name,
			Version:     version,
			Description: string(body),
		})
	}

	return manifest, nil
}

// ListVersions returns all available versions for a skill from the registry.
func (c *RegistryClient) ListVersions(ctx context.Context, name string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/skills/%s/versions", c.baseURL, url.PathEscape(name))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create versions request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("versions request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list versions: unexpected status %d", resp.StatusCode)
	}

	const maxVersionsBytes = 1 << 20
	var versions []string
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxVersionsBytes)).Decode(&versions); err != nil {
		return nil, fmt.Errorf("decode versions response: %w", err)
	}

	return versions, nil
}

// Download fetches a skill tarball from the registry and extracts it to dest.
func (c *RegistryClient) Download(ctx context.Context, name, version, dest string) error {
	url := fmt.Sprintf("%s/api/v1/skills/%s/%s/download", c.baseURL, url.PathEscape(name), url.PathEscape(version))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %d", resp.StatusCode)
	}

	// Limit download body to 100 MB.
	const maxDownloadBytes = 100 << 20
	return extractTarGz(io.LimitReader(resp.Body, maxDownloadBytes), dest)
}

// Tar extraction safety limits.
const (
	maxTarEntries   = 10000     // Maximum number of entries in a tar archive.
	maxTarTotalSize = 500 << 20 // Maximum total extracted size (500 MB).
	maxTarFileSize  = 50 << 20  // Maximum size of a single extracted file (50 MB).
	safeModeMask    = os.FileMode(0o7777) &^ (os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
)

// extractTarGz reads a gzip-compressed tar archive from r and extracts its
// contents to the dest directory. It enforces limits on entry count, total
// extracted size, and individual file size. Setuid/setgid bits are stripped.
func extractTarGz(r io.Reader, dest string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var entryCount int
	var totalSize int64

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		entryCount++
		if entryCount > maxTarEntries {
			return fmt.Errorf("tar archive exceeds maximum entry count (%d)", maxTarEntries)
		}

		target := filepath.Join(dest, filepath.Clean(header.Name))

		// Ensure the target does not escape the destination directory.
		cleanDest := filepath.Clean(dest) + string(filepath.Separator)
		if !strings.HasPrefix(target, cleanDest) && target != filepath.Clean(dest) {
			return fmt.Errorf("tar entry %q attempts to escape destination", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %q: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent directory for %q: %w", target, err)
			}

			// Strip setuid, setgid, and sticky bits from file mode.
			mode := os.FileMode(header.Mode) & safeModeMask
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create file %q: %w", target, err)
			}
			// Use LimitReader to cap reads and track actual bytes written
			// (not header.Size which can be spoofed).
			written, copyErr := io.Copy(f, io.LimitReader(tr, maxTarFileSize+1))
			f.Close()
			if copyErr != nil {
				return fmt.Errorf("write file %q: %w", target, copyErr)
			}
			if written > maxTarFileSize {
				return fmt.Errorf("tar entry %q exceeds maximum file size (%d bytes)", header.Name, maxTarFileSize)
			}
			totalSize += written
			if totalSize > maxTarTotalSize {
				return fmt.Errorf("tar archive exceeds maximum total extracted size (%d bytes)", maxTarTotalSize)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("tar entry %q: symlinks and hard links are not allowed", header.Name)
		}
	}

	return nil
}

// allowedGitSchemes lists URL schemes permitted for git clone operations.
var allowedGitSchemes = map[string]bool{
	"https": true,
	"ssh":   true,
}

// validateGitURL checks that the URL uses an allowed scheme. This prevents
// file://, ftp://, and other potentially dangerous URL schemes.
// Local paths (no scheme) and SSH shorthand (git@host:path) are allowed.
func validateGitURL(rawURL string) error {
	// Handle SSH shorthand (git@host:path) — always allowed.
	if strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		return nil
	}

	// No scheme present — treat as a local path (used in development/testing).
	if !strings.Contains(rawURL, "://") {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid git URL %q: %w", rawURL, err)
	}

	if !allowedGitSchemes[parsed.Scheme] {
		return fmt.Errorf("git URL scheme %q not allowed (allowed: https, ssh)", parsed.Scheme)
	}

	return nil
}

// InstallFromGit clones a git repository and validates that SKILL.yaml exists.
// Only https:// and ssh:// (including git@host:path shorthand) URLs are allowed.
func (c *RegistryClient) InstallFromGit(ctx context.Context, gitURL, dest string) error {
	if err := validateGitURL(gitURL); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--", gitURL, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %s: %w", string(out), err)
	}

	skillPath := filepath.Join(dest, "SKILL.yaml")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return fmt.Errorf("cloned repository does not contain SKILL.yaml")
	} else if err != nil {
		return fmt.Errorf("check SKILL.yaml: %w", err)
	}

	return nil
}
