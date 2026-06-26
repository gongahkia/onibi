package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
)

func runNewSession(cmd *cobra.Command, args []string) error {
	visible, _ := cmd.Flags().GetBool("visible")
	headless, _ := cmd.Flags().GetBool("headless")
	if visible && headless {
		return errors.New("--visible and --headless are mutually exclusive")
	}
	mode := "headless"
	if visible {
		mode = "visible"
	}
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := os.Getwd()
	resp, err := sessionRPC(cmd.Context(), intake.Event{
		Type:  intake.TypeSessionNew,
		Mode:  mode,
		Name:  strings.TrimSpace(name),
		Agent: args[0],
		Args:  args[1:],
		CWD:   cwd,
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runShowSession(cmd *cobra.Command, args []string) error {
	session := ""
	if len(args) > 0 {
		session = args[0]
	}
	resp, err := sessionRPC(cmd.Context(), intake.Event{Type: intake.TypeSessionShow, Session: session})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runHideSession(cmd *cobra.Command, args []string) error {
	end, _ := cmd.Flags().GetBool("end")
	headless, _ := cmd.Flags().GetBool("headless")
	if end && headless {
		return errors.New("--end and --headless are mutually exclusive")
	}
	mode := "headless"
	if end {
		mode = "end"
	} else if !headless && stdinIsTerminal() {
		fmt.Fprint(cmd.OutOrStdout(), "End session instead of continuing headless? [y/N] ")
		line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if strings.EqualFold(strings.TrimSpace(line), "y") {
			mode = "end"
		}
	}
	session := ""
	if len(args) > 0 {
		session = args[0]
	}
	resp, err := sessionRPC(cmd.Context(), intake.Event{Type: intake.TypeSessionHide, Session: session, Mode: mode})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func sessionRPC(ctx context.Context, ev intake.Event) (intake.Response, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return intake.Response{}, err
	}
	resp, err := intake.Request(paths.Socket, ev, 30*time.Second)
	if err != nil {
		return intake.Response{}, fmt.Errorf("onibi service unavailable; run `onibi up`: %w", err)
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return resp, errors.New(resp.Reason)
	}
	return resp, nil
}

func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
