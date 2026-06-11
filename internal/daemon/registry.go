package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
)

// Session represents one hosted agent process. The buffer captures recent
// PTY output for rendering; LastActivity is touched on every output read so
// the idle detector can fire turn-complete events.
type Session struct {
	ID    string // stable opaque id (also passed to hooks via ONIBI_SESSION_ID)
	Name  string // human-readable label (CLI --name or auto from agent)
	Agent string // adapter name (claude, codex, ...)
	Cmd   string
	Host  *pty.Host
	Buf   *RingBuffer
	Transport  string
	TmuxTarget string

	mu           sync.Mutex
	started      time.Time
	lastActivity time.Time
	ended        bool
}

// NewSession returns an initialized Session. Call NewID for a random ID.
func NewSession(id, name, agent string, host *pty.Host, bufSize int) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		Name:         name,
		Agent:        agent,
		Host:         host,
		Buf:          NewRingBuffer(bufSize),
		Transport:    "pty",
		started:      now,
		lastActivity: now,
	}
}

// Touch records activity now. Called from the output reader on every read.
func (s *Session) Touch() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// SinceActivity returns elapsed time since last Touch.
func (s *Session) SinceActivity() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastActivity)
}

func (s *Session) LastActivityAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastActivity
}

// StartedAt returns when the session began.
func (s *Session) StartedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// MarkEnded sets the session as ended. Idempotent.
func (s *Session) MarkEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return false
	}
	s.ended = true
	return true
}

// Ended reports whether MarkEnded was called.
func (s *Session) Ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// Registry is the in-memory session map. Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*Session{}}
}

// ErrUnknownSession is returned by Get when the id isn't registered.
var ErrUnknownSession = errors.New("session not found")

// Add registers s. Returns an error if its ID is already taken.
func (r *Registry) Add(s *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sessions[s.ID]; dup {
		return errors.New("duplicate session id")
	}
	r.sessions[s.ID] = s
	return nil
}

// Get returns the session for id.
func (r *Registry) Get(id string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, ErrUnknownSession
	}
	return s, nil
}

// Remove deletes id (no-op if missing).
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
}

// List returns all sessions sorted by start time (oldest first).
func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	// simple insertion sort by StartedAt; N is tiny
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1].StartedAt().After(out[j].StartedAt()) {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

// NewID returns a random 16-char hex id suitable for use as a session id.
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// fallback to time-based; collisions astronomically unlikely
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(b)
}
