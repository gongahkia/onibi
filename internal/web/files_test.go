package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFilesTreeHonorsGitignore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".gitignore", "ignored.txt\nbuild/\n*.log\n")
	writeTestFile(t, root, "README.md", "# repo\n")
	writeTestFile(t, root, "src/main.go", "package main\n")
	writeTestFile(t, root, "ignored.txt", "ignored\n")
	writeTestFile(t, root, "build/out.txt", "ignored\n")
	writeTestFile(t, root, "trace.log", "ignored\n")
	writeTestFile(t, root, ".git/config", "ignored\n")

	resp := requestFileTree(t, root, http.StatusOK)
	paths := flattenFileTree(resp.Entries)
	for _, want := range []string{".gitignore", "README.md", "src", "src/main.go"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("missing %q in %#v", want, paths)
		}
	}
	for _, ignored := range []string{"ignored.txt", "build", "build/out.txt", "trace.log", ".git", ".git/config"} {
		if slices.Contains(paths, ignored) {
			t.Fatalf("ignored path %q returned in %#v", ignored, paths)
		}
	}
	if resp.SessionID != "s1" || resp.Root != root {
		t.Fatalf("response = %#v", resp)
	}
}

func TestFilesTreeCapsEntriesPerDir(t *testing.T) {
	root := t.TempDir()
	for i := range fileTreeMaxEntriesPerDir + 5 {
		writeTestFile(t, root, fmt.Sprintf("file-%03d.txt", i), "x\n")
	}
	resp := requestFileTree(t, root, http.StatusOK)
	if len(resp.Entries) != fileTreeMaxEntriesPerDir || !resp.Truncated {
		t.Fatalf("entries=%d truncated=%v", len(resp.Entries), resp.Truncated)
	}
}

func TestFilesTreeRequiresKnownSession(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/files/tree?session=missing", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestFilesContentReturnsText(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/main.go", "package main\n")
	resp := requestFileContent(t, root, "src/main.go", http.StatusOK)
	if resp.Path != "src/main.go" || resp.Content != "package main\n" || resp.Binary || resp.Size != int64(len("package main\n")) {
		t.Fatalf("content = %#v", resp)
	}
	if resp.MIME == "" || resp.Type != "file" || resp.SessionID != "s1" {
		t.Fatalf("content = %#v", resp)
	}
}

func TestFilesContentBlocksPathTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, parent, "secret.txt", "secret\n")
	_ = requestFileContent(t, root, "../secret.txt", http.StatusBadRequest)
}

func TestFilesContentBinaryReturnsMetadataOnly(t *testing.T) {
	root := t.TempDir()
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1, 2, 3}
	path := filepath.Join(root, "image.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	resp := requestFileContent(t, root, "image.png", http.StatusOK)
	if !resp.Binary || resp.Content != "" || resp.Size != int64(len(data)) || resp.MIME != "image/png" {
		t.Fatalf("content = %#v", resp)
	}
}

func TestFilesContentCapsLargeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, make([]byte, fileContentMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = requestFileContent(t, root, "large.txt", http.StatusRequestEntityTooLarge)
}

func requestFileTree(t *testing.T, root string, wantStatus int) FileTreeResponse {
	t.Helper()
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionList = func(_ context.Context, _ SessionListOptions) ([]SessionSummary, error) {
		return []SessionSummary{{ID: "s1", CWD: root}}, nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/files/tree?session=s1", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var resp FileTreeResponse
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func requestFileContent(t *testing.T, root, rel string, wantStatus int) FileContentResponse {
	t.Helper()
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionList = func(_ context.Context, _ SessionListOptions) ([]SessionSummary, error) {
		return []SessionSummary{{ID: "s1", CWD: root}}, nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/files/content?session=s1&path="+url.QueryEscape(rel), nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var resp FileContentResponse
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func flattenFileTree(entries []FileTreeEntry) []string {
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Path)
		out = append(out, flattenFileTree(entry.Children)...)
	}
	return out
}
