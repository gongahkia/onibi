package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/prompt"
)

const (
	sessionTargetPrefix = "session:"
	defaultSessionLines = 200
	maxSessionLines     = 2000
	maxSessionKeyBytes  = 32
	maxProgramBytes     = 256
	maxProgramArgs      = 32
	maxArgBytes         = 1024
	maxPathBytes        = 4096
	maxNameBytes        = 64
)

var errSessionsUnavailable = errors.New("managed sessions are unavailable")

// SessionBackend reads and drives multiplexer sessions owned by the Onibi
// daemon. Unlike Ghostty targets, these can be read without focusing them
// and keep running when the terminal app quits.
type SessionBackend interface {
	Sessions(context.Context) ([]intake.SessionInfo, error)
	Capture(ctx context.Context, id string, lines int) (SessionCapture, error)
	// Execute must return an intake.Reject error only when nothing reached
	// the session. It returns the session ID, which is new for "new".
	Execute(context.Context, SessionAction) (string, error)
}

type SessionCapture struct {
	SessionID string         `json:"sessionId"`
	Text      string         `json:"text"`
	ANSI      string         `json:"ansi,omitempty"`
	Prompt    *prompt.Prompt `json:"prompt,omitempty"`
}

// SessionAction is the browser's request for a managed session. New, kill
// and interrupt are the only lifecycle operations exposed remotely; adopt
// stays local because it reaches sessions Onibi did not start.
type SessionAction struct {
	Kind      string   `json:"kind"`
	SessionID string   `json:"sessionId"`
	Text      string   `json:"text,omitempty"`
	Enter     bool     `json:"enter,omitempty"`
	Key       string   `json:"key,omitempty"`
	Program   string   `json:"program,omitempty"`
	Args      []string `json:"args,omitempty"`
	CWD       string   `json:"cwd,omitempty"`
	Name      string   `json:"name,omitempty"`
}

func validSessionID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, ch := range id {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// plainField rejects control characters, which have no business in a
// program, argument, folder or name and could forge terminal input.
func plainField(value string, maxBytes int) bool {
	if len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateNewSession(action SessionAction) error {
	if action.SessionID != "" || action.Text != "" || action.Key != "" || action.Enter {
		return errors.New("A new session takes a program, folder and optional name only.")
	}
	if action.Program == "" || !plainField(action.Program, maxProgramBytes) {
		return errors.New("Choose a program: shell, a command name, or an absolute path.")
	}
	if len(action.Args) > maxProgramArgs {
		return errors.New("Use at most 32 arguments.")
	}
	for _, arg := range action.Args {
		if !plainField(arg, maxArgBytes) {
			return errors.New("Arguments must be plain text of at most 1 KiB each.")
		}
	}
	if !(filepath.IsAbs(action.CWD) || action.CWD == "~" || strings.HasPrefix(action.CWD, "~/")) || !plainField(action.CWD, maxPathBytes) {
		return errors.New("Choose a folder on your Mac as an absolute path.")
	}
	if !plainField(action.Name, maxNameBytes) {
		return errors.New("Session names are plain text of at most 64 bytes.")
	}
	return nil
}

func validateSessionAction(action SessionAction) error {
	if action.Kind == "new" {
		return validateNewSession(action)
	}
	if action.Program != "" || action.Args != nil || action.CWD != "" || (action.Name != "" && action.Kind != "rename") {
		return errors.New("Only a new session takes a program, folder or name.")
	}
	if !validSessionID(action.SessionID) {
		return errors.New("An explicit managed session is required.")
	}
	switch action.Kind {
	case "input":
		if action.Text == "" || action.Key != "" {
			return errors.New("Input requires text only.")
		}
		if len(action.Text) > maxInputBytes || !utf8.ValidString(action.Text) {
			return errors.New("Input must be valid text of at most 16 KiB.")
		}
	case "key":
		if action.Key == "" || len(action.Key) > maxSessionKeyBytes || action.Text != "" || action.Enter {
			return errors.New("Choose one supported key.")
		}
	case "interrupt", "kill":
		if action.Text != "" || action.Key != "" || action.Enter {
			return errors.New("This action takes no input.")
		}
	case "rename":
		if action.Name == "" || !plainField(action.Name, maxNameBytes) || action.Text != "" || action.Key != "" || action.Enter {
			return errors.New("Session names are plain text of at most 64 bytes.")
		}
	default:
		return errors.New("Unsupported session action.")
	}
	return nil
}

func sessionActionHash(action SessionAction) string {
	raw, _ := json.Marshal(action)
	sum := sha256.Sum256(append([]byte(sessionTargetPrefix), raw...))
	return hex.EncodeToString(sum[:])
}

func (s *Server) sessionList(w http.ResponseWriter, r *http.Request) {
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "sessions_unavailable", "Managed sessions are not configured.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	sessions, err := s.managed.Sessions(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "daemon_unavailable", "The Onibi daemon is not running. Restart onibi web, or run onibi start, to see managed sessions.")
		return
	}
	if sessions == nil {
		sessions = []intake.SessionInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) sessionSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "sessions_unavailable", "Managed sessions are not configured.")
		return
	}
	id := r.URL.Query().Get("sessionId")
	if !validSessionID(id) {
		writeError(w, http.StatusBadRequest, "invalid_session", "An explicit managed session is required.")
		return
	}
	lines := defaultSessionLines
	if raw := r.URL.Query().Get("lines"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxSessionLines {
			writeError(w, http.StatusBadRequest, "invalid_lines", "Request between 1 and 2000 lines of history.")
			return
		}
		lines = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	capture, err := s.managed.Capture(ctx, id, lines)
	if intake.IsRejected(err) && !errors.Is(err, errSessionsUnavailable) {
		writeError(w, http.StatusNotFound, "session_gone", "This session has ended or is no longer managed by Onibi.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable", "This session could not be read. Check that the Onibi daemon is running.")
		return
	}
	capture.SessionID = id
	capture.Text = trimSnapshot(capture.Text)
	capture.ANSI = trimSnapshot(capture.ANSI)
	capture.Prompt = detectPrompt(capture.Text)
	writeJSON(w, http.StatusOK, capture)
}

