package trust

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"
)

type Effect string

const (
	EffectAutoApprove  Effect = "auto_approve"
	EffectAlwaysPrompt Effect = "always_prompt"
	EffectDeny         Effect = "deny"
)

type Policy struct {
	Rules []Rule `toml:"rule"`
}

type Rule struct {
	ID         string        `toml:"-"`
	Match      Match         `toml:"match"`
	Effect     Effect        `toml:"effect"`
	ExpiresRaw string        `toml:"expires"`
	Expires    time.Duration `toml:"-"`
	Never      bool          `toml:"-"`
	Runtime    bool          `toml:"-"`
	ExpiresAt  time.Time     `toml:"-"`
}

type Match struct {
	Tool  string `toml:"tool"`
	Path  string `toml:"path"`
	Agent string `toml:"agent"`
}

type Request struct {
	Tool  string
	Path  string
	Agent string
}

type View struct {
	Roots []RootView `json:"roots"`
}

type RootView struct {
	Root       string     `json:"root"`
	PolicyPath string     `json:"policy_path"`
	Rules      []RuleView `json:"rules"`
}

type RuleView struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Runtime   bool   `json:"runtime"`
	Effect    Effect `json:"effect"`
	Expires   string `json:"expires"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Path      string `json:"path,omitempty"`
	Agent     string `json:"agent,omitempty"`
}

func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	return Parse(data)
}

func Save(path string, p Policy) error {
	p = filePolicy(p)
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := toml.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Parse(data []byte) (Policy, error) {
	var p Policy
	if err := toml.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p Policy) Validate() error {
	for i := range p.Rules {
		if err := p.Rules[i].validate(i); err != nil {
			return err
		}
	}
	return nil
}

func PolicyPath(root string) string {
	return filepath.Join(root, ".onibi", "trust.toml")
}

func ViewForPolicy(root string, p Policy, now time.Time) RootView {
	root = filepath.Clean(root)
	view := RootView{Root: root, PolicyPath: PolicyPath(root)}
	fileN := 0
	runtimeN := 0
	for _, rule := range p.Rules {
		if rule.expired(now) {
			continue
		}
		source := "file"
		id := ""
		if rule.Runtime {
			source = "runtime"
			runtimeN++
			id = rule.ID
			if id == "" {
				id = fmt.Sprintf("runtime:%d", runtimeN)
			}
		} else {
			fileN++
			id = fmt.Sprintf("file:%d", fileN)
		}
		view.Rules = append(view.Rules, RuleView{
			ID:        id,
			Source:    source,
			Runtime:   rule.Runtime,
			Effect:    rule.Effect,
			Expires:   rule.ExpiresRaw,
			ExpiresAt: expiresAtString(rule),
			Tool:      rule.Match.Tool,
			Path:      rule.Match.Path,
			Agent:     rule.Match.Agent,
		})
	}
	return view
}

func (p Policy) Evaluate(req Request) (Rule, bool) {
	return p.EvaluateAt(req, time.Now())
}

func (p Policy) EvaluateAt(req Request, now time.Time) (Rule, bool) {
	for _, rule := range p.Rules {
		if rule.expired(now) {
			continue
		}
		if rule.matches(req) {
			return rule, true
		}
	}
	return Rule{}, false
}

func RuntimeRule(match Match, effect Effect, ttl time.Duration, now time.Time) Rule {
	return Rule{
		ID:         NewRuntimeID(),
		Match:      match,
		Effect:     effect,
		ExpiresRaw: ttl.String(),
		Expires:    ttl,
		Runtime:    true,
		ExpiresAt:  now.Add(ttl),
	}
}

func NewRuntimeID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "runtime:" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("runtime:%d", time.Now().UnixNano())
}

func (r *Rule) validate(i int) error {
	prefix := fmt.Sprintf("rule %d", i+1)
	if i < 0 {
		prefix = "runtime rule"
	}
	if r.Match.Tool == "" && r.Match.Path == "" && r.Match.Agent == "" {
		return fmt.Errorf("%s match required", prefix)
	}
	if r.Match.Tool != "" && !doublestar.ValidatePattern(r.Match.Tool) {
		return fmt.Errorf("%s invalid tool glob %q", prefix, r.Match.Tool)
	}
	if r.Match.Path != "" && !doublestar.ValidatePattern(filepath.ToSlash(r.Match.Path)) {
		return fmt.Errorf("%s invalid path glob %q", prefix, r.Match.Path)
	}
	if strings.TrimSpace(r.Match.Agent) != r.Match.Agent {
		return fmt.Errorf("%s invalid agent match %q", prefix, r.Match.Agent)
	}
	switch r.Effect {
	case EffectAutoApprove, EffectAlwaysPrompt, EffectDeny:
	default:
		return fmt.Errorf("%s invalid effect %q", prefix, r.Effect)
	}
	expires := strings.TrimSpace(r.ExpiresRaw)
	if expires == "" {
		return fmt.Errorf("%s expires required", prefix)
	}
	if strings.EqualFold(expires, "never") {
		r.ExpiresRaw = "never"
		r.Expires = 0
		r.Never = true
		return nil
	}
	d, err := time.ParseDuration(expires)
	if err != nil {
		return fmt.Errorf("%s invalid expires %q", prefix, r.ExpiresRaw)
	}
	if d <= 0 {
		return fmt.Errorf("%s expires must be positive", prefix)
	}
	r.ExpiresRaw = expires
	r.Expires = d
	r.Never = false
	return nil
}

func (r Rule) matches(req Request) bool {
	if r.Match.Tool != "" && !globMatch(r.Match.Tool, req.Tool) {
		return false
	}
	if r.Match.Path != "" && !globMatch(filepath.ToSlash(r.Match.Path), filepath.ToSlash(req.Path)) {
		return false
	}
	if r.Match.Agent != "" && r.Match.Agent != req.Agent {
		return false
	}
	return true
}

func (r Rule) expired(now time.Time) bool {
	return !r.Never && !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}

func expiresAtString(rule Rule) string {
	if rule.ExpiresAt.IsZero() {
		return ""
	}
	return rule.ExpiresAt.UTC().Format(time.RFC3339Nano)
}

func filePolicy(p Policy) Policy {
	out := Policy{Rules: make([]Rule, 0, len(p.Rules))}
	for _, rule := range p.Rules {
		if rule.Runtime {
			continue
		}
		rule.ID = ""
		rule.Runtime = false
		rule.ExpiresAt = time.Time{}
		out.Rules = append(out.Rules, rule)
	}
	return out
}

func PersistedRule(rule Rule) Rule {
	rule.ID = ""
	rule.Runtime = false
	rule.ExpiresAt = time.Time{}
	return rule
}

func globMatch(pattern, value string) bool {
	ok, err := doublestar.Match(pattern, value)
	return err == nil && ok
}
