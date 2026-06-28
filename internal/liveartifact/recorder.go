package liveartifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
)

const EnvDir = "ONIBI_LIVE_ARTIFACT_DIR"

type Recorder struct {
	mu       sync.Mutex
	path     string
	provider string
	secrets  []string
	events   []Event
	started  time.Time
}

type Event struct {
	At     time.Time      `json:"at"`
	Name   string         `json:"name"`
	Fields map[string]any `json:"fields,omitempty"`
}

type File struct {
	Provider string          `json:"provider"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished"`
	Env      map[string]bool `json:"env"`
	Events   []Event         `json:"events"`
}

func New(provider string, envKeys ...string) (*Recorder, error) {
	dir := strings.TrimSpace(os.Getenv(EnvDir))
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "onibi-live-smoke")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r := &Recorder{
		path:     filepath.Join(dir, fmt.Sprintf("%s-%s.json", safeName(provider), now.Format("20060102T150405Z"))),
		provider: provider,
		started:  now,
	}
	for _, key := range envKeys {
		if v := strings.TrimSpace(os.Getenv(key)); len(v) >= 8 {
			r.secrets = append(r.secrets, v)
		}
	}
	r.Record("start", map[string]any{"env": envPresence(envKeys)})
	return r, nil
}

func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *Recorder) Record(name string, fields map[string]any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{At: time.Now().UTC(), Name: name, Fields: r.sanitizeMap(fields)})
}

func (r *Recorder) Error(name string, err error) {
	if err == nil {
		r.Record(name, nil)
		return
	}
	r.Record(name, map[string]any{"error": err.Error()})
}

func (r *Recorder) Close(envKeys ...string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f := File{Provider: r.provider, Started: r.started, Finished: time.Now().UTC(), Env: envPresence(envKeys), Events: r.events}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, append(b, '\n'), 0o600)
}

func (r *Recorder) sanitizeMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = r.sanitize(v)
	}
	return out
}

func (r *Recorder) sanitize(v any) any {
	switch x := v.(type) {
	case string:
		s := approval.Scrub(x)
		for _, sec := range r.secrets {
			s = strings.ReplaceAll(s, sec, "[REDACTED]")
		}
		return s
	case error:
		return r.sanitize(x.Error())
	case []string:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = r.sanitize(s).(string)
		}
		return out
	case map[string]string:
		out := map[string]string{}
		for k, s := range x {
			out[k] = r.sanitize(s).(string)
		}
		return out
	default:
		return v
	}
}

func envPresence(keys []string) map[string]bool {
	out := map[string]bool{}
	for _, key := range keys {
		out[key] = strings.TrimSpace(os.Getenv(key)) != ""
	}
	return out
}

func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "provider"
	}
	return b.String()
}
