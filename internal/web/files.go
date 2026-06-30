package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	gitignore "github.com/sabhiram/go-gitignore"
)

const (
	fileTreeMaxDepth         = 8
	fileTreeMaxEntriesPerDir = 200
	fileTreeMaxResponseBytes = 1 << 20
	fileContentMaxBytes      = 2 << 20
)

type FileTreeResponse struct {
	SessionID string          `json:"session_id"`
	Root      string          `json:"root"`
	Entries   []FileTreeEntry `json:"entries"`
	Truncated bool            `json:"truncated,omitempty"`
}

type FileTreeEntry struct {
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	Type      string          `json:"type"`
	Size      int64           `json:"size,omitempty"`
	Children  []FileTreeEntry `json:"children,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

type FileContentResponse struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	Binary    bool   `json:"binary"`
	Content   string `json:"content,omitempty"`
}

func (s *Server) handleFilesTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}
	if sessionID == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	if s.sessionList == nil && s.db == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	cwd, ok, err := s.fileTreeSessionCWD(r.Context(), sessionID)
	if err != nil {
		s.log.Warn("web file tree session failed", "request_id", requestID(r), "session_id", sessionID, "err", err)
		http.Error(w, "session lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	resp, err := buildFileTree(sessionID, cwd)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errFileTreeCWDRequired) {
			status = http.StatusUnprocessableEntity
		} else if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		} else if errors.Is(err, errFileTreeCWDNotDir) {
			status = http.StatusBadRequest
		}
		s.log.Warn("web file tree failed", "request_id", requestID(r), "session_id", sessionID, "cwd", cwd, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	body, err := marshalFileTreeWithinLimit(resp)
	if err != nil {
		s.log.Warn("web file tree marshal failed", "request_id", requestID(r), "session_id", sessionID, "err", err)
		http.Error(w, "file tree too large", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (s *Server) handleFilesContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}
	if sessionID == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if s.sessionList == nil && s.db == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	cwd, ok, err := s.fileTreeSessionCWD(r.Context(), sessionID)
	if err != nil {
		s.log.Warn("web file content session failed", "request_id", requestID(r), "session_id", sessionID, "err", err)
		http.Error(w, "session lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	resp, err := readFileContent(sessionID, cwd, rel)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, errFileTreeCWDRequired), errors.Is(err, errFileContentPathRequired):
			status = http.StatusBadRequest
		case errors.Is(err, errFileTreeCWDNotDir), errors.Is(err, errFileContentIsDir), errors.Is(err, errFileContentSymlink):
			status = http.StatusBadRequest
		case errors.Is(err, errFileContentPathEscape):
			status = http.StatusBadRequest
		case errors.Is(err, errFileContentTooLarge):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, os.ErrNotExist):
			status = http.StatusNotFound
		}
		s.log.Warn("web file content failed", "request_id", requestID(r), "session_id", sessionID, "cwd", cwd, "path", rel, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) fileTreeSessionCWD(ctx context.Context, sessionID string) (string, bool, error) {
	if s.sessionList != nil {
		rows, err := s.sessionList(ctx, SessionListOptions{})
		if err != nil {
			return "", false, err
		}
		for _, row := range rows {
			if row.ID == sessionID {
				return strings.TrimSpace(row.CWD), true, nil
			}
		}
	}
	if s.db != nil {
		row, ok, err := s.db.SessionByID(ctx, sessionID)
		if err != nil || !ok || row.Ended {
			return "", ok && !row.Ended, err
		}
		return strings.TrimSpace(row.CWD), true, nil
	}
	return "", false, nil
}

var (
	errFileTreeCWDRequired = errors.New("session cwd required")
	errFileTreeCWDNotDir   = errors.New("session cwd is not a directory")

	errFileContentPathRequired = errors.New("path required")
	errFileContentPathEscape   = errors.New("path escapes session cwd")
	errFileContentIsDir        = errors.New("path is a directory")
	errFileContentSymlink      = errors.New("symlink files unavailable")
	errFileContentTooLarge     = errors.New("file exceeds 2MB")
)

func buildFileTree(sessionID, cwd string) (FileTreeResponse, error) {
	root, err := sessionFileRoot(cwd)
	if err != nil {
		return FileTreeResponse{}, err
	}
	ignorer, err := loadFileTreeGitIgnore(root)
	if err != nil {
		return FileTreeResponse{}, err
	}
	entries, truncated, err := readFileTreeDir(root, "", 0, ignorer)
	if err != nil {
		return FileTreeResponse{}, err
	}
	return FileTreeResponse{
		SessionID: sessionID,
		Root:      root,
		Entries:   entries,
		Truncated: truncated,
	}, nil
}

func readFileContent(sessionID, cwd, rel string) (FileContentResponse, error) {
	root, err := sessionFileRoot(cwd)
	if err != nil {
		return FileContentResponse{}, err
	}
	abs, cleanRel, err := resolveSessionFilePath(root, rel)
	if err != nil {
		return FileContentResponse{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return FileContentResponse{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return FileContentResponse{}, errFileContentSymlink
	}
	if info.IsDir() {
		return FileContentResponse{}, errFileContentIsDir
	}
	if info.Size() > fileContentMaxBytes {
		return FileContentResponse{}, errFileContentTooLarge
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileContentResponse{}, err
	}
	if len(data) > fileContentMaxBytes {
		return FileContentResponse{}, errFileContentTooLarge
	}
	mime := detectFileContentMIME(data)
	binary := fileContentIsBinary(data, mime)
	resp := FileContentResponse{
		SessionID: sessionID,
		Path:      cleanRel,
		Type:      "file",
		MIME:      mime,
		Size:      int64(len(data)),
		Binary:    binary,
	}
	if !binary {
		resp.Content = string(data)
	}
	return resp, nil
}

func sessionFileRoot(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", errFileTreeCWDRequired
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errFileTreeCWDNotDir
	}
	return root, nil
}

func resolveSessionFilePath(root, rel string) (string, string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", "", errFileContentPathRequired
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || filepath.IsAbs(cleanRel) {
		return "", "", errFileContentPathEscape
	}
	abs := filepath.Clean(filepath.Join(root, cleanRel))
	backRel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", "", err
	}
	if backRel == "." || backRel == ".." || strings.HasPrefix(backRel, ".."+string(filepath.Separator)) {
		return "", "", errFileContentPathEscape
	}
	return abs, filepath.ToSlash(backRel), nil
}

func detectFileContentMIME(data []byte) string {
	if len(data) == 0 {
		return "text/plain; charset=utf-8"
	}
	return http.DetectContentType(data)
}

func fileContentIsBinary(data []byte, mime string) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	if strings.HasPrefix(mime, "text/") {
		return false
	}
	if strings.Contains(mime, "charset=utf-8") && utf8.Valid(data) {
		return false
	}
	return !utf8.Valid(data)
}

func loadFileTreeGitIgnore(root string) (*gitignore.GitIgnore, error) {
	path := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return gitignore.CompileIgnoreLines(".git/"), nil
		}
		return nil, err
	}
	return gitignore.CompileIgnoreFileAndLines(path, ".git/")
}

func readFileTreeDir(root, rel string, depth int, ignorer *gitignore.GitIgnore) ([]FileTreeEntry, bool, error) {
	dir := filepath.Join(root, filepath.FromSlash(rel))
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	entries := make([]FileTreeEntry, 0, min(len(items), fileTreeMaxEntriesPerDir))
	truncated := false
	for _, item := range items {
		childRel := item.Name()
		if rel != "" {
			childRel = rel + "/" + item.Name()
		}
		isDir := item.IsDir()
		if fileTreeIgnored(ignorer, childRel, isDir) {
			continue
		}
		if len(entries) >= fileTreeMaxEntriesPerDir {
			truncated = true
			break
		}
		info, err := item.Info()
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", childRel, err)
		}
		entry := FileTreeEntry{
			Name: item.Name(),
			Path: childRel,
			Type: fileTreeEntryType(info.Mode(), isDir),
		}
		if entry.Type == "file" {
			entry.Size = info.Size()
		}
		if isDir {
			if depth+1 >= fileTreeMaxDepth {
				entry.Truncated = true
			} else {
				children, childTruncated, err := readFileTreeDir(root, childRel, depth+1, ignorer)
				if err != nil {
					return nil, false, err
				}
				entry.Children = children
				entry.Truncated = childTruncated
			}
		}
		entries = append(entries, entry)
	}
	return entries, truncated, nil
}

func fileTreeIgnored(ignorer *gitignore.GitIgnore, rel string, isDir bool) bool {
	if ignorer == nil || rel == "" {
		return false
	}
	rel = filepath.ToSlash(rel)
	if ignorer.MatchesPath(rel) {
		return true
	}
	return isDir && ignorer.MatchesPath(rel+"/")
}

func fileTreeEntryType(mode os.FileMode, isDir bool) string {
	if mode&os.ModeSymlink != 0 {
		return "symlink"
	}
	if isDir {
		return "dir"
	}
	return "file"
}

func marshalFileTreeWithinLimit(resp FileTreeResponse) ([]byte, error) {
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	if len(body) <= fileTreeMaxResponseBytes {
		return body, nil
	}
	resp.Truncated = true
	for len(body) > fileTreeMaxResponseBytes && pruneFileTreeEntry(&resp.Entries) {
		body, err = json.Marshal(resp)
		if err != nil {
			return nil, err
		}
	}
	if len(body) > fileTreeMaxResponseBytes {
		return nil, errors.New("file tree response exceeds 1MB")
	}
	return body, nil
}

func pruneFileTreeEntry(entries *[]FileTreeEntry) bool {
	if len(*entries) == 0 {
		return false
	}
	last := &(*entries)[len(*entries)-1]
	if pruneFileTreeEntry(&last.Children) {
		last.Truncated = true
		return true
	}
	*entries = (*entries)[:len(*entries)-1]
	return true
}
