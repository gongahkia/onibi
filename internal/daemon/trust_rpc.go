package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/trust"
)

func (d *Daemon) handleTrustRPC(ctx context.Context, ev intake.Event) (intake.Response, error) {
	if d == nil || d.Trust == nil {
		return intake.Response{}, errors.New("trust watcher unavailable")
	}
	root, err := trustRPCRoot(ev.TrustRoot)
	if err != nil {
		return intake.Response{}, err
	}
	if err := d.Trust.AddRoot(root); err != nil {
		return intake.Response{}, err
	}
	action := strings.ToLower(strings.TrimSpace(ev.TrustAction))
	switch action {
	case "list":
		view, err := d.Trust.View(root)
		if err != nil {
			return intake.Response{}, err
		}
		data, err := json.Marshal(view)
		if err != nil {
			return intake.Response{}, err
		}
		return intake.Response{Text: string(data)}, nil
	case "add":
		return d.handleTrustAddRPC(ctx, root, ev)
	case "remove":
		id := strings.TrimSpace(ev.TrustRuleID)
		if id == "" {
			return intake.Response{}, errors.New("rule id required")
		}
		removed, err := d.Trust.RemoveRule(root, id)
		if err != nil {
			return intake.Response{}, err
		}
		if !removed {
			return intake.Response{}, errors.New("rule not found")
		}
		d.audit(ctx, "trust.rule.remove", "", "", 0, fmt.Sprintf("root=%s id=%s", root, id))
		return intake.Response{Text: "removed " + id}, nil
	case "reload":
		ev, ok, err := d.Trust.Reload(root)
		if err != nil {
			return intake.Response{}, err
		}
		if !ok || ev.Err != nil {
			if ev.Err != nil {
				return intake.Response{}, ev.Err
			}
			return intake.Response{}, errors.New("trust root not watched")
		}
		d.audit(ctx, "trust.policy.reload_manual", "", "", 0, fmt.Sprintf("root=%s path=%s rules=%d", root, ev.Path, len(ev.Policy.Rules)))
		return intake.Response{Text: fmt.Sprintf("reloaded %s", ev.Path)}, nil
	case "persist":
		n, err := d.Trust.PersistRuntimeRules(root)
		if err != nil {
			return intake.Response{}, err
		}
		d.audit(ctx, "trust.runtime.persist", "", "", 0, fmt.Sprintf("root=%s count=%d", root, n))
		return intake.Response{Text: fmt.Sprintf("persisted %d runtime rule(s)", n)}, nil
	default:
		return intake.Response{}, errors.New("unknown trust action")
	}
}

func (d *Daemon) handleTrustAddRPC(ctx context.Context, root string, ev intake.Event) (intake.Response, error) {
	tool := strings.TrimSpace(ev.Tool)
	path := strings.TrimSpace(ev.FilePath)
	expires := strings.TrimSpace(ev.Expires)
	if tool == "" {
		return intake.Response{}, errors.New("tool required")
	}
	if path == "" {
		return intake.Response{}, errors.New("path required")
	}
	if expires == "" {
		expires = "5m"
	}
	ttl, err := time.ParseDuration(expires)
	if err != nil || ttl <= 0 {
		return intake.Response{}, errors.New("expires must be a positive duration")
	}
	effect, err := trustEffect(ev.Effect)
	if err != nil {
		return intake.Response{}, err
	}
	rule := trust.RuntimeRule(trust.Match{
		Tool:  tool,
		Path:  trustMatchPath(root, path),
		Agent: strings.TrimSpace(ev.Agent),
	}, effect, ttl, time.Now())
	rule.ExpiresRaw = expires
	rule, err = d.Trust.AddRuntimeRuleWithID(root, rule)
	if err != nil {
		return intake.Response{}, err
	}
	d.audit(ctx, "trust.runtime.add", "", "", 0, fmt.Sprintf("root=%s id=%s tool=%s path=%s agent=%s effect=%s expires=%s", root, rule.ID, tool, rule.Match.Path, rule.Match.Agent, effect, expires))
	return intake.Response{Text: "added " + rule.ID}, nil
}

func trustRPCRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("root required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("root must be a directory")
	}
	return abs, nil
}

func trustEffect(raw string) (trust.Effect, error) {
	switch trust.Effect(strings.TrimSpace(raw)) {
	case "":
		return trust.EffectAutoApprove, nil
	case trust.EffectAutoApprove:
		return trust.EffectAutoApprove, nil
	case trust.EffectAlwaysPrompt:
		return trust.EffectAlwaysPrompt, nil
	case trust.EffectDeny:
		return trust.EffectDeny, nil
	default:
		return "", fmt.Errorf("invalid effect %q", raw)
	}
}
