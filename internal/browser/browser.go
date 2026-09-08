// Package browser provides bounded, read-only Playwright page extraction.
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

type Request struct {
	URL                string
	LoginURL           string
	Username           string
	Password           string
	UsernameSelector   string
	PasswordSelector   string
	SubmitSelector     string
	ReadySelector      string
	JSONSelector       string
	SessionStateBase64 string
	PermittedHosts     []string
	Timeout            time.Duration
}

// Fetcher keeps partner adapters testable without launching Chromium.
type Fetcher interface {
	Fetch(context.Context, Request) ([]byte, error)
	Close() error
}

type Playwright struct {
	mu      sync.Mutex
	pw      *playwright.Playwright
	browser playwright.Browser
	sem     chan struct{}
}

func New(maxContexts int) *Playwright {
	if maxContexts < 1 {
		maxContexts = 1
	}
	return &Playwright{sem: make(chan struct{}, maxContexts)}
}

func (client *Playwright) Fetch(ctx context.Context, request Request) ([]byte, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	if err := acquire(ctx, client.sem); err != nil {
		return nil, err
	}
	defer func() { <-client.sem }()
	if err := client.start(); err != nil {
		return nil, err
	}

	options, err := contextOptions(request.SessionStateBase64)
	if err != nil {
		return nil, err
	}
	options.TimezoneId = playwright.String("Asia/Singapore")
	context, err := client.browser.NewContext(options)
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	defer context.Close()
	page, err := context.NewPage()
	if err != nil {
		return nil, fmt.Errorf("open browser page: %w", err)
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	page.SetDefaultTimeout(float64(timeout.Milliseconds()))
	page.SetDefaultNavigationTimeout(float64(timeout.Milliseconds()))

	if request.SessionStateBase64 == "" && request.LoginURL != "" {
		if _, err := page.Goto(request.LoginURL); err != nil {
			return nil, fmt.Errorf("open partner login: %w", err)
		}
		if err := page.Locator(request.UsernameSelector).Fill(request.Username); err != nil {
			return nil, fmt.Errorf("fill partner username: %w", err)
		}
		if err := page.Locator(request.PasswordSelector).Fill(request.Password); err != nil {
			return nil, fmt.Errorf("fill partner password: %w", err)
		}
		if err := page.Locator(request.SubmitSelector).Click(); err != nil {
			return nil, fmt.Errorf("submit partner login: %w", err)
		}
	}
	if _, err := page.Goto(request.URL); err != nil {
		return nil, fmt.Errorf("open partner availability: %w", err)
	}
	if request.ReadySelector != "" {
		if _, err := page.WaitForSelector(request.ReadySelector); err != nil {
			return nil, fmt.Errorf("wait for partner availability: %w", err)
		}
	}
	if request.JSONSelector != "" {
		contents, err := page.Locator(request.JSONSelector).TextContent()
		if err != nil {
			return nil, fmt.Errorf("read partner availability payload: %w", err)
		}
		return []byte(strings.TrimSpace(contents)), nil
	}
	contents, err := page.Content()
	if err != nil {
		return nil, fmt.Errorf("read partner availability page: %w", err)
	}
	return []byte(contents), nil
}

func (client *Playwright) start() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.browser != nil {
		return nil
	}
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("start Playwright (run 'playwright install chromium'): %w", err)
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		_ = pw.Stop()
		return fmt.Errorf("launch bundled Chromium (run 'playwright install chromium'): %w", err)
	}
	client.pw = pw
	client.browser = browser
	return nil
}

func (client *Playwright) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	var result error
	if client.browser != nil {
		result = client.browser.Close()
		client.browser = nil
	}
	if client.pw != nil {
		if err := client.pw.Stop(); err != nil && result == nil {
			result = err
		}
		client.pw = nil
	}
	return result
}

func contextOptions(encoded string) (playwright.BrowserNewContextOptions, error) {
	if strings.TrimSpace(encoded) == "" {
		return playwright.BrowserNewContextOptions{}, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return playwright.BrowserNewContextOptions{}, errors.New("decode imported browser session: invalid base64")
	}
	var state playwright.OptionalStorageState
	if err := json.Unmarshal(decoded, &state); err != nil {
		return playwright.BrowserNewContextOptions{}, errors.New("decode imported browser session: invalid storage-state JSON")
	}
	return playwright.BrowserNewContextOptions{StorageState: &state}, nil
}

func validateRequest(request Request) error {
	if err := validateURL(request.URL, request.PermittedHosts); err != nil {
		return fmt.Errorf("availability URL: %w", err)
	}
	if request.LoginURL != "" {
		if err := validateURL(request.LoginURL, request.PermittedHosts); err != nil {
			return fmt.Errorf("login URL: %w", err)
		}
	}
	if request.SessionStateBase64 == "" && request.LoginURL != "" {
		if request.Username == "" || request.Password == "" || request.UsernameSelector == "" || request.PasswordSelector == "" || request.SubmitSelector == "" {
			return errors.New("partner login needs username, password, and all login selectors")
		}
	}
	return nil
}

func validateURL(raw string, permittedHosts []string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("must be an HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	for _, permitted := range permittedHosts {
		permitted = strings.ToLower(strings.TrimSpace(permitted))
		if host == permitted || strings.HasSuffix(host, "."+permitted) {
			return nil
		}
	}
	return fmt.Errorf("host %q is not permitted", host)
}

func acquire(ctx context.Context, semaphore chan struct{}) error {
	select {
	case semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
