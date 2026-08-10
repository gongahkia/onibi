package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

func TestHTTPClientRejectsNonAllowlistedHost(t *testing.T) {
	client := NewHTTPClient([]domain.SourceInfo{{ID: "test", Name: "test", Operator: "test", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, PermittedHosts: []string{"allowed.example"}}}})
	if _, err := client.Fetch(context.Background(), HTTPRequest{SourceID: "test", URL: "https://blocked.example/"}); err == nil {
		t.Fatal("expected allowlist error")
	}
}

func TestHTTPClientCoalescesCachedResponse(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("ETag", "test")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewHTTPClient([]domain.SourceInfo{{ID: "test", Name: "test", Operator: "test", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, PermittedHosts: []string{parsed.Hostname()}, Timeout: time.Second}}})
	client.client = server.Client()
	request := HTTPRequest{SourceID: "test", URL: server.URL, CacheTTL: time.Minute}
	if _, err := client.Fetch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	response, err := client.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !response.FromCache || requests != 1 {
		t.Fatalf("cache response = %#v, requests = %d", response, requests)
	}
}
