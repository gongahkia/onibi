package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
)

func runPing(cmd *cobra.Command, _ []string) error {
	count, _ := cmd.Flags().GetInt("count")
	interval, _ := cmd.Flags().GetDuration("interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	if count <= 0 {
		return errors.New("--count must be > 0")
	}
	if interval < 0 {
		return errors.New("--interval must be >= 0")
	}
	if timeout <= 0 {
		return errors.New("--timeout must be > 0")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	failures := 0
	for i := 0; i < count; i++ {
		if i > 0 {
			if !sleepPingInterval(cmd.Context(), interval) {
				return cmd.Context().Err()
			}
		}
		start := time.Now()
		resp, err := pingSocket(cmd.Context(), paths.Socket, timeout)
		rtt := time.Since(start).Round(time.Millisecond)
		if err != nil {
			failures++
			fmt.Fprintf(cmd.ErrOrStderr(), "ping failed rtt=%s err=%v\n", rtt, err)
			continue
		}
		text := strings.TrimSpace(resp.Text)
		if text == "" {
			text = "pong"
		}
		fields := strings.Fields(text)
		if len(fields) > 0 && fields[0] == "pong" {
			fields = fields[1:]
		}
		suffix := ""
		if len(fields) > 0 {
			suffix = " " + strings.Join(fields, " ")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "pong rtt=%s%s\n", rtt, suffix)
	}
	if failures > 0 {
		return fmt.Errorf("%d/%d ping failed; run `onibi up`", failures, count)
	}
	return nil
}

func pingSocket(ctx context.Context, socket string, timeout time.Duration) (intake.Response, error) {
	resp, err := intake.Request(socket, intake.Event{Type: intake.TypePing}, timeout)
	if err != nil {
		return intake.Response{}, fmt.Errorf("onibi daemon unavailable: %w", err)
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return resp, errors.New(resp.Reason)
	}
	return resp, nil
}

func sleepPingInterval(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
