package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	srv := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer})
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
	srv := New(Options{DB: db, ApprovalQueue: approval.New(db, time.Minute), ActionSigner: signer})
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
	srv := New(Options{DB: db, ApprovalQueue: q, ActionSigner: signer})
	req := httptest.NewRequest(http.MethodPost, "/ntfy/approval/a1/approve?exp=4102444800&nonce=n&sig=bad", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}
