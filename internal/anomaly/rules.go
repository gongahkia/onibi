package anomaly

import (
	"bytes"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	RuleWriteBurst       = "write-burst"
	RuleForkBomb         = "fork-bomb"
	RuleExfilHost        = "exfil-host"
	RuleSecretArgs       = "secret-args"
	RuleReverseShell     = "reverse-shell"
	RuleCurlPipeShell    = "curl-pipe-shell"
	RuleOutsideWorkspace = "outside-workspace-write"
	RuleToolLoop         = "tool-loop"
)

type Action struct {
	SessionID string
	Agent     string
	Tool      string
	InputJSON string
	Command   string
	FilePath  string
	CWD       string
	At        time.Time
	Turn      int
}

type Options struct {
	WorkspaceRoot    string
	NetworkAllowlist []string
	WriteAllowlist   []string
}

type Finding struct {
	RuleName string
	Evidence string
	Action   Action
}

type Rule struct {
	Name        string
	Description string
	Evaluate    func(Context, Action) (Finding, bool)
}

type Context struct {
	History         []Action
	Options         Options
	WriteBurstCount int
	ToolLoopCount   int
}

var Rules = []Rule{
	{Name: RuleWriteBurst, Description: "more than 20 file writes within 60s", Evaluate: evalWriteBurst},
	{Name: RuleForkBomb, Description: "classic shell fork bomb", Evaluate: evalForkBomb},
	{Name: RuleExfilHost, Description: "network command targets a non-allowlisted host", Evaluate: evalExfilHost},
	{Name: RuleSecretArgs, Description: "secret pattern appears in tool args", Evaluate: evalSecretArgs},
	{Name: RuleReverseShell, Description: "reverse shell command", Evaluate: evalReverseShell},
	{Name: RuleCurlPipeShell, Description: "curl piped into shell", Evaluate: evalCurlPipeShell},
	{Name: RuleOutsideWorkspace, Description: "write targets path outside workspace", Evaluate: evalOutsideWorkspaceWrite},
	{Name: RuleToolLoop, Description: "same tool and args repeated more than 5 times within 20 turns", Evaluate: evalToolLoop},
}

var (
	forkBombRE      = regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;?\s*:`)
	awsKeyRE        = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	githubTokenRE   = regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)
	openAITokenRE   = regexp.MustCompile(`sk-[A-Za-z0-9]{48}`)
	pemHeaderRE     = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	reverseBashRE   = regexp.MustCompile(`(?is)\bbash\s+-i\b.*>&.*(?:/dev/)?tcp/`)
	reverseNCRE     = regexp.MustCompile(`(?is)\b(?:nc|netcat)\b.*\s-e\s`)
	curlPipeShellRE = regexp.MustCompile(`(?is)\bcurl\b[^|]+\|[^|]*\b(?:sh|bash)\b`)
	urlRE           = regexp.MustCompile(`(?i)\b(?:https?|s?ftp)://[^\s"'<>]+`)
	scpHostRE       = regexp.MustCompile(`(?:^|\s)(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.-]+):[^\s]+`)
)

func LoadOptions(root string) (Options, error) {
	root = strings.TrimSpace(root)
	opts := Options{WorkspaceRoot: root}
	if root == "" {
		return opts, nil
	}
	var raw struct {
		Allowlist []string `toml:"allowlist"`
		Network   struct {
			Allowlist []string `toml:"allowlist"`
			Hosts     []string `toml:"hosts"`
		} `toml:"network"`
	}
	data, err := os.ReadFile(filepath.Join(root, ".onibi", "network.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return opts, nil
		}
		return Options{}, err
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Options{}, err
	}
	opts.NetworkAllowlist = append(opts.NetworkAllowlist, raw.Allowlist...)
	opts.NetworkAllowlist = append(opts.NetworkAllowlist, raw.Network.Allowlist...)
	opts.NetworkAllowlist = append(opts.NetworkAllowlist, raw.Network.Hosts...)
	return opts, nil
}

