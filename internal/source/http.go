package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

const defaultMaxResponseBytes int64 = 8 << 20

type HTTPRequest struct {
	SourceID string
	Method   string
	URL      string
	Headers  http.Header
	Body     []byte
	CacheTTL time.Duration
}

type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	FetchedAt  time.Time
	FromCache  bool
}

type HTTPError struct {
	Category   string
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

func (err *HTTPError) Error() string {
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s HTTP error (%d): %v", err.Category, err.StatusCode, err.Err)
	}
	return fmt.Sprintf("%s HTTP error: %v", err.Category, err.Err)
}

func (err *HTTPError) Unwrap() error { return err.Err }

type cacheEntry struct {
	response HTTPResponse
	expires  time.Time
}

type inflightCall struct {
	done chan struct{}
	resp HTTPResponse
	err  error
}

type HTTPClient struct {
	client           *http.Client
	userAgent        string
	maxResponseBytes int64
	policies         map[string]domain.SourcePolicy

	mu             sync.Mutex
	cache          map[string]cacheEntry
	inflight       map[string]*inflightCall
	hostSemaphores map[string]chan struct{}
	failures       map[string]int
	openUntil      map[string]time.Time
}

func NewHTTPClient(infos []domain.SourceInfo) *HTTPClient {
	policies := make(map[string]domain.SourcePolicy, len(infos))
	hosts := make(map[string]chan struct{})
	for _, info := range infos {
		policies[info.ID] = info.Policy
		for _, host := range info.Policy.PermittedHosts {
			if _, exists := hosts[host]; exists {
				continue
			}
			concurrency := info.Policy.Concurrency
			if concurrency < 1 {
				concurrency = 1
			}
			hosts[host] = make(chan struct{}, concurrency)
		}
	}
	return &HTTPClient{
		client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		userAgent:        "kaypoh/0.1 (+https://github.com/gongahkia/kaypoh)",
		maxResponseBytes: defaultMaxResponseBytes,
		policies:         policies,
		cache:            make(map[string]cacheEntry),
		inflight:         make(map[string]*inflightCall),
		hostSemaphores:   hosts,
		failures:         make(map[string]int),
		openUntil:        make(map[string]time.Time),
	}
}

func (client *HTTPClient) Fetch(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	if request.CacheTTL < 0 {
		return HTTPResponse{}, errors.New("cache TTL cannot be negative")
	}
	if request.Method == "" {
		request.Method = http.MethodGet
	}
	if request.Method != http.MethodGet {
		request.CacheTTL = 0
	}
	parsed, policy, err := client.validate(request)
	if err != nil {
		return HTTPResponse{}, err
	}
	key := request.SourceID + "\x00" + request.Method + "\x00" + request.URL + "\x00" + string(request.Body)
	now := time.Now()
	client.mu.Lock()
	if until := client.openUntil[request.SourceID]; now.Before(until) {
		client.mu.Unlock()
		return HTTPResponse{}, &HTTPError{Category: "circuit_open", RetryAfter: time.Until(until), Err: errors.New("source is temporarily degraded")}
	}
	if cached, ok := client.cache[key]; ok && now.Before(cached.expires) {
		cached.response.FromCache = true
		client.mu.Unlock()
		return cached.response, nil
	}
	if call, ok := client.inflight[key]; ok {
		client.mu.Unlock()
		select {
		case <-call.done:
			return call.resp, call.err
		case <-ctx.Done():
			return HTTPResponse{}, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	client.inflight[key] = call
	cached, hasCached := client.cache[key]
	client.mu.Unlock()

	call.resp, call.err = client.fetchWithRetry(ctx, request, parsed, policy, cached, hasCached)
	client.mu.Lock()
	if call.err == nil {
		client.failures[request.SourceID] = 0
		delete(client.openUntil, request.SourceID)
		if request.CacheTTL > 0 {
			client.cache[key] = cacheEntry{response: call.resp, expires: time.Now().Add(request.CacheTTL)}
		}
	} else {
		client.failures[request.SourceID]++
		if client.failures[request.SourceID] >= 3 {
			client.openUntil[request.SourceID] = time.Now().Add(5 * time.Minute)
		}
	}
	delete(client.inflight, key)
	close(call.done)
	client.mu.Unlock()
	return call.resp, call.err
}

func (client *HTTPClient) validate(request HTTPRequest) (*url.URL, domain.SourcePolicy, error) {
	policy, ok := client.policies[request.SourceID]
	if !ok {
		return nil, domain.SourcePolicy{}, fmt.Errorf("unknown source policy %q", request.SourceID)
	}
	if !policy.AllowsNetwork() {
		return nil, domain.SourcePolicy{}, fmt.Errorf("%s: %w", request.SourceID, ErrPolicyDisabled)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, domain.SourcePolicy{}, fmt.Errorf("invalid HTTPS URL %q", request.URL)
	}
	if !allowedHost(parsed.Hostname(), policy.PermittedHosts) {
		return nil, domain.SourcePolicy{}, fmt.Errorf("host %q is not permitted for source %q", parsed.Hostname(), request.SourceID)
	}
	return parsed, policy, nil
}

func allowedHost(host string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(host, candidate) {
			return true
		}
	}
	return false
}

func (client *HTTPClient) fetchWithRetry(ctx context.Context, request HTTPRequest, parsed *url.URL, policy domain.SourcePolicy, cached cacheEntry, hasCached bool) (HTTPResponse, error) {
	maxAttempts := 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, retryAfter(lastErr))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return HTTPResponse{}, ctx.Err()
			}
		}
		response, err := client.fetchOnce(ctx, request, parsed, policy, cached, hasCached)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !retryable(err) {
			break
		}
	}
	return HTTPResponse{}, lastErr
}

