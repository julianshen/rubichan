package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	mcpclient "github.com/julianshen/rubichan/internal/tools/mcp"
)

type MCPBackend struct {
	workDir string
	server  config.MCPServerConfig

	mu     sync.Mutex
	client *mcpclient.Client
	tools  map[string]bool
}

func NewMCPBackend(workDir string, browserCfg config.BrowserConfig, servers []config.MCPServerConfig) (*MCPBackend, error) {
	server, ok := selectBrowserServer(browserCfg, servers)
	if !ok {
		return nil, nil
	}
	return &MCPBackend{
		workDir: workDir,
		server:  server,
	}, nil
}

func selectBrowserServer(browserCfg config.BrowserConfig, servers []config.MCPServerConfig) (config.MCPServerConfig, bool) {
	if browserCfg.MCPServer != "" {
		for _, server := range servers {
			if server.Name == browserCfg.MCPServer {
				return server, true
			}
		}
		return config.MCPServerConfig{}, false
	}
	for _, server := range servers {
		name := strings.ToLower(server.Name)
		if strings.Contains(name, "browser") || strings.Contains(name, "playwright") {
			return server, true
		}
	}
	return config.MCPServerConfig{}, false
}

func (b *MCPBackend) Name() string { return "mcp" }

func (b *MCPBackend) Open(ctx context.Context, handle any, opts OpenOptions) (any, OpenResult, error) {
	if err := b.ensureClient(ctx); err != nil {
		return nil, OpenResult{}, err
	}
	if _, err := b.call(ctx, "browser_navigate", map[string]any{"url": opts.URL}); err != nil {
		return nil, OpenResult{}, err
	}
	snapshot, err := b.Snapshot(ctx, handle)
	if err != nil {
		snapshot = ""
	}
	return struct{}{}, OpenResult{URL: opts.URL, Title: firstSnapshotLine(snapshot, "title: "), Backend: b.Name()}, nil
}

func (b *MCPBackend) Click(ctx context.Context, handle any, selector string, waitForNavigation bool) error {
	_, err := b.call(ctx, "browser_run_code", map[string]any{
		"code": fmt.Sprintf(`async (page) => { const loc = page.locator(%q); if (await loc.count() !== 1) throw new Error("selector match count must be exactly 1"); await loc.click(); return "ok"; }`, selector),
	})
	if err != nil {
		return err
	}
	if waitForNavigation {
		return b.Wait(ctx, handle, WaitOptions{TimeoutMS: 750})
	}
	return nil
}

func (b *MCPBackend) Fill(ctx context.Context, handle any, selector, value string, submit bool) error {
	code := fmt.Sprintf(`async (page) => { const loc = page.locator(%q); if (await loc.count() !== 1) throw new Error("selector match count must be exactly 1"); await loc.fill(%q); %s return "ok"; }`, selector, value, "")
	if submit {
		code = fmt.Sprintf(`async (page) => { const loc = page.locator(%q); if (await loc.count() !== 1) throw new Error("selector match count must be exactly 1"); await loc.fill(%q); await loc.press("Enter"); return "ok"; }`, selector, value)
	}
	_, err := b.call(ctx, "browser_run_code", map[string]any{"code": code})
	return err
}

func (b *MCPBackend) Snapshot(ctx context.Context, handle any) (string, error) {
	if !b.hasTool("browser_snapshot") {
		result, err := b.call(ctx, "browser_run_code", map[string]any{
			"code": `async (page) => JSON.stringify({ title: await page.title(), url: page.url(), text: (await page.locator("body").innerText()).split(/\n+/).slice(0, 40) })`,
		})
		if err != nil {
			return "", err
		}
		return result, nil
	}
	return b.call(ctx, "browser_snapshot", map[string]any{})
}

func (b *MCPBackend) Screenshot(ctx context.Context, handle any, selector string, fullPage bool, path string) (ScreenshotResult, error) {
	if selector == "" && b.hasTool("browser_take_screenshot") {
		_, err := b.call(ctx, "browser_take_screenshot", map[string]any{
			"filename": path,
			"type":     "png",
			"fullPage": fullPage,
		})
		if err != nil {
			return ScreenshotResult{}, err
		}
		return ScreenshotResult{Path: path}, nil
	}
	_, err := b.call(ctx, "browser_run_code", map[string]any{
		"code": fmt.Sprintf(`async (page) => { const loc = page.locator(%q); if (await loc.count() !== 1) throw new Error("selector match count must be exactly 1"); await loc.screenshot({ path: %q }); return %q; }`, selector, path, path),
	})
	if err != nil {
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{Path: path}, nil
}

func (b *MCPBackend) Wait(ctx context.Context, handle any, opts WaitOptions) error {
	if opts.TimeoutMS > 0 && opts.Selector == "" && opts.Text == "" {
		time.Sleep(time.Duration(opts.TimeoutMS) * time.Millisecond)
		return nil
	}
	if opts.Selector != "" {
		_, err := b.call(ctx, "browser_run_code", map[string]any{
			"code": fmt.Sprintf(`async (page) => { await page.locator(%q).waitFor(); return "ok"; }`, opts.Selector),
		})
		return err
	}
	if opts.Text != "" && b.hasTool("browser_wait_for") {
		_, err := b.call(ctx, "browser_wait_for", map[string]any{"text": opts.Text})
		return err
	}
	if opts.Text != "" {
		_, err := b.call(ctx, "browser_run_code", map[string]any{
			"code": fmt.Sprintf(`async (page) => { await page.waitForFunction((text) => document.body && document.body.innerText.includes(text), %q); return "ok"; }`, opts.Text),
		})
		return err
	}
	return nil
}

func (b *MCPBackend) Close(ctx context.Context, handle any) error {
	if !b.hasTool("browser_close") {
		return nil
	}
	_, err := b.call(ctx, "browser_close", map[string]any{})
	return err
}

func (b *MCPBackend) ensureClient(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		return nil
	}

	var transport mcpclient.Transport
	var err error
	switch b.server.Transport {
	case "stdio":
		transport, err = mcpclient.NewStdioTransport(b.server.Command, b.server.Args)
	case "sse":
		initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		transport, err = mcpclient.NewSSETransport(initCtx, b.server.URL)
	default:
		err = fmt.Errorf("unsupported mcp transport %q", b.server.Transport)
	}
	if err != nil {
		return err
	}
	client := mcpclient.NewClient(b.server.Name, transport)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.Initialize(initCtx); err != nil {
		return err
	}
	list, err := client.ListTools(initCtx)
	if err != nil {
		return err
	}
	b.client = client
	b.tools = make(map[string]bool, len(list))
	for _, tool := range list {
		b.tools[tool.Name] = true
	}
	return nil
}

func (b *MCPBackend) hasTool(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tools != nil && b.tools[name]
}

func (b *MCPBackend) call(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := b.ensureClient(ctx); err != nil {
		return "", err
	}
	b.mu.Lock()
	client := b.client
	b.mu.Unlock()
	result, err := client.CallTool(ctx, name, args)
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("mcp browser tool %s returned an error", name)
	}
	var parts []string
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func firstSnapshotLine(snapshot, prefix string) string {
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}