// detectPrompt reports a prompt waiting in the newest output so the browser
// can offer its choices; nil when nothing is waiting.
func detectPrompt(text string) *prompt.Prompt {
	if p := prompt.Detect(text); p.Reason != "" {
		return &p
	}
	return nil
}

// trimSnapshot keeps the newest output within the response budget, starting
// on a line boundary so a cut never begins inside an escape sequence.
func trimSnapshot(text string) string {
	if len(text) <= maxSnapshotBytes {
		return text
	}
	text = text[len(text)-maxSnapshotBytes:]
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			return text[i+1:]
		}
	}
	for len(text) > 0 && !utf8.RuneStart(text[0]) {
		text = text[1:]
	}
	return text
}

// executeSession runs inside the action pipeline after the receipt is
// claimed and the execution slot is held.
func (s *Server) executeSession(ctx context.Context, r *http.Request, receipt Receipt, action SessionAction) Receipt {
	if s.managed == nil {
		receipt.State, receipt.Error = "rejected", "Managed sessions are not configured. No input was sent."
		return receipt
	}
	if action.Kind == "new" {
		return s.startSession(ctx, r, receipt, action)
	}
	sessions, err := s.managed.Sessions(ctx)
	if err != nil {
		receipt.State, receipt.Error = "rejected", "The Onibi daemon could not be checked before execution. No input was sent."
		return receipt
	}
	found := false
	for _, session := range sessions {
		if session.ID == action.SessionID {
			found = true
			break
		}
	}
	if !found {
		receipt.State, receipt.Error = "rejected", "This session has ended or is no longer managed by Onibi. No input was sent."
		return receipt
	}
	if _, _, ok := s.authenticate(r.WithContext(ctx)); !ok {
		receipt.State, receipt.Error = "rejected", "Browser access ended during session validation. No input was sent."
		return receipt
	}
	if _, err := s.managed.Execute(ctx, action); err != nil {
		if intake.IsRejected(err) {
			receipt.State, receipt.Error = "rejected", sessionRejection(err)
			if action.Kind == "rename" {
				receipt.Error = strings.Replace(receipt.Error, "No input was sent.", "The name was not changed.", 1)
			}
		} else {
			receipt.State, receipt.Error = "uncertain", uncertainMessage
		}
		return receipt
	}
	receipt.State = "completed"
	if action.Kind == "rename" {
		receipt.Result = &ghostty.Result{TargetID: sessionTargetPrefix + action.SessionID, Message: "Renamed to " + action.Name + "."}
	}
	return receipt
}

