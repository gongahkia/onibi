package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	approvals "github.com/gongahkia/onibi/internal/approval"
)

const ntfyActionTTL = 5 * time.Minute

type ActionSigner struct {
	secret []byte
	mu     sync.Mutex
	used   map[string]time.Time
}

func NewActionSigner(secret []byte) (*ActionSigner, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
	}
	cp := append([]byte(nil), secret...)
	return &ActionSigner{secret: cp, used: map[string]time.Time{}}, nil
}

func (s *ActionSigner) SignedApprovalURL(baseURL, approvalID string, verdict approvals.Verdict, now time.Time) (string, error) {
	if s == nil {
		return "", errors.New("action signer nil")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", errors.New("action base URL must include scheme and host")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	exp := now.Add(ntfyActionTTL).Unix()
	nonceText := base64.RawURLEncoding.EncodeToString(nonce)
	sig := s.signature(approvalID, verdict, exp, nonceText)
	base.Path = strings.TrimRight(base.Path, "/") + "/ntfy/approval/" + url.PathEscape(approvalID) + "/" + url.PathEscape(string(verdict))
	q := base.Query()
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("nonce", nonceText)
	q.Set("sig", sig)
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func (s *ActionSigner) VerifyAndConsume(approvalID string, verdict approvals.Verdict, values url.Values, now time.Time) error {
	if s == nil {
		return errors.New("action signer nil")
	}
	exp, err := strconv.ParseInt(values.Get("exp"), 10, 64)
	if err != nil || exp <= now.Unix() {
		return errors.New("action link expired")
	}
	nonce := strings.TrimSpace(values.Get("nonce"))
	if nonce == "" {
		return errors.New("action link nonce required")
	}
	want := s.signature(approvalID, verdict, exp, nonce)
	got := strings.TrimSpace(values.Get("sig"))
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return errors.New("action link signature invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, usedUntil := range s.used {
		if !usedUntil.After(now) {
			delete(s.used, key)
		}
	}
	if s.used[got] != (time.Time{}) {
		return errors.New("action link already used")
	}
	s.used[got] = time.Unix(exp, 0)
	return nil
}

func (s *ActionSigner) signature(approvalID string, verdict approvals.Verdict, exp int64, nonce string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = fmt.Fprintf(mac, "ntfy-action-v1\n%s\n%s\n%d\n%s", approvalID, verdict, exp, nonce)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleNtfyApprovalAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.actionSigner == nil {
		http.NotFound(w, r)
		return
	}
	if s.approvalQueue == nil {
		http.Error(w, "approval queue unavailable", http.StatusServiceUnavailable)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	verdict, err := mapNtfyActionVerdict(r.PathValue("verdict"))
	if id == "" || err != nil {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	if err := s.actionSigner.VerifyAndConsume(id, verdict, r.URL.Query(), time.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := s.approvalQueue.Decide(context.WithoutCancel(r.Context()), id, verdict, "", "ntfy action", 0); err != nil {
		writeApprovalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("approval " + id + " " + string(verdict)))
}

func mapNtfyActionVerdict(value string) (approvals.Verdict, error) {
	switch approvals.Verdict(strings.TrimSpace(value)) {
	case approvals.VerdictApprove:
		return approvals.VerdictApprove, nil
	case approvals.VerdictDeny:
		return approvals.VerdictDeny, nil
	default:
		return "", errors.New("bad verdict")
	}
}
