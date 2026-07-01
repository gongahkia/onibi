package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/intake"
)

var demoRPC = sessionRPC

func runDemo(cmd *cobra.Command, args []string) error {
	action, err := selectedActionFlag(cmd, "approval")
	if err != nil {
		return err
	}
	switch action {
	case "approval":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runDemoApproval(cmd, args)
	case "":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runDemoScript(cmd)
	default:
		return showActionHelp(cmd, args, "approval")
	}
}

func runDemoApproval(cmd *cobra.Command, _ []string) error {
	tool, _ := cmd.Flags().GetString("tool")
	inputJSON, _ := cmd.Flags().GetString("input")
	cwd, _ := os.Getwd()
	resp, err := demoRPC(cmd.Context(), intake.Event{
		Type:      intake.TypeDemoApproval,
		Agent:     "demo",
		Tool:      strings.TrimSpace(tool),
		InputJSON: strings.TrimSpace(inputJSON),
		CWD:       cwd,
	})
	if err != nil {
		return err
	}
	if resp.Reason != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "demo approval: %s (%s)\n", resp.Decision, resp.Reason)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "demo approval: %s\n", resp.Decision)
	return nil
}

func runDemoScript(cmd *cobra.Command) error {
	duration, _ := cmd.Flags().GetDuration("duration")
	if duration <= 0 {
		duration = 90 * time.Second
	}
	cwd, _ := os.Getwd()
	resp, err := demoRPC(cmd.Context(), intake.Event{
		Type:  intake.TypeSessionNew,
		Agent: "shell",
		Args:  []string{"bash"},
		Name:  "onibi-demo",
		Mode:  "headless",
		CWD:   cwd,
	})
	if err != nil {
		return err
	}
	sessionID := strings.TrimSpace(resp.SessionID)
	if sessionID == "" {
		return fmt.Errorf("demo session did not return an id")
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "demo session: %s\n", sessionID)
	if strings.TrimSpace(resp.Text) != "" {
		fmt.Fprintln(out, resp.Text)
	}

	pause := duration / 10
	if pause < time.Millisecond {
		pause = time.Millisecond
	}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"intro", func(ctx context.Context) error {
			return demoInput(ctx, sessionID, `echo "Onibi demo: managed bash session"`, true)
		}},
		{"command", func(ctx context.Context) error {
			return demoInput(ctx, sessionID, `sleep 1 && echo "background command complete"`, true)
		}},
		{"file", func(ctx context.Context) error {
			return demoInput(ctx, sessionID, `printf 'Onibi demo file\n' > /tmp/onibi-demo.txt && echo "wrote /tmp/onibi-demo.txt"`, true)
		}},
		{"approval", func(ctx context.Context) error {
			resp, err := demoRPC(ctx, intake.Event{
				Type:      intake.TypeDemoApproval,
				Session:   sessionID,
				Agent:     "demo",
				Tool:      "Bash",
				InputJSON: `{"command":"echo onibi demo approval"}`,
				Action:    "request",
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(resp.Text) != "" {
				fmt.Fprintln(out, resp.Text)
			}
			return nil
		}},
		{"vim", func(ctx context.Context) error {
			if err := demoInput(ctx, sessionID, `if command -v vim >/dev/null 2>&1; then vim /tmp/onibi-demo.txt; else echo "vim not found; skipping vim step"; fi`, true); err != nil {
				return err
			}
			if _, err := exec.LookPath("vim"); err != nil {
				return nil
			}
			if err := demoSleep(ctx, pause); err != nil {
				return err
			}
			return demoInput(ctx, sessionID, "\x1b:q!\n", false)
		}},
		{"handover-mac", func(ctx context.Context) error {
			resp, err := demoRPC(ctx, intake.Event{Type: intake.TypeSessionShow, Session: sessionID})
			if err != nil {
				fmt.Fprintf(out, "handover mac skipped: %v\n", err)
				return nil
			}
			fmt.Fprintln(out, resp.Text)
			return nil
		}},
		{"handover-phone", func(ctx context.Context) error {
			resp, err := demoRPC(ctx, intake.Event{Type: intake.TypeSessionHide, Session: sessionID, Mode: "headless"})
			if err != nil {
				fmt.Fprintf(out, "handover phone skipped: %v\n", err)
				return nil
			}
			fmt.Fprintln(out, resp.Text)
			return nil
		}},
		{"complete", func(ctx context.Context) error {
			return demoInput(ctx, sessionID, `echo "Onibi demo complete"`, true)
		}},
	}
	for _, step := range steps {
		fmt.Fprintf(out, "demo step: %s\n", step.name)
		if err := step.fn(cmd.Context()); err != nil {
			return err
		}
		if err := demoSleep(cmd.Context(), pause); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "demo ready")
	return nil
}

func demoInput(ctx context.Context, sessionID, text string, enter bool) error {
	_, err := demoRPC(ctx, intake.Event{
		Type:    intake.TypeSessionInput,
		Session: sessionID,
		Text:    text,
		Enter:   enter,
	})
	return err
}

func demoSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
