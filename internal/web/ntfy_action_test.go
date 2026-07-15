package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/store"
)

func TestNtfySignedActionApprovesOnce(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := approval.New(db, time.Minute)
	signer, err := NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer, ExperimentalProviders: true})
	id, _, err := q.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	rawURL, err := signer.SignedApprovalURL("https://onibi.example", id, approval.VerdictApprove, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(rawURL, "https://onibi.example")
	req := httptest.NewRequest(http.MethodPost, path, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	got, err := q.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestProviderActionRoutesRequireExperimentalProfile(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := approval.New(db, time.Minute)
	signer, err := NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	ntfyID, _, err := q.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	gotifyID, _, err := q.Request(t.Context(), "s2", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	ntfyURL, err := signer.SignedApprovalURL("https://onibi.example", ntfyID, approval.VerdictApprove, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gotifyURL, err := signer.SignedGotifyApprovalPageURL("https://onibi.example", gotifyID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	disabled := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, strings.TrimPrefix(ntfyURL, "https://onibi.example"), nil),
		httptest.NewRequest(http.MethodGet, strings.TrimPrefix(gotifyURL, "https://onibi.example"), nil),
	} {
		w := httptest.NewRecorder()
		disabled.Handler().ServeHTTP(w, request)
		if w.Code != http.StatusNotFound {
			t.Fatalf("disabled provider action status = %d body = %q", w.Code, w.Body.String())
		}
	}
	for _, id := range []string{ntfyID, gotifyID} {
		got, err := q.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != approval.StatePending {
			t.Fatalf("disabled provider action changed %s to %s", id, got.State)
		}
	}
	enabled := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer, ExperimentalProviders: true})
	w := httptest.NewRecorder()
	enabled.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, strings.TrimPrefix(ntfyURL, "https://onibi.example"), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enabled ntfy action status = %d body = %q", w.Code, w.Body.String())
	}
	got, err := q.Get(t.Context(), ntfyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("enabled ntfy action state = %s", got.State)
	}
}

func TestNtfySignedActionRequiresPost(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signer, err := NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, ApprovalQueue: approval.New(db, time.Minute), ActionSigner: signer, ExperimentalProviders: true})
	req := httptest.NewRequest(http.MethodGet, "/ntfy/approval/a1/approve", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestNtfySignedActionRejectsBadSignature(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := approval.New(db, time.Minute)
	signer, err := NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer, ExperimentalProviders: true})
	req := httptest.NewRequest(http.MethodPost, "/ntfy/approval/a1/approve?exp=4102444800&nonce=n&sig=bad", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestGotifySignedApprovalPageRendersOneUseActions(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := approval.New(db, time.Minute)
	signer, err := NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer, ExperimentalProviders: true})
	id, _, err := q.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	rawURL, err := signer.SignedGotifyApprovalPageURL("https://onibi.example", id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(rawURL, "https://onibi.example")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "onibi.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `/gotify/approval/`+id+`/approve`) || !strings.Contains(body, `/gotify/approval/`+id+`/deny`) {
		t.Fatalf("body = %q", body)
	}
	match := regexp.MustCompile(`action="([^"]*/gotify/approval/` + id + `/approve[^"]*)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("approve action missing in %q", body)
	}
	approvePath := strings.TrimPrefix(html.UnescapeString(match[1]), "https://onibi.example")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, approvePath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("approve status = %d body = %q", w.Code, w.Body.String())
	}
	got, err := q.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d body = %q", w.Code, w.Body.String())
	}
}