func (client *HTTPClient) fetchOnce(ctx context.Context, request HTTPRequest, parsed *url.URL, policy domain.SourcePolicy, cached cacheEntry, hasCached bool) (HTTPResponse, error) {
	semaphore := client.hostSemaphores[parsed.Hostname()]
	select {
	case semaphore <- struct{}{}:
		defer func() { <-semaphore }()
	case <-ctx.Done():
		return HTTPResponse{}, ctx.Err()
	}
	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, request.Method, request.URL, bytes.NewReader(request.Body))
	if err != nil {
		return HTTPResponse{}, &HTTPError{Category: "request", Err: err}
	}
	httpRequest.Header.Set("User-Agent", client.userAgent)
	httpRequest.Header.Set("Accept", "application/json, application/geo+json;q=0.9, */*;q=0.1")
	for key, values := range request.Headers {
		for _, value := range values {
			httpRequest.Header.Add(key, value)
		}
	}
	if hasCached && request.Method == http.MethodGet {
		if etag := cached.response.Header.Get("ETag"); etag != "" {
			httpRequest.Header.Set("If-None-Match", etag)
		}
		if modified := cached.response.Header.Get("Last-Modified"); modified != "" {
			httpRequest.Header.Set("If-Modified-Since", modified)
		}
	}
	started := time.Now()
	response, err := client.client.Do(httpRequest)
	if err != nil {
		return HTTPResponse{}, &HTTPError{Category: "transport", Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && hasCached {
		cached.response.Header = response.Header.Clone()
		cached.response.FetchedAt = time.Now()
		cached.response.FromCache = true
		return cached.response, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return HTTPResponse{}, &HTTPError{Category: statusCategory(response.StatusCode), StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After")), Err: fmt.Errorf("unexpected response status %s", response.Status)}
	}
	limited := io.LimitReader(response.Body, client.maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return HTTPResponse{}, &HTTPError{Category: "read", StatusCode: response.StatusCode, Err: err}
	}
	if int64(len(body)) > client.maxResponseBytes {
		return HTTPResponse{}, &HTTPError{Category: "response_too_large", StatusCode: response.StatusCode, Err: fmt.Errorf("response exceeds %d byte limit", client.maxResponseBytes)}
	}
	_ = started
	return HTTPResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: bytes.Clone(body), FetchedAt: time.Now()}, nil
}

func retryable(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return errors.Is(err, context.DeadlineExceeded)
	}
	return httpErr.Category == "transport" || httpErr.Category == "rate_limited" || httpErr.Category == "server"
}

func retryAfter(err error) time.Duration {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.RetryAfter
	}
	return 0
}

func retryDelay(attempt int, requested time.Duration) time.Duration {
	if requested > 0 {
		return requested
	}
	base := time.Duration(1<<uint(attempt-1)) * 250 * time.Millisecond
	return base + time.Duration(rand.IntN(200))*time.Millisecond
}

func parseRetryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func statusCategory(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status >= 500:
		return "server"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "authorization"
	default:
		return "client"
	}
}