func Evaluate(actions []Action, opts Options) []Finding {
	normalized := normalizeActions(actions)
	state := newWindowState()
	var out []Finding
	for i, action := range normalized {
		ctx := state.context(normalized[:i], action, opts)
		for _, rule := range Rules {
			if finding, ok := rule.Evaluate(ctx, action); ok {
				out = append(out, finding)
			}
		}
	}
	return out
}

func EvaluateOne(history []Action, action Action, opts Options) []Finding {
	actions := normalizeActions(append(append([]Action(nil), history...), action))
	current := actions[len(actions)-1]
	state := newWindowState()
	for _, prev := range actions[:len(actions)-1] {
		state.counts(prev)
	}
	ctx := state.context(actions[:len(actions)-1], current, opts)
	var out []Finding
	for _, rule := range Rules {
		if finding, ok := rule.Evaluate(ctx, current); ok {
			out = append(out, finding)
		}
	}
	return out
}

func evalWriteBurst(ctx Context, action Action) (Finding, bool) {
	if !isWriteAction(action) {
		return Finding{}, false
	}
	if ctx.WriteBurstCount <= 20 {
		return Finding{}, false
	}
	return finding(RuleWriteBurst, action, "21+ writes within 60s"), true
}

func evalForkBomb(_ Context, action Action) (Finding, bool) {
	if !forkBombRE.MatchString(actionText(action)) {
		return Finding{}, false
	}
	return finding(RuleForkBomb, action, "fork bomb pattern"), true
}

func evalExfilHost(ctx Context, action Action) (Finding, bool) {
	if !networkTool(action) {
		return Finding{}, false
	}
	for _, host := range hostsIn(actionText(action)) {
		if host != "" && !hostAllowed(host, ctx.Options.NetworkAllowlist) {
			return finding(RuleExfilHost, action, "host="+host), true
		}
	}
	return Finding{}, false
}

func evalSecretArgs(_ Context, action Action) (Finding, bool) {
	text := actionText(action)
	switch {
	case awsKeyRE.MatchString(text):
		return finding(RuleSecretArgs, action, "aws access key"), true
	case githubTokenRE.MatchString(text):
		return finding(RuleSecretArgs, action, "github token"), true
	case openAITokenRE.MatchString(text):
		return finding(RuleSecretArgs, action, "openai token"), true
	case pemHeaderRE.MatchString(text):
		return finding(RuleSecretArgs, action, "private key header"), true
	default:
		return Finding{}, false
	}
}

func evalReverseShell(_ Context, action Action) (Finding, bool) {
	text := actionText(action)
	if reverseBashRE.MatchString(text) || reverseNCRE.MatchString(text) {
		return finding(RuleReverseShell, action, "reverse shell pattern"), true
	}
	return Finding{}, false
}

func evalCurlPipeShell(_ Context, action Action) (Finding, bool) {
	if curlPipeShellRE.MatchString(actionText(action)) {
		return finding(RuleCurlPipeShell, action, "curl piped to shell"), true
	}
	return Finding{}, false
}

func evalOutsideWorkspaceWrite(ctx Context, action Action) (Finding, bool) {
	if !isWriteAction(action) || strings.TrimSpace(action.FilePath) == "" {
		return Finding{}, false
	}
	target := resolvePath(action.CWD, action.FilePath)
	root := strings.TrimSpace(ctx.Options.WorkspaceRoot)
	if root == "" {
		root = strings.TrimSpace(action.CWD)
	}
	if root == "" || pathWithin(target, root) || anyPathWithin(target, ctx.Options.WriteAllowlist) {
		return Finding{}, false
	}
	return finding(RuleOutsideWorkspace, action, "path="+target), true
}

func evalToolLoop(ctx Context, action Action) (Finding, bool) {
	if ctx.ToolLoopCount <= 5 {
		return Finding{}, false
	}
	return finding(RuleToolLoop, action, "same tool+args repeated 6+ times within 20 turns"), true
}

type windowState struct {
	writes *WindowCounter
	loops  *WindowCounter
}

func newWindowState() *windowState {
	return &windowState{
		writes: NewWindowCounter(60*time.Second, 0),
		loops:  NewWindowCounter(0, 20),
	}
}

