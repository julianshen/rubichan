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
	url := fmt.Sprintf("%s/api/v1/skills/%s/%s/manifest", c.baseURL, name, version)

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

// Download fetches a skill tarball from the registry and extracts it to dest.
func (c *RegistryClient) Download(ctx context.Context, name, version, dest string) error {
	url := fmt.Sprintf("%s/api/v1/skills/%s/%s/download", c.baseURL, name, version)

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

// extractTarGz reads a gzip-compressed tar archive from r and extracts its
// contents to the dest directory.
func extractTarGz(r io.Reader, dest string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
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
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file %q: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %q: %w", target, err)
			}
			f.Close()
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("tar entry %q: symlinks and hard links are not allowed", header.Name)
		}
	}

	return nil
}

// InstallFromGit clones a git repository and validates that SKILL.yaml exists.
func (c *RegistryClient) InstallFromGit(ctx context.Context, url, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", url, dest)
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
