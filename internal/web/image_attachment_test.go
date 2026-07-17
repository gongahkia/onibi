package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImageAttachmentWritesOwnerImageByHash(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.uploadDir = filepath.Join(t.TempDir(), "uploads")
	cookie := ownerCookie(t, srv)
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1, 2, 3}
	resp := requestImageAttachment(t, srv, cookie, imageAttachmentRequest{MIME: "image/png", Data: base64.StdEncoding.EncodeToString(data)}, http.StatusOK)
	sum := sha256.Sum256(data)
	wantHash := hex.EncodeToString(sum[:])
	if resp.SHA256 != wantHash || resp.MIME != "image/png" || resp.Size != int64(len(data)) {
		t.Fatalf("response = %#v", resp)
	}
	wantPath := filepath.Join(srv.uploadDir, wantHash+".png")
	if resp.Path != wantPath {
		t.Fatalf("path = %q want %q", resp.Path, wantPath)
	}
	got, err := os.ReadFile(wantPath)
	if err != nil || !slices.Equal(got, data) {
		t.Fatalf("uploaded data = %#v err = %v", got, err)
	}
}

func TestImageAttachmentRejectsSVGAndOversize(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.uploadDir = t.TempDir()
	cookie := ownerCookie(t, srv)
	requestImageAttachment(t, srv, cookie, imageAttachmentRequest{MIME: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte("<svg></svg>"))}, http.StatusUnsupportedMediaType)
	requestImageAttachment(t, srv, cookie, imageAttachmentRequest{MIME: "image/png", Data: base64.StdEncoding.EncodeToString(append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, imageAttachmentMaxBytes)...))}, http.StatusRequestEntityTooLarge)
}

func TestImageAttachmentRequiresOwnerAndCSRF(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.uploadDir = t.TempDir()
	cookie := ownerCookie(t, srv)
	body := `{"mime":"image/png","data":"iVBORw0KGgo="}`
	for _, tc := range []struct {
		name   string
		cookie bool
		csrf   bool
		want   int
	}{
		{name: "unauthenticated", want: http.StatusUnauthorized},
		{name: "missing csrf", cookie: true, want: http.StatusForbidden},
		{name: "owner", cookie: true, csrf: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/attachments/images", strings.NewReader(body))
			if tc.cookie {
				req.AddCookie(cookie)
			}
			if tc.csrf {
				addCSRF(req, cookie.Value)
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestFilesRoutesAreAbsent(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	cookie := ownerCookie(t, srv)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/files/tree?session=s1"},
		{method: http.MethodGet, path: "/files/content?session=s1&path=x"},
		{method: http.MethodPut, path: "/files/content?session=s1&path=x", body: `{"content":"x"}`},
		{method: http.MethodPost, path: "/files/upload", body: `{"mime":"image/png","data":"iVBORw0KGgo="}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(cookie)
		addCSRF(req, cookie.Value)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, w.Code)
		}
	}
}

func ownerCookie(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	return rr.Result().Cookies()[0]
}

func requestImageAttachment(t *testing.T, srv *Server, cookie *http.Cookie, payload imageAttachmentRequest, wantStatus int) imageAttachmentResponse {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/attachments/images", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	addCSRF(req, cookie.Value)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var resp imageAttachmentResponse
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}
