package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type nativeSession struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
	headless    bool
}

type NativeBackend struct{}

func NewNativeBackend() *NativeBackend {
	return &NativeBackend{}
}

func (b *NativeBackend) Name() string { return "native" }

func (b *NativeBackend) Open(ctx context.Context, handle any, opts OpenOptions) (any, OpenResult, error) {
	sess, ok := handle.(*nativeSession)
	if !ok || sess == nil {
		var err error
		sess, err = newNativeSession(opts)
		if err != nil {
			return nil, OpenResult{}, err
		}
	} else if sess.headless != opts.Headless {
		sess.close()
		var err error
		sess, err = newNativeSession(opts)
		if err != nil {
			return nil, OpenResult{}, err
		}
	}
	if err := chromedp.Run(sess.ctx, viewportAction(opts), chromedp.Navigate(opts.URL)); err != nil {
		return nil, OpenResult{}, err
	}
	var title, current string
	if err := chromedp.Run(sess.ctx, chromedp.Title(&title), chromedp.Location(&current)); err != nil {
		return nil, OpenResult{}, err
	}
	return sess, OpenResult{URL: current, Title: title, Backend: b.Name()}, nil
}

func (b *NativeBackend) Click(ctx context.Context, handle any, selector string, waitForNavigation bool) error {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return err
	}
	if err := ensureUniqueSelector(sess.ctx, selector); err != nil {
		return err
	}
	if err := chromedp.Run(sess.ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
		return err
	}
	if waitForNavigation {
		return chromedp.Run(sess.ctx, chromedp.Sleep(750*time.Millisecond))
	}
	return nil
}

func (b *NativeBackend) Fill(ctx context.Context, handle any, selector, value string, submit bool) error {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return err
	}
	if err := ensureUniqueSelector(sess.ctx, selector); err != nil {
		return err
	}
	actions := []chromedp.Action{
		chromedp.SetValue(selector, "", chromedp.ByQuery),
		chromedp.SendKeys(selector, value, chromedp.ByQuery),
	}
	if submit {
		actions = append(actions, chromedp.SendKeys(selector, "\n", chromedp.ByQuery))
	}
	return chromedp.Run(sess.ctx, actions...)
}

func (b *NativeBackend) Snapshot(ctx context.Context, handle any) (string, error) {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return "", err
	}
	const script = `(() => {
		const text = (document.body?.innerText || "").trim().split(/\n+/).map(x => x.trim()).filter(Boolean).slice(0, 40);
		const links = Array.from(document.querySelectorAll("a")).map(a => a.innerText?.trim()).filter(Boolean).slice(0, 10);
		const buttons = Array.from(document.querySelectorAll("button,input[type=button],input[type=submit]")).map(el => (el.innerText || el.value || "").trim()).filter(Boolean).slice(0, 10);
		const fields = Array.from(document.querySelectorAll("input,textarea,select")).map(el => el.name || el.id || el.getAttribute("placeholder") || el.tagName.toLowerCase()).filter(Boolean).slice(0, 10);
		return JSON.stringify({
			title: document.title || "",
			url: location.href,
			text,
			links,
			buttons,
			fields
		});
	})()`
	var payload string
	if err := chromedp.Run(sess.ctx, chromedp.Evaluate(script, &payload)); err != nil {
		return "", err
	}
	var out struct {
		Title   string   `json:"title"`
		URL     string   `json:"url"`
		Text    []string `json:"text"`
		Links   []string `json:"links"`
		Buttons []string `json:"buttons"`
		Fields  []string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return payload, nil
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("title: %s", out.Title), fmt.Sprintf("url: %s", out.URL))
	if len(out.Text) > 0 {
		lines = append(lines, "text:")
		for _, line := range out.Text {
			lines = append(lines, "- "+line)
		}
	}
	if len(out.Links) > 0 {
		lines = append(lines, "links: "+strings.Join(out.Links, ", "))
	}
	if len(out.Buttons) > 0 {
		lines = append(lines, "buttons: "+strings.Join(out.Buttons, ", "))
	}
	if len(out.Fields) > 0 {
		lines = append(lines, "fields: "+strings.Join(out.Fields, ", "))
	}
	return strings.Join(lines, "\n"), nil
}

func (b *NativeBackend) Screenshot(ctx context.Context, handle any, selector string, fullPage bool, path string) (ScreenshotResult, error) {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return ScreenshotResult{}, err
	}
	var buf []byte
	if selector != "" {
		if err := ensureUniqueSelector(sess.ctx, selector); err != nil {
			return ScreenshotResult{}, err
		}
		if err := chromedp.Run(sess.ctx, chromedp.Screenshot(selector, &buf, chromedp.ByQuery)); err != nil {
			return ScreenshotResult{}, err
		}
	} else if fullPage {
		if err := chromedp.Run(sess.ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
			return ScreenshotResult{}, err
		}
	} else {
		if err := chromedp.Run(sess.ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return ScreenshotResult{}, err
		}
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{Path: path}, nil
}

func (b *NativeBackend) Wait(ctx context.Context, handle any, opts WaitOptions) error {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return err
	}
	timeout := 30 * time.Second
	if opts.TimeoutMS > 0 {
		timeout = time.Duration(opts.TimeoutMS) * time.Millisecond
	}
	waitCtx, cancel := context.WithTimeout(sess.ctx, timeout)
	defer cancel()

	switch {
	case opts.Selector != "":
		return chromedp.Run(waitCtx, chromedp.WaitVisible(opts.Selector, chromedp.ByQuery))
	case opts.Text != "":
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			var bodyText string
			if err := chromedp.Run(waitCtx, chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &bodyText)); err == nil && strings.Contains(bodyText, opts.Text) {
				return nil
			}
			select {
			case <-waitCtx.Done():
				return waitCtx.Err()
			case <-ticker.C:
			}
		}
	default:
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-timer.C:
			return nil
		}
	}
}

func (b *NativeBackend) Close(ctx context.Context, handle any) error {
	sess, err := requireNativeSession(handle)
	if err != nil {
		return err
	}
	sess.close()
	return nil
}

func newNativeSession(opts OpenOptions) (*nativeSession, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", opts.Headless),
		chromedp.Flag("disable-gpu", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	tabCtx, cancel := chromedp.NewContext(allocCtx)
	return &nativeSession{
		ctx:         tabCtx,
		cancel:      cancel,
		allocCancel: allocCancel,
		headless:    opts.Headless,
	}, nil
}

func (s *nativeSession) close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
}

func viewportAction(opts OpenOptions) chromedp.Action {
	width := opts.Viewport.Width
	height := opts.Viewport.Height
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}
	return chromedp.EmulateViewport(int64(width), int64(height))
}

func requireNativeSession(handle any) (*nativeSession, error) {
	sess, ok := handle.(*nativeSession)
	if !ok || sess == nil {
		return nil, fmt.Errorf("invalid native browser session")
	}
	return sess, nil
}

func ensureUniqueSelector(ctx context.Context, selector string) error {
	var count int64
	script := fmt.Sprintf(`document.querySelectorAll(%q).length`, selector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &count)); err != nil {
		return err
	}
	switch {
	case count == 0:
		return fmt.Errorf("selector %q matched no elements", selector)
	case count > 1:
		return fmt.Errorf("selector %q matched %d elements", selector, count)
	default:
		return nil
	}
}
