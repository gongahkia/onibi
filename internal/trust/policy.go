package trust

import (
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
	Match      Match         `toml:"match"`
	Effect     Effect        `toml:"effect"`
	ExpiresRaw string        `toml:"expires"`
	Expires    time.Duration `toml:"-"`
	Never      bool          `toml:"-"`
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

func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	return Parse(data)
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

func (p Policy) Evaluate(req Request) (Rule, bool) {
	for _, rule := range p.Rules {
		if rule.matches(req) {
			return rule, true
		}
	}
	return Rule{}, false
}

func (r *Rule) validate(i int) error {
	prefix := fmt.Sprintf("rule %d", i+1)
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

func globMatch(pattern, value string) bool {
	ok, err := doublestar.Match(pattern, value)
	return err == nil && ok
}
