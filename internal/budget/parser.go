package budget

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ClaudeParser struct {
	BaseDir string

	mu      sync.Mutex
	offsets map[string]int64
	totals  map[string]CostEvent
}

type SessionRef struct {
	SessionID         string
	ProviderSessionID string
	Agent             string
	CWD               string
}

type CostEvent struct {
	SessionID         string    `json:"session_id"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	Agent             string    `json:"agent,omitempty"`
	Transcript        string    `json:"transcript,omitempty"`
	Model             string    `json:"model,omitempty"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	TotalInputTokens  int64     `json:"total_input_tokens"`
	TotalOutputTokens int64     `json:"total_output_tokens"`
	Turns             int       `json:"turns"`
	Offset            int64     `json:"offset"`
	TS                time.Time `json:"ts"`
}

type TurnUsage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
}

func NewClaudeParser(baseDir string) *ClaudeParser {
	return &ClaudeParser{
		BaseDir: strings.TrimSpace(baseDir),
		offsets: map[string]int64{},
		totals:  map[string]CostEvent{},
	}
}

func (p *ClaudeParser) Update(ref SessionRef) (CostEvent, bool, error) {
	path, err := p.FindTranscript(ref)
	if err != nil {
		return CostEvent{}, false, err
	}
	return p.UpdateFile(path, ref)
}

func (p *ClaudeParser) Current(ref SessionRef) (CostEvent, bool, error) {
	path, err := p.FindTranscript(ref)
	if err != nil {
		return CostEvent{}, false, err
	}
	return p.CurrentFile(path)
}

func (p *ClaudeParser) UpdateFile(path string, ref SessionRef) (CostEvent, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CostEvent{}, false, errors.New("transcript path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return CostEvent{}, false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return CostEvent{}, false, err
	}
	if info.IsDir() {
		return CostEvent{}, false, errors.New("transcript path is a directory")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	offset := p.offsets[abs]
	if offset > info.Size() {
		offset = 0
	}
	turns, nextOffset, err := readUsageFrom(abs, offset)
	if err != nil {
		return CostEvent{}, false, err
	}
	p.offsets[abs] = nextOffset
	if len(turns) == 0 {
		return CostEvent{}, false, nil
	}
	event := p.totals[abs]
	event.SessionID = ref.SessionID
	event.ProviderSessionID = ref.ProviderSessionID
	event.Agent = ref.Agent
	event.Transcript = abs
	event.Offset = nextOffset
	event.TS = time.Now().UTC()
	event.Turns = len(turns)
	event.InputTokens = 0
	event.OutputTokens = 0
	for _, turn := range turns {
		event.InputTokens += turn.InputTokens
		event.OutputTokens += turn.OutputTokens
		if strings.TrimSpace(turn.Model) != "" {
			event.Model = turn.Model
		}
	}
	event.TotalInputTokens += event.InputTokens
	event.TotalOutputTokens += event.OutputTokens
	p.totals[abs] = event
	return event, true, nil
}

func (p *ClaudeParser) CurrentFile(path string) (CostEvent, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CostEvent{}, false, errors.New("transcript path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return CostEvent{}, false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	event, ok := p.totals[abs]
	return event, ok, nil
}

func (p *ClaudeParser) FindTranscript(ref SessionRef) (string, error) {
	base, err := p.baseDir()
	if err != nil {
		return "", err
	}
	projectKey, err := ClaudeProjectKey(ref.CWD)
	if err != nil {
		return "", err
	}
	projectDir := filepath.Join(base, "projects", projectKey)
	sessionID := strings.TrimSpace(ref.ProviderSessionID)
	if sessionID != "" {
		for _, path := range []string{
			filepath.Join(projectDir, "sessions", sessionID+".jsonl"),
			filepath.Join(projectDir, sessionID+".jsonl"),
			filepath.Join(projectDir, "sessions", sessionID, "session.jsonl"),
			filepath.Join(projectDir, sessionID, sessionID+".jsonl"),
		} {
			if isFile(path) {
				return path, nil
			}
		}
		return "", os.ErrNotExist
	}
	return newestTranscript(projectDir)
}

func (p *ClaudeParser) baseDir() (string, error) {
	if p != nil && strings.TrimSpace(p.BaseDir) != "" {
		return filepath.Abs(p.BaseDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func ClaudeProjectKey(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", errors.New("cwd required")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	return strings.ReplaceAll(filepath.ToSlash(abs), "/", "-"), nil
}

func ParseUsageLine(line []byte) (TurnUsage, bool, error) {
	var top map[string]any
	if err := json.Unmarshal(line, &top); err != nil {
		return TurnUsage{}, false, err
	}
	usage := objectAt(top, "usage")
	if usage == nil {
		usage = nestedObject(top, "message", "usage")
	}
	if usage == nil {
		return TurnUsage{}, false, nil
	}
	input, inputOK := intAt(usage, "input_tokens")
	output, outputOK := intAt(usage, "output_tokens")
	if !inputOK && !outputOK {
		return TurnUsage{}, false, nil
	}
	model := stringAt(top, "model")
	if model == "" {
		model = nestedString(top, "message", "model")
	}
	return TurnUsage{Model: model, InputTokens: input, OutputTokens: output}, true, nil
}

func readUsageFrom(path string, offset int64) ([]TurnUsage, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var turns []TurnUsage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		turn, ok, err := ParseUsageLine([]byte(line))
		if err != nil {
			return nil, offset, err
		}
		if ok {
			turns = append(turns, turn)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, offset, err
	}
	next, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, offset, err
	}
	return turns, next, nil
}

func newestTranscript(projectDir string) (string, error) {
	var best string
	var bestMod time.Time
	for _, pattern := range []string{
		filepath.Join(projectDir, "*.jsonl"),
		filepath.Join(projectDir, "sessions", "*.jsonl"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			if best == "" || info.ModTime().After(bestMod) {
				best = path
				bestMod = info.ModTime()
			}
		}
	}
	if best == "" {
		return "", os.ErrNotExist
	}
	return best, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func objectAt(m map[string]any, key string) map[string]any {
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func nestedObject(m map[string]any, key1, key2 string) map[string]any {
	inner := objectAt(m, key1)
	if inner == nil {
		return nil
	}
	return objectAt(inner, key2)
}

func stringAt(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func nestedString(m map[string]any, key1, key2 string) string {
	inner := objectAt(m, key1)
	if inner == nil {
		return ""
	}
	return stringAt(inner, key2)
}

func intAt(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}
