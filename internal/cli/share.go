package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/setup"
)

const defaultShareMaxViewers = 5

func shareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share <session>",
		Short: "Mint a read-only viewer pairing URL",
		Args:  cobra.ExactArgs(1),
		RunE:  runShare,
	}
	cmd.Flags().Duration("ttl", setup.ViewerPairTokenTTL, "viewer pair token TTL")
	cmd.Flags().Int("max-viewers", defaultShareMaxViewers, "maximum viewer pair claims")
	cmd.Flags().Bool("json", false, "print JSON")
	cmd.Flags().String("host", "", "override host in generated pair URL")
	cmd.Flags().Int("port", 0, "override port in generated pair URL")
	cmd.Flags().Bool("fallbacks", true, "print fallback URLs")
	cmd.Flags().Bool("no-qr", false, "print URL without QR")
	cmd.Flags().Bool("copy", false, "copy primary URL to clipboard with pbcopy")
	return cmd
}

func runShare(cmd *cobra.Command, args []string) error {
	sessionID := strings.TrimSpace(args[0])
	if sessionID == "" {
		return errors.New("session id required")
	}
	paths, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	entry, ok, err := db.Session(cmd.Context(), sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if entry.Ended {
		return fmt.Errorf("session ended: %s", sessionID)
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	ttl, _ := cmd.Flags().GetDuration("ttl")
	if ttl <= 0 {
		return errors.New("ttl must be positive")
	}
	maxViewers, _ := cmd.Flags().GetInt("max-viewers")
	if maxViewers <= 0 {
		return errors.New("max-viewers must be positive")
	}
	port, err := pairPort(cmd, cfg)
	if err != nil {
		return err
	}
	token, err := setup.NewViewerToken(cmd.Context(), db, sessionID, ttl, maxViewers)
	if err != nil {
		return err
	}
	urls := pairURLs(cmd, token, port)
	for i := range urls {
		urls[i] = viewerShareURL(urls[i], sessionID)
	}
	if copyURL, _ := cmd.Flags().GetBool("copy"); copyURL {
		if err := copyToClipboard(urls[0]); err != nil {
			return err
		}
	}
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"url":         urls[0],
			"fallbacks":   urls[1:],
			"session_id":  sessionID,
			"role":        "viewer",
			"expires":     ttl.String(),
			"max_viewers": maxViewers,
		})
	}
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), urls[0])
		return nil
	}
	printCLIHeader(cmd, "Share")
	fmt.Fprintln(cmd.OutOrStdout(), "Read-only viewer URL:", urls[0])
	fmt.Fprintln(cmd.OutOrStdout(), "Session:", sessionID)
	fmt.Fprintln(cmd.OutOrStdout(), "Expires:", ttl.String())
	fmt.Fprintln(cmd.OutOrStdout(), "Max viewers:", maxViewers)
	if fallbacks, _ := cmd.Flags().GetBool("fallbacks"); fallbacks {
		for _, alt := range urls[1:] {
			fmt.Fprintln(cmd.OutOrStdout(), "Fallback:", alt)
		}
	}
	if noQR, _ := cmd.Flags().GetBool("no-qr"); noQR {
		return nil
	}
	return setup.PrintQR(cmd.OutOrStdout(), urls[0])
}

func viewerShareURL(pairURL, sessionID string) string {
	return pairURL + "#/s/" + url.PathEscape(sessionID)
}
