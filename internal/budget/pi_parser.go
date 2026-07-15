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

const maxPiSessionMatches = 10000

type PiParser struct {
	BaseDir string

	mu      sync.Mutex
	offsets map[string]int64
	totals  map[string]CostEvent
	paths   map[string]string
}

func NewPiParser(baseDir string) *PiParser {
	return &PiParser{
		BaseDir: strings.TrimSpace(baseDir),
		offsets: map[string]int64{},
		totals:  map[string]CostEvent{},
		paths:   map[string]string{},
	}
}

func (p *PiParser) Update(ref SessionRef) (CostEvent, bool, error) {
	path, err := p.FindTranscript(ref)
	if err != nil {
		return CostEvent{}, false, err
	}
	return p.UpdateFile(path, ref)
}

func (p *PiParser) Current(ref SessionRef) (CostEvent, bool, error) {
	path, err := p.FindTranscript(ref)
	if err != nil {
		return CostEvent{}, false, err
	}
	return p.CurrentFile(path)
}

func (p *PiParser) UpdateFile(path string, ref SessionRef) (CostEvent, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CostEvent{}, false, errors.New("Pi transcript path required")
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
		return CostEvent{}, false, errors.New("Pi transcript path is a directory")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	offset := p.offsets[abs]
	if offset > info.Size() {
		offset = 0
	}
	turns, nextOffset, err := readPiUsageFrom(abs, offset)
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
	event.Agent = "pi"
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

func (p *PiParser) CurrentFile(path string) (CostEvent, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CostEvent{}, false, errors.New("Pi transcript path required")
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

func (p *PiParser) FindTranscript(ref SessionRef) (string, error) {
	providerSessionID := strings.TrimSpace(ref.ProviderSessionID)
	if !validPiSessionID(providerSessionID) {
		return "", errors.New("Pi provider session id required")
	}
	p.mu.Lock()
	cached := p.paths[providerSessionID]
	p.mu.Unlock()
	if cached != "" && piSessionHeaderMatches(cached, providerSessionID) {
		return cached, nil
	}
	base, err := p.baseDir(ref)
	if err != nil {
		return "", err
	}
	path, err := findPiSessionFile(base, providerSessionID)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.paths[providerSessionID] = path
	p.mu.Unlock()
	return path, nil
}

func (p *PiParser) baseDir(ref SessionRef) (string, error) {
	if p != nil && strings.TrimSpace(p.BaseDir) != "" {
		return filepath.Abs(p.BaseDir)
	}
	if sessionDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_SESSION_DIR")); sessionDir != "" {
		return filepath.Abs(sessionDir)
	}
	agentDir, err := piAgentDir()
	if err != nil {
		return "", err
	}
	if cwd := strings.TrimSpace(ref.CWD); cwd != "" {
		if sessionDir, ok, err := piSettingsSessionDir(filepath.Join(cwd, ".pi", "settings.json"), cwd); err != nil {
			return "", err
		} else if ok {
			return sessionDir, nil
		}
	}
	if sessionDir, ok, err := piSettingsSessionDir(filepath.Join(agentDir, "settings.json"), agentDir); err != nil {
		return "", err
	} else if ok {
		return sessionDir, nil
	}
	return filepath.Join(agentDir, "sessions"), nil
}

func piAgentDir() (string, error) {
	if agentDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); agentDir != "" {
		return filepath.Abs(agentDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

func piSettingsSessionDir(path, root string) (string, bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var settings struct {
		SessionDir string `json:"sessionDir"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return "", false, err
	}
	dir := strings.TrimSpace(settings.SessionDir)
	if dir == "" {
		return "", false, nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	return abs, true, nil
}

func findPiSessionFile(base, sessionID string) (string, error) {
	var found string
	seen := 0
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		seen++
		if seen > maxPiSessionMatches {
			return errors.New("Pi session search limit exceeded")
		}
		name := strings.TrimSuffix(entry.Name(), ".jsonl")
		if name != sessionID && !strings.HasSuffix(name, "_"+sessionID) {
			return nil
		}
		if piSessionHeaderMatches(path, sessionID) {
			found = path
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) && found != "" {
		return found, nil
	}
	if err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}

func piSessionHeaderMatches(path, sessionID string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var header struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		return json.Unmarshal([]byte(line), &header) == nil && header.Type == "session" && header.ID == sessionID
	}
	return false
}

func validPiSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func ParsePiUsageLine(line []byte) (TurnUsage, bool, error) {
	var top map[string]any
	if err := json.Unmarshal(line, &top); err != nil {
		return TurnUsage{}, false, err
	}
	if stringAt(top, "type") != "message" {
		return TurnUsage{}, false, nil
	}
	message := objectAt(top, "message")
	if message == nil || stringAt(message, "role") != "assistant" {
		return TurnUsage{}, false, nil
	}
	usage := objectAt(message, "usage")
	if usage == nil {
		return TurnUsage{}, false, nil
	}
	input, inputOK := intAt(usage, "input")
	output, outputOK := intAt(usage, "output")
	if !inputOK && !outputOK {
		return TurnUsage{}, false, nil
	}
	if input < 0 || output < 0 {
		return TurnUsage{}, false, errors.New("Pi usage tokens must be non-negative")
	}
	return TurnUsage{Model: stringAt(message, "model"), InputTokens: input, OutputTokens: output}, true, nil
}

func readPiUsageFrom(path string, offset int64) ([]TurnUsage, int64, error) {
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
		turn, ok, err := ParsePiUsageLine([]byte(line))
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