func (s *windowState) context(history []Action, action Action, opts Options) Context {
	writes, loops := s.counts(action)
	return Context{History: history, Options: opts, WriteBurstCount: writes, ToolLoopCount: loops}
}

func (s *windowState) counts(action Action) (int, int) {
	writes := 0
	if isWriteAction(action) {
		writes = s.writes.Add("write:"+sessionKey(action), action.At, action.Turn)
	}
	loops := s.loops.Add("loop:"+sessionKey(action)+":"+fingerprint(action), action.At, action.Turn)
	return writes, loops
}

func normalizeActions(actions []Action) []Action {
	out := make([]Action, len(actions))
	for i, action := range actions {
		out[i] = normalizeAction(action)
		if out[i].Turn == 0 {
			out[i].Turn = i + 1
		}
	}
	return out
}

func normalizeAction(action Action) Action {
	var input map[string]any
	if strings.TrimSpace(action.InputJSON) != "" {
		_ = json.Unmarshal([]byte(action.InputJSON), &input)
	}
	if action.Command == "" {
		action.Command = firstString(input, "command", "cmd", "shellCommand", "script", "bash")
	}
	if action.FilePath == "" {
		action.FilePath = firstString(input, "file_path", "filePath", "filepath", "path", "notebook_path", "notebookPath", "target", "targetPath")
	}
	return action
}

func finding(rule string, action Action, evidence string) Finding {
	return Finding{RuleName: rule, Evidence: evidence, Action: action}
}

func isWriteAction(action Action) bool {
	switch strings.ToLower(strings.TrimSpace(action.Tool)) {
	case "write", "edit", "multiedit", "notebookedit":
		return true
	}
	return strings.TrimSpace(action.FilePath) != "" && strings.TrimSpace(action.Tool) == ""
}

func networkTool(action Action) bool {
	text := strings.ToLower(action.Command)
	return strings.Contains(text, "curl ") || strings.HasPrefix(text, "curl ") ||
		strings.Contains(text, "wget ") || strings.HasPrefix(text, "wget ") ||
		strings.Contains(text, "scp ") || strings.HasPrefix(text, "scp ") ||
		strings.Contains(text, "rsync ") || strings.HasPrefix(text, "rsync ")
}

func actionText(action Action) string {
	return strings.Join([]string{action.Tool, action.Command, action.FilePath, action.InputJSON}, "\n")
}

func hostsIn(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range urlRE.FindAllString(text, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ".,);]"))
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host != "" && !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	for _, m := range scpHostRE.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		host := strings.ToLower(m[1])
		if host != "" && strings.Contains(host, ".") && !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	return out
}

func hostAllowed(host string, allowlist []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return true
	}
	for _, allowed := range allowlist {
		allowed = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(allowed), "."))
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func resolvePath(cwd, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd = "."
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func anyPathWithin(target string, roots []string) bool {
	for _, root := range roots {
		if pathWithin(target, root) {
			return true
		}
	}
	return false
}

func pathWithin(target, root string) bool {
	target = filepath.Clean(target)
	root = filepath.Clean(strings.TrimSpace(root))
	if target == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func fingerprint(action Action) string {
	input := strings.TrimSpace(action.InputJSON)
	if input != "" {
		var v any
		if json.Unmarshal([]byte(input), &v) == nil {
			b, err := json.Marshal(v)
			if err == nil {
				var out bytes.Buffer
				if json.Compact(&out, b) == nil {
					input = out.String()
				}
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(action.Tool)) + "\x00" + input + "\x00" + strings.TrimSpace(action.Command) + "\x00" + strings.TrimSpace(action.FilePath)
}

func firstString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	for _, key := range keys {
		for got, v := range m {
			if strings.EqualFold(got, key) {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

func sessionKey(action Action) string {
	id := strings.TrimSpace(action.SessionID)
	if id == "" {
		return "_"
	}
	return id
}

func HasRule(findings []Finding, rule string) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool {
		return f.RuleName == rule
	})
}
