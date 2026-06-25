package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

func pairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Mint a fresh web pairing QR",
		RunE:  runPair,
	}
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func devicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "List paired web devices",
		RunE:  runDevices,
	}
	cmd.Flags().Bool("all", false, "include revoked devices")
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func unpairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpair [device-id]",
		Short: "Revoke a paired web device",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUnpair,
	}
	return cmd
}

func runPair(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	port, err := listenPort(cfg.Web.ListenAddr)
	if err != nil {
		return err
	}
	token, err := setup.NewToken(cmd.Context(), db)
	if err != nil {
		return err
	}
	urls := webPairURLs(token, port, web.LANHosts(), web.PreferredHost())
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"url":       urls[0],
			"fallbacks": urls[1:],
			"expires":   setup.PairTokenTTL.String(),
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Pair Onibi from your phone:")
	fmt.Fprintln(cmd.OutOrStdout(), urls[0])
	for _, alt := range urls[1:] {
		fmt.Fprintln(cmd.OutOrStdout(), "Fallback:", alt)
	}
	return setup.PrintQR(cmd.OutOrStdout(), urls[0])
}

func runDevices(cmd *cobra.Command, _ []string) error {
	_, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	includeRevoked, _ := cmd.Flags().GetBool("all")
	devices, err := db.ListWebSessions(cmd.Context(), includeRevoked)
	if err != nil {
		return err
	}
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(devices)
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "DEVICE ID", "LABEL", "CREATED", "LAST SEEN", "STATE")}
	for _, d := range devices {
		state := style.green("active")
		if d.Revoked {
			state = style.red("revoked")
		}
		table = append(table, []string{
			d.SessionID,
			d.DeviceLabel,
			formatDeviceTime(d.CreatedAt),
			formatDeviceTime(d.LastSeenAt),
			state,
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func runUnpair(cmd *cobra.Command, args []string) error {
	_, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	id := ""
	if len(args) == 1 {
		id = strings.TrimSpace(args[0])
	} else {
		id, err = promptDeviceID(cmd, db)
		if err != nil {
			return err
		}
	}
	if id == "" {
		return errors.New("device id required")
	}
	ok, err := db.RevokeWebSession(cmd.Context(), id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("device not found or already revoked: %s", id)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Revoked %s\n", styleFor(cmd).green("[OK]"), id)
	return nil
}

func openCLIStore() (config.Paths, *store.DB, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return config.Paths{}, nil, err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return config.Paths{}, nil, err
	}
	return paths, db, nil
}

func promptDeviceID(cmd *cobra.Command, db *store.DB) (string, error) {
	devices, err := db.ListWebSessions(context.Background(), false)
	if err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", errors.New("no active devices")
	}
	if !inputIsTerminal(cmd.InOrStdin()) {
		_ = renderDevicesForPicker(cmd, devices)
		return "", errors.New("device id required")
	}
	if err := renderDevicesForPicker(cmd, devices); err != nil {
		return "", err
	}
	fmt.Fprint(cmd.OutOrStdout(), "Device number to revoke: ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return "", sc.Err()
	}
	raw := strings.TrimSpace(sc.Text())
	for i, d := range devices {
		if raw == fmt.Sprintf("%d", i+1) || raw == d.SessionID {
			return d.SessionID, nil
		}
	}
	return "", fmt.Errorf("unknown device selection: %s", raw)
}

func renderDevicesForPicker(cmd *cobra.Command, devices []store.WebSession) error {
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "#", "DEVICE ID", "LABEL", "LAST SEEN")}
	for i, d := range devices {
		table = append(table, []string{
			fmt.Sprintf("%d", i+1),
			d.SessionID,
			d.DeviceLabel,
			formatDeviceTime(d.LastSeenAt),
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func formatDeviceTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
