package doctor

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const securityRedaction = "[REDACTED]"

type SecurityReport struct {
	Status       Status            `json:"status"`
	LogDir       string            `json:"log_dir"`
	ScannedFiles int               `json:"scanned_files"`
	Findings     []SecurityFinding `json:"findings,omitempty"`
	Errors       []string          `json:"errors,omitempty"`
}

func (r SecurityReport) Failed() bool {
	return r.Status == Fail
}

type SecurityFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Pattern string `json:"pattern"`
	Snippet string `json:"snippet"`
}

type securityPattern struct {
	name string
	re   *regexp.Regexp
}

var securityPatterns = []securityPattern{
	{name: "github_pat", re: regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{name: "github_server", re: regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`)},
	{name: "github_oauth", re: regexp.MustCompile(`gho_[A-Za-z0-9]{36}`)},
	{name: "openai", re: regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{name: "anthropic", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9-]{30,}`)},
	{name: "xox_token", re: regexp.MustCompile(`xox[abpr]-[A-Za-z0-9-]{10,}`)},
	{name: "aws_access_key", re: regexp.MustCompile(`AKIA[A-Z0-9]{16}`)},
	{name: "aws_session_key", re: regexp.MustCompile(`ASIA[A-Z0-9]{16}`)},
	{name: "google_api_key", re: regexp.MustCompile(`AIza[A-Za-z0-9_-]{35}`)},
	{name: "huggingface", re: regexp.MustCompile(`hf_[A-Za-z0-9]{34}`)},
	{name: "bearer", re: regexp.MustCompile(`(?i)Bearer[[:space:]]+[A-Za-z0-9._~+/_-]{21,}=*`)},
	{name: "private_key_header", re: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
}

func Security(ctx context.Context, opts Options) SecurityReport {
	report := SecurityReport{Status: Pass, LogDir: opts.Paths.LogDir}
	entries, err := os.ReadDir(opts.Paths.LogDir)
	if errors.Is(err, os.ErrNotExist) {
		return report
	}
	if err != nil {
		report.Status = Warn
		report.Errors = append(report.Errors, err.Error())
		return report
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			report.Status = Warn
			report.Errors = append(report.Errors, ctx.Err().Error())
			return report
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		report.ScannedFiles++
		path := filepath.Join(opts.Paths.LogDir, entry.Name())
		findings, err := scanSecurityLog(path)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			continue
		}
		report.Findings = append(report.Findings, findings...)
	}
	if len(report.Findings) > 0 {
		report.Status = Fail
	} else if len(report.Errors) > 0 {
		report.Status = Warn
	}
	return report
}

func scanSecurityLog(path string) ([]SecurityFinding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var findings []SecurityFinding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := sc.Text()
		for _, pattern := range securityPatterns {
			for _, loc := range pattern.re.FindAllStringIndex(line, -1) {
				findings = append(findings, SecurityFinding{
					File:    path,
					Line:    lineNo,
					Pattern: pattern.name,
					Snippet: securitySnippet(line, loc[0]),
				})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return findings, err
	}
	return findings, nil
}

type securityRange struct {
	start int
	end   int
}

func securitySnippet(line string, around int) string {
	redacted := redactSecurityLine(line)
	if len(redacted) <= 240 {
		return redacted
	}
	start := around - 100
	if start < 0 {
		start = 0
	}
	if start > len(redacted) {
		start = len(redacted)
	}
	end := start + 240
	if end > len(redacted) {
		end = len(redacted)
		start = end - 240
		if start < 0 {
			start = 0
		}
	}
	snippet := redacted[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(redacted) {
		snippet += "..."
	}
	return snippet
}

func redactSecurityLine(line string) string {
	ranges := securityRanges(line)
	if len(ranges) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, r := range ranges {
		if r.start > last {
			b.WriteString(line[last:r.start])
		}
		b.WriteString(securityRedaction)
		last = r.end
	}
	if last < len(line) {
		b.WriteString(line[last:])
	}
	return b.String()
}

func securityRanges(line string) []securityRange {
	var ranges []securityRange
	for _, pattern := range securityPatterns {
		for _, loc := range pattern.re.FindAllStringIndex(line, -1) {
			ranges = append(ranges, securityRange{start: loc[0], end: loc[1]})
		}
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})
	return mergeSecurityRanges(ranges)
}

func mergeSecurityRanges(ranges []securityRange) []securityRange {
	if len(ranges) == 0 {
		return nil
	}
	out := []securityRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &out[len(out)-1]
		if r.start <= last.end {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