func (s *Server) startSession(ctx context.Context, r *http.Request, receipt Receipt, action SessionAction) Receipt {
	if _, _, ok := s.authenticate(r.WithContext(ctx)); !ok {
		receipt.State, receipt.Error = "rejected", "Browser access ended before the session started. Nothing was started."
		return receipt
	}
	if action.CWD == "~" || strings.HasPrefix(action.CWD, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			receipt.State, receipt.Error = "rejected", "Your home folder could not be found. Nothing was started."
			return receipt
		}
		action.CWD = filepath.Join(home, strings.TrimPrefix(action.CWD, "~"))
	}
	id, err := s.managed.Execute(ctx, action)
	if err != nil {
		if intake.IsRejected(err) {
			receipt.State, receipt.Error = "rejected", strings.Replace(sessionRejection(err), "No input was sent.", "Nothing was started.", 1)
		} else {
			receipt.State, receipt.Error = "uncertain", "The session may have started. Check the session list before starting it again."
		}
		return receipt
	}
	receipt.State = "completed"
	receipt.Result = &ghostty.Result{TargetID: sessionTargetPrefix + id, Message: "It keeps running when Ghostty quits."}
	return receipt
}

// Daemon rejections are curated short messages; transport errors may carry
// local paths, so they get a fixed message.
func sessionRejection(err error) string {
	var daemonErr *daemonRejection
	if errors.As(err, &daemonErr) && len(daemonErr.message) <= 256 {
		return daemonErr.message + ". No input was sent."
	}
	return "The Onibi daemon was unavailable. No input was sent."
}

type daemonRejection struct{ message string }

func (e *daemonRejection) Error() string { return e.message }

// DaemonSessions is a SessionBackend that talks to the local daemon's
// same-user intake socket.
type DaemonSessions struct {
	Socket string
}

func (d DaemonSessions) request(ev intake.Event, timeout time.Duration) (intake.Response, error) {
	if d.Socket == "" {
		return intake.Response{}, intake.Reject(errSessionsUnavailable)
	}
	resp, err := intake.Request(d.Socket, ev, timeout)
	if err != nil {
		if intake.IsRejected(err) {
			return intake.Response{}, intake.Reject(errors.Join(errSessionsUnavailable, err))
		}
		return intake.Response{}, err
	}
	if resp.Reason != "" {
		if resp.Rejected {
			return intake.Response{}, intake.Reject(&daemonRejection{message: resp.Reason})
		}
		return intake.Response{}, errors.New(resp.Reason)
	}
	return resp, nil
}

func (d DaemonSessions) Sessions(context.Context) ([]intake.SessionInfo, error) {
	resp, err := d.request(intake.Event{Type: intake.TypeSessionList}, 3*time.Second)
	return resp.Sessions, err
}

func (d DaemonSessions) Capture(_ context.Context, id string, lines int) (SessionCapture, error) {
	resp, err := d.request(intake.Event{Type: intake.TypeSessionCapture, Session: id, Lines: lines}, 8*time.Second)
	return SessionCapture{SessionID: id, Text: resp.Text, ANSI: resp.ANSI}, err
}

func (d DaemonSessions) Execute(_ context.Context, action SessionAction) (string, error) {
	var ev intake.Event
	switch action.Kind {
	case "new":
		ev = intake.Event{Type: intake.TypeSessionNew, Agent: action.Program, Args: action.Args, CWD: action.CWD, Name: action.Name}
	case "input":
		ev = intake.Event{Type: intake.TypeSessionInput, Session: action.SessionID, Text: action.Text, Enter: action.Enter, Exact: true}
	case "key":
		ev = intake.Event{Type: intake.TypeSessionKey, Session: action.SessionID, Key: action.Key}
	case "interrupt", "kill":
		ev = intake.Event{Type: intake.TypeSessionControl, Session: action.SessionID, Action: action.Kind, Exact: true}
	case "rename":
		ev = intake.Event{Type: intake.TypeSessionControl, Session: action.SessionID, Action: "rename", Name: action.Name, Exact: true}
	default:
		return "", intake.Reject(errors.New("unsupported session action"))
	}
	resp, err := d.request(ev, 15*time.Second)
	if err != nil {
		return "", err
	}
	if action.Kind == "new" {
		return resp.SessionID, nil
	}
	return action.SessionID, nil
}
