package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	Support = "event-bridge"
	begin   = "# >>> onibi managed shell hook"
	end     = "# <<< onibi managed shell hook"
)

func Supported() []string { return []string{"zsh", "bash", "fish"} }

func Install(ctx context.Context, db *store.DB, notifyBin, name string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}
	path, block, err := target(name, notifyBin)
	if err != nil {
		return err
	}
	if name == "fish" {
		if err := common.WriteFile(path, []byte(block), 0o600); err != nil {
			return err
		}
	} else if err := mergeBlock(path, block); err != nil {
		return err
	}
	return common.Record(ctx, db, "shell:"+name, path, []byte(block))
}

func Uninstall(ctx context.Context, db *store.DB, name string) error {
	path, _, err := target(name, "/bin/true")
	if err != nil {
		return err
	}
	if name == "fish" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := removeBlock(path); err != nil {
		return err
	}
	return common.DeleteRecord(ctx, db, "shell:"+name, path)
}

func Status(ctx context.Context, db *store.DB, name string) common.Info {
	path, block, err := target(name, "/bin/true")
	agent := "shell:" + name
	if err != nil {
		return common.Info{Name: agent, Support: Support, BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: agent, Support: Support, BundledVersion: common.IntegrationVersion, InstallPath: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			common.MarkNotInstalled(&info)
			return info
		}
		info.Message = err.Error()
		return info
	}
	src := string(b)
	info.Installed = strings.Contains(src, begin) && strings.Contains(src, end)
	if !info.Installed {
		if strings.Contains(src, "onibi-notify") {
			info.Message = "unmanaged onibi-like hook; run onibi install-hooks --shell " + name + " to adopt"
			info.Next = "onibi install-hooks --shell " + name
		} else {
			common.MarkNotInstalled(&info)
		}
		return info
	}
	info.InstalledVersion = common.VersionPtr(installedVersion(src))
	common.ApplyManagedStatus(ctx, db, &info, agent, path, []byte(extractBlock(src, block)), name+" hook installed", "onibi install-hooks --shell "+name)
	return info
}

func VerifyHash(ctx context.Context, db *store.DB, name string) error {
	path, block, err := target(name, "/bin/true")
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	got := extractBlock(string(b), block)
	if !strings.Contains(got, begin) {
		return errors.New("onibi-managed shell hook is missing")
	}
	return common.VerifyRecorded(ctx, db, "shell:"+name, path, []byte(got))
}

func Adopt(ctx context.Context, db *store.DB, name string) error {
	path, block, err := target(name, "/bin/true")
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	got := extractBlock(string(b), block)
	if !strings.Contains(got, begin) {
		return errors.New("onibi-managed shell hook is missing")
	}
	return common.Record(ctx, db, "shell:"+name, path, []byte(got))
}

func target(name, notifyBin string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	switch name {
	case "zsh":
		return filepath.Join(home, ".zshrc"), zshBlock(notifyBin), nil
	case "bash":
		return filepath.Join(home, ".bashrc"), bashBlock(notifyBin), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "conf.d", "onibi.fish"), fishBlock(notifyBin), nil
	default:
		return "", "", fmt.Errorf("unsupported shell %q", name)
	}
}

func mergeBlock(path, block string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	src := removeBlockText(string(b))
	if strings.TrimSpace(src) != "" && !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	src += block
	return common.WriteFile(path, []byte(src), 0o600)
}

func removeBlock(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return common.WriteFile(path, []byte(removeBlockText(string(b))), 0o600)
}

func removeBlockText(src string) string {
	i := strings.Index(src, begin)
	if i < 0 {
		return src
	}
	j := strings.Index(src[i:], end)
	if j < 0 {
		return src
	}
	j = i + j + len(end)
	if j < len(src) && src[j] == '\n' {
		j++
	}
	return src[:i] + src[j:]
}

func extractBlock(src, fallback string) string {
	i := strings.Index(src, begin)
	if i < 0 {
		return fallback
	}
	j := strings.Index(src[i:], end)
	if j < 0 {
		return fallback
	}
	j = i + j + len(end)
	if j < len(src) && src[j] == '\n' {
		j++
	}
	return src[i:j]
}

func installedVersion(src string) string {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "onibi version ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# onibi version "))
		}
	}
	return ""
}

func zshBlock(notifyBin string) string {
	cmd := common.Command(notifyBin, "shell", "shell", "cmd_done", false, "")
	return fmt.Sprintf(`%s
# onibi version %s
_onibi_preexec() { __onibi_cmd="$1"; __onibi_start="${EPOCHREALTIME:-$SECONDS}"; }
_onibi_precmd() {
  local st="$?"
  [[ -z "${__onibi_cmd:-}" || -z "${__onibi_start:-}" ]] && return
  local now="${EPOCHREALTIME:-$SECONDS}"
  local elapsed_ms="$(awk "BEGIN { printf \"%%d\", (($now) - (${__onibi_start})) * 1000 }" 2>/dev/null)"
  [[ -z "$elapsed_ms" ]] && elapsed_ms=0
  local min_ms="${ONIBI_SHELL_MIN_MS:-5000}"
  (( elapsed_ms >= min_ms )) && %s --status "$st" --cmd "$__onibi_cmd" --elapsed-ms "$elapsed_ms"
  unset __onibi_cmd __onibi_start
}
autoload -Uz add-zsh-hook
add-zsh-hook preexec _onibi_preexec
add-zsh-hook precmd _onibi_precmd
%s
`, begin, common.IntegrationVersion, cmd, end)
}

func bashBlock(notifyBin string) string {
	cmd := common.Command(notifyBin, "shell", "shell", "cmd_done", false, "")
	return fmt.Sprintf(`%s
# onibi version %s
__onibi_preexec() { __onibi_cmd="$BASH_COMMAND"; __onibi_start="$(date +%%s%%3N)"; }
__onibi_precmd() {
  local st="$?"
  [[ -z "${__onibi_cmd:-}" || -z "${__onibi_start:-}" ]] && return
  local now="$(date +%%s%%3N)"
  local elapsed_ms="$((now - __onibi_start))"
  local min_ms="${ONIBI_SHELL_MIN_MS:-5000}"
  [[ "$elapsed_ms" -ge "$min_ms" ]] && %s --status "$st" --cmd "$__onibi_cmd" --elapsed-ms "$elapsed_ms"
  unset __onibi_cmd __onibi_start
}
trap '__onibi_preexec' DEBUG
PROMPT_COMMAND="__onibi_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
%s
`, begin, common.IntegrationVersion, cmd, end)
}

func fishBlock(notifyBin string) string {
	cmd := common.Command(notifyBin, "shell", "shell", "cmd_done", false, "")
	return fmt.Sprintf(`%s
# onibi version %s
function __onibi_preexec --on-event fish_preexec
  set -g __onibi_cmd $argv
  set -g __onibi_start (date +%%s%%3N)
end
function __onibi_postexec --on-event fish_postexec
  set -l st $status
  test -z "$__onibi_cmd"; and return
  set -l now (date +%%s%%3N)
  set -l elapsed_ms (math "$now - $__onibi_start")
  set -l min_ms (set -q ONIBI_SHELL_MIN_MS; and echo $ONIBI_SHELL_MIN_MS; or echo 5000)
  if test "$elapsed_ms" -ge "$min_ms"
    %s --status "$st" --cmd "$__onibi_cmd" --elapsed-ms "$elapsed_ms"
  end
  set -e __onibi_cmd
  set -e __onibi_start
end
%s
`, begin, common.IntegrationVersion, cmd, end)
}
