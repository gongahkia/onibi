package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/intake"
)

func runSnapshotTake(cmd *cobra.Command, args []string) error {
	resp, err := snapshotRPC(cmd, intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "take",
		Session:        args[0],
		SnapshotName:   args[1],
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runSnapshotRestore(cmd *cobra.Command, args []string) error {
	resp, err := snapshotRPC(cmd, intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "restore",
		SnapshotName:   args[0],
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runSnapshotFork(cmd *cobra.Command, args []string) error {
	turn, err := parseTurnArg(args[1])
	if err != nil {
		return err
	}
	resp, err := snapshotRPC(cmd, intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "fork",
		SnapshotName:   args[0],
		SnapshotTurn:   turn,
		Text:           args[2],
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runSnapshotsList(cmd *cobra.Command, _ []string) error {
	resp, err := snapshotRPC(cmd, intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "list"})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runSnapshotDelete(cmd *cobra.Command, args []string) error {
	resp, err := snapshotRPC(cmd, intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "delete",
		SnapshotName:   args[0],
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func snapshotRPC(cmd *cobra.Command, ev intake.Event) (intake.Response, error) {
	resp, err := sessionRPC(cmd.Context(), ev)
	if err != nil {
		return intake.Response{}, err
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return resp, errors.New(resp.Reason)
	}
	return resp, nil
}

func parseTurnArg(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "@turn-")
	value = strings.TrimPrefix(value, "turn-")
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad turn %q", raw)
	}
	return n, nil
}
