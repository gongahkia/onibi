package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

type Session struct {
	ID           string
	Name         string
	Agent        string
	Cmd          string
	CWD          string
	Transport    string
	TmuxTarget   string
	Buf          *RingBuffer
	mu           sync.Mutex
	started      time.Time
	lastActivity time.Time
	ended        bool
}

func NewSession(id, name, agent string, size int) *Session {
	now := time.Now()
	return &Session{ID: id, Name: name, Agent: agent, Transport: "tmux", Buf: NewRingBuffer(size), started: now, lastActivity: now}
}
func newSessionAt(id, name, agent string, size int, started, last time.Time) *Session {
	s := NewSession(id, name, agent, size)
	if !started.IsZero() {
		s.started = started
	}
	if !last.IsZero() {
		s.lastActivity = last
	}
	return s
}
func (s *Session) Touch()                    { s.mu.Lock(); s.lastActivity = time.Now(); s.mu.Unlock() }
func (s *Session) LastActivityAt() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.lastActivity }
func (s *Session) StartedAt() time.Time      { s.mu.Lock(); defer s.mu.Unlock(); return s.started }
func (s *Session) MarkEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return false
	}
	s.ended = true
	return true
}
func (s *Session) Ended() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.ended }

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewRegistry() *Registry { return &Registry{sessions: map[string]*Session{}} }

var ErrUnknownSession = errors.New("session not found")

func (r *Registry) Add(s *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s.ID]; ok {
		return errors.New("duplicate session id")
	}
	r.sessions[s.ID] = s
	return nil
}
func (r *Registry) Get(id string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, ErrUnknownSession
	}
	return s, nil
}
func (r *Registry) List() []*Session {
	r.mu.RLock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt().Before(out[j].StartedAt()) })
	return out
}
func NewID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
}
