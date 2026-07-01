package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateCheckOnlyStablePrintsLatest(t *testing.T) {
	withFakeUpdateAPI(t)
	out, _ := executeRoot(t, "update", "--check-only", "--channel", "stable", "--color", "never")
	got := out.String()
	if !strings.Contains(got, "latest stable: v1.2.3") || !strings.Contains(got, "https://example.test/v1.2.3") {
		t.Fatalf("output = %q", got)
	}
}

func TestUpdateCheckOnlyBetaPrintsLatestPrerelease(t *testing.T) {
	withFakeUpdateAPI(t)
	out, _ := executeRoot(t, "update", "--check-only", "--channel", "beta", "--color", "never")
	got := out.String()
	if !strings.Contains(got, "latest beta: v1.3.0-beta.1") || !strings.Contains(got, "https://example.test/v1.3.0-beta.1") {
		t.Fatalf("output = %q", got)
	}
}

func TestUpdateApplyGuardUntilVerificationExists(t *testing.T) {
	withFakeUpdateAPI(t)
	out, errOut, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("err = %v stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
}

func withFakeUpdateAPI(t *testing.T) {
	t.Helper()
	oldLatest := updateLatestURL
	oldReleases := updateReleasesURL
	oldClient := updateHTTPClient
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Fatalf("headers = %#v", r.Header)
		}
		switch r.URL.Path {
		case "/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://example.test/v1.2.3"}`))
		case "/releases":
			if r.URL.Query().Get("per_page") != "1" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.3.0-beta.1","html_url":"https://example.test/v1.3.0-beta.1","prerelease":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	updateLatestURL = server.URL + "/latest"
	updateReleasesURL = server.URL + "/releases?per_page=1"
	updateHTTPClient = server.Client()
	t.Cleanup(func() {
		updateLatestURL = oldLatest
		updateReleasesURL = oldReleases
		updateHTTPClient = oldClient
		server.Close()
	})
}
