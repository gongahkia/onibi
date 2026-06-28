package daemon

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/approval"
)

const (
	DefaultProviderOutputMaxChunks = 8
	DefaultProviderOutputMaxBytes  = 24 * 1024
	DefaultProviderChunkBytes      = 3000
)

var strictSecretRE = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_-]{16,}|xox[abprs]-[A-Za-z0-9-]{16,}|gh[pousr]_[A-Za-z0-9_]{16,}|[A-Za-z0-9_+/=-]{32,})\b`)

type ProviderOutputPolicy struct {
	MaxChunks int
	MaxBytes  int
	Redaction string
}

func (p ProviderOutputPolicy) normalized() ProviderOutputPolicy {
	if p.MaxChunks <= 0 {
		p.MaxChunks = DefaultProviderOutputMaxChunks
	}
	if p.MaxBytes <= 0 {
		p.MaxBytes = DefaultProviderOutputMaxBytes
	}
	if p.Redaction == "" {
		p.Redaction = "default"
	}
	p.Redaction = strings.ToLower(strings.TrimSpace(p.Redaction))
	switch p.Redaction {
	case "default", "strict", "off":
	default:
		p.Redaction = "default"
	}
	return p
}

func (d *Daemon) prepareProviderOutput(s string) string {
	p := ProviderOutputPolicy{}
	if d != nil {
		p = d.ProviderOutput
	}
	return p.apply(s)
}

func (p ProviderOutputPolicy) apply(s string) string {
	p = p.normalized()
	s = p.redact(s)
	limit := p.MaxBytes
	if byChunks := p.MaxChunks * DefaultProviderChunkBytes; byChunks > 0 && byChunks < limit {
		limit = byChunks
	}
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return fmt.Sprintf("%s\n[truncated provider output: sent %d/%d bytes]", cut, len(cut), len(s))
}

func (p ProviderOutputPolicy) redact(s string) string {
	if chatUnredacted() || p.normalized().Redaction == "off" {
		return s
	}
	s = approval.Scrub(s)
	if p.normalized().Redaction == "strict" {
		s = strictSecretRE.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}
