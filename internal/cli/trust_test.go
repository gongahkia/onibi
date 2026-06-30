package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/trust"
)

func TestTrustCLIListRendersRuntimeRules(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTrustRPC(t, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		if ev.Type != intake.TypeTrust || ev.TrustAction != "list" || ev.TrustRoot != root {
			t.Fatalf("event = %#v", ev)
		}
		data, err := json.Marshal(trust.View{Roots: []trust.RootView{{
			Root: root,
			Rules: []trust.RuleView{{
				ID:      "runtime:abc",
				Source:  "runtime",
				Runtime: true,
				Effect:  trust.EffectAutoApprove,
				Expires: "5m",
				Tool:    "Edit",
				Path:    "src/**",
				Agent:   "claude",
			}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return intake.Response{Text: string(data)}, nil
	})
	out, _ := executeRoot(t, "trust", "list", "--color", "never")
	got := out.String()
	for _, want := range []string{"runtime:abc", "runtime", "auto_approve", "tool=Edit path=src/** agent=claude"} {
		if !strings.Contains(got, want) {
			t.Fatalf("out = %q, want %q", got, want)
		}
	}
}

func TestTrustCLIMutationsSendRPC(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var actions []string
	withTrustRPC(t, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		if ev.Type != intake.TypeTrust || ev.TrustRoot != root {
			t.Fatalf("event = %#v", ev)
		}
		actions = append(actions, ev.TrustAction)
		switch ev.TrustAction {
		case "add":
			if ev.Tool != "Edit" || ev.FilePath != "src/**" || ev.Agent != "claude" || ev.Effect != "auto_approve" || ev.Expires != "5m" {
				t.Fatalf("add event = %#v", ev)
			}
			return intake.Response{Text: "added runtime:abc"}, nil
		case "remove":
			if ev.TrustRuleID != "runtime:abc" {
				t.Fatalf("remove event = %#v", ev)
			}
			return intake.Response{Text: "removed runtime:abc"}, nil
		case "reload":
			return intake.Response{Text: "reloaded " + root + "/.onibi/trust.toml"}, nil
		case "persist":
			return intake.Response{Text: "persisted 1 runtime rule(s)"}, nil
		default:
			t.Fatalf("event = %#v", ev)
			return intake.Response{}, nil
		}
	})
	executeRoot(t, "trust", "add", "--tool", "Edit", "--path", "src/**", "--agent", "claude", "--expires", "5m", "--color", "never")
	executeRoot(t, "trust", "remove", "runtime:abc", "--color", "never")
	executeRoot(t, "trust", "reload", "--color", "never")
	executeRoot(t, "trust", "persist", "--color", "never")
	if strings.Join(actions, ",") != "add,remove,reload,persist" {
		t.Fatalf("actions = %#v", actions)
	}
}

func withTrustRPC(t *testing.T, fn func(context.Context, intake.Event) (intake.Response, error)) {
	t.Helper()
	old := trustRPCRequest
	trustRPCRequest = fn
	t.Cleanup(func() { trustRPCRequest = old })
}
