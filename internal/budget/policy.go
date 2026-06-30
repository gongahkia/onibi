package budget

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type OverrunAction string

const (
	OverrunInterrupt OverrunAction = "interrupt"
	OverrunKill      OverrunAction = "kill"
	OverrunWarn      OverrunAction = "warn"
)

const MicroCentsPerCent int64 = 1_000_000

const maxInt64 = int64(^uint64(0) >> 1)

type Policy struct {
	Global  GlobalPolicy  `toml:"global"`
	Session SessionPolicy `toml:"session"`
}

type GlobalPolicy struct {
	MaxTokensPerDay int64 `toml:"max_tokens_per_day"`
}

type SessionPolicy struct {
	MaxTokens int64         `toml:"max_tokens"`
	OnOverrun OverrunAction `toml:"on_overrun"`
}

type ModelRate struct {
	Model                 string
	InputCentsPerMillion  int64
	OutputCentsPerMillion int64
}

type CostEstimate struct {
	Model            string
	InputTokens      int64
	OutputTokens     int64
	InputMicroCents  int64
	OutputMicroCents int64
	TotalMicroCents  int64
}

var modelRates = []ModelRate{
	{Model: "claude-sonnet-4-6", InputCentsPerMillion: 300, OutputCentsPerMillion: 1500},
	{Model: "claude-opus-4-7", InputCentsPerMillion: 500, OutputCentsPerMillion: 2500},
	{Model: "claude-haiku-4-5", InputCentsPerMillion: 100, OutputCentsPerMillion: 500},
}

func DefaultPolicy() Policy {
	return Policy{Session: SessionPolicy{OnOverrun: OverrunInterrupt}}
}

func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return Policy{}, err
	}
	return ParsePolicy(data)
}

func ParsePolicy(data []byte) (Policy, error) {
	p := DefaultPolicy()
	if len(bytes.TrimSpace(data)) == 0 {
		return p, nil
	}
	if err := toml.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	p = p.withDefaults()
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p Policy) Validate() error {
	p = p.withDefaults()
	if p.Global.MaxTokensPerDay < 0 {
		return fmt.Errorf("global.max_tokens_per_day must be >= 0")
	}
	if p.Session.MaxTokens < 0 {
		return fmt.Errorf("session.max_tokens must be >= 0")
	}
	switch p.Session.OnOverrun {
	case OverrunInterrupt, OverrunKill, OverrunWarn:
		return nil
	default:
		return fmt.Errorf("session.on_overrun must be one of interrupt, kill, warn")
	}
}

func PolicyPath(root string) string {
	return filepath.Join(root, ".onibi", "budget.toml")
}

func RateForModel(model string) (ModelRate, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, rate := range modelRates {
		if model == rate.Model {
			return rate, true
		}
	}
	return ModelRate{}, false
}

func SupportedModelRates() []ModelRate {
	return append([]ModelRate(nil), modelRates...)
}

func EstimateCost(model string, inputTokens, outputTokens int64) (CostEstimate, bool) {
	rate, ok := RateForModel(model)
	if !ok || inputTokens < 0 || outputTokens < 0 {
		return CostEstimate{}, false
	}
	if rate.InputCentsPerMillion <= 0 || rate.OutputCentsPerMillion <= 0 {
		return CostEstimate{}, false
	}
	if inputTokens > maxInt64/rate.InputCentsPerMillion || outputTokens > maxInt64/rate.OutputCentsPerMillion {
		return CostEstimate{}, false
	}
	inputMicroCents := inputTokens * rate.InputCentsPerMillion
	outputMicroCents := outputTokens * rate.OutputCentsPerMillion
	if inputMicroCents > maxInt64-outputMicroCents {
		return CostEstimate{}, false
	}
	return CostEstimate{
		Model:            rate.Model,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		InputMicroCents:  inputMicroCents,
		OutputMicroCents: outputMicroCents,
		TotalMicroCents:  inputMicroCents + outputMicroCents,
	}, true
}

func (c CostEstimate) Cents() float64 {
	return float64(c.TotalMicroCents) / float64(MicroCentsPerCent)
}

func (c CostEstimate) USD() float64 {
	return c.Cents() / 100
}

func (p Policy) withDefaults() Policy {
	if p.Session.OnOverrun == "" {
		p.Session.OnOverrun = OverrunInterrupt
	}
	return p
}
