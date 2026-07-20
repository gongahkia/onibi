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

type ProviderOutputOverrides struct {
	Telegram ProviderOutputPolicy
	Matrix   ProviderOutputPolicy
	Slack    ProviderOutputPolicy
	Discord  ProviderOutputPolicy
	Zulip    ProviderOutputPolicy
	IRC      ProviderOutputPolicy
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

func (d *Daemon) prepareProviderOutputFor(provider, s string) string {
	return d.providerOutputPolicy(provider).apply(s)
}

func (d *Daemon) providerOutputPolicy(provider string) ProviderOutputPolicy {
	p := ProviderOutputPolicy{}
	if d != nil {
		p = d.ProviderOutput
		p = p.withOverride(d.ProviderOutputOverrides.forProvider(provider))
	}
	return p.normalized()
}

func (o ProviderOutputOverrides) forProvider(provider string) ProviderOutputPolicy {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "telegram":
		return o.Telegram
	case "matrix":
		return o.Matrix
	case "slack":
		return o.Slack
	case "discord":
		return o.Discord
	case "zulip":
		return o.Zulip
	case "irc":
		return o.IRC
	default:
		return ProviderOutputPolicy{}
	}
}

func (p ProviderOutputPolicy) withOverride(ov ProviderOutputPolicy) ProviderOutputPolicy {
	if ov.MaxChunks > 0 {
		p.MaxChunks = ov.MaxChunks
	}
	if ov.MaxBytes > 0 {
		p.MaxBytes = ov.MaxBytes
	}
	if strings.TrimSpace(ov.Redaction) != "" {
		p.Redaction = ov.Redaction
	}
	return p
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
