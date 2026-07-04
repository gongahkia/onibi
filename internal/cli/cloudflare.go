package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

func cloudflareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloudflare",
		Short: "Manage Cloudflare Tunnel auth",
		RunE:  runCloudflareStatus,
	}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Store a Cloudflare API token",
		RunE:  runCloudflareSetup,
	}
	setup.Flags().String("api-token", "", "Cloudflare API token")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show Cloudflare Tunnel setup state",
		RunE:  runCloudflareStatus,
	}
	status.Flags().Bool("json", false, "print JSON")
	disable := &cobra.Command{
		Use:   "disable",
		Short: "Remove Cloudflare API token",
		RunE:  runCloudflareDisable,
	}
	cmd.AddCommand(setup, status, disable)
	return cmd
}

func runCloudflareSetup(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	token, _ := cmd.Flags().GetString("api-token")
	if strings.TrimSpace(token) == "" {
		token, err = promptCloudflareAPIToken(cmd)
		if err != nil {
			return err
		}
	}
	if !validCloudflareAPIToken(token) {
		return errors.New("cloudflare API token required")
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := st.Set(webtransport.CloudflareSecretAPIToken, strings.TrimSpace(token)); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Stored Cloudflare API token")
	fmt.Fprintf(cmd.OutOrStdout(), "Use: %s, %s, %s, then `onibi up --transport=cloudflare-named`.\n", webtransport.CloudflareAccountIDEnv, webtransport.CloudflareTunnelIDEnv, webtransport.CloudflareHostnameEnv)
	return nil
}

func runCloudflareStatus(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	_, tokenOK, tokenErr := st.Get(webtransport.CloudflareSecretAPIToken)
	envToken := envSet(webtransport.CloudflareAPITokenEnv)
	secretBackend := string(st.Backend())
	if envToken {
		secretBackend = "env"
		tokenErr = nil
	}
	report := cloudflareStatusReport{
		APIToken:      tokenOK || envToken,
		SecretBackend: secretBackend,
		TokenError:    errorString(tokenErr),
		AccountID:     envSet(webtransport.CloudflareAccountIDEnv),
		TunnelID:      envSet(webtransport.CloudflareTunnelIDEnv),
		TunnelName:    envSet(webtransport.CloudflareTunnelNameEnv) || envSet(webtransport.CloudflareTunnelEnv),
		Hostname:      envSet(webtransport.CloudflareHostnameEnv),
	}
	report.Next = cloudflareStatusNext(report)
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	style := styleFor(cmd)
	rows := [][]string{
		{"api_token", style.bool(report.APIToken), report.SecretBackend},
		{"account_id", style.bool(report.AccountID), webtransport.CloudflareAccountIDEnv},
		{"tunnel_id", style.bool(report.TunnelID), webtransport.CloudflareTunnelIDEnv},
		{"tunnel_name", style.bool(report.TunnelName), webtransport.CloudflareTunnelNameEnv},
		{"hostname", style.bool(report.Hostname), webtransport.CloudflareHostnameEnv},
	}
	if report.TokenError != "" {
		rows = append(rows, []string{"token_error", style.status("WARN"), report.TokenError})
	}
	if len(report.Next) > 0 {
		rows = append(rows, []string{"next", style.status("INFO"), strings.Join(report.Next, "; ")})
	}
	return renderTable(cmd.OutOrStdout(), rows)
}

type cloudflareStatusReport struct {
	APIToken      bool     `json:"api_token"`
	SecretBackend string   `json:"secret_backend"`
	TokenError    string   `json:"token_error,omitempty"`
	AccountID     bool     `json:"account_id"`
	TunnelID      bool     `json:"tunnel_id"`
	TunnelName    bool     `json:"tunnel_name"`
	Hostname      bool     `json:"hostname"`
	Next          []string `json:"next,omitempty"`
}

func cloudflareStatusNext(report cloudflareStatusReport) []string {
	var next []string
	if !report.APIToken {
		next = append(next, "onibi cloudflare setup")
	}
	if !report.AccountID {
		next = append(next, "set "+webtransport.CloudflareAccountIDEnv)
	}
	if !report.TunnelID {
		next = append(next, "set "+webtransport.CloudflareTunnelIDEnv)
	}
	if !report.TunnelName {
		next = append(next, "set "+webtransport.CloudflareTunnelNameEnv)
	}
	if !report.Hostname {
		next = append(next, "set "+webtransport.CloudflareHostnameEnv)
	}
	return next
}

func runCloudflareDisable(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := st.Delete(webtransport.CloudflareSecretAPIToken); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Cloudflare disabled.")
	return nil
}

func promptCloudflareAPIToken(cmd *cobra.Command) (string, error) {
	if !inputIsTerminal(cmd.InOrStdin()) {
		return "", errors.New("cloudflare API token required")
	}
	fmt.Fprint(cmd.OutOrStdout(), "Paste Cloudflare API token: ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return "", sc.Err()
	}
	token := strings.TrimSpace(sc.Text())
	if !validCloudflareAPIToken(token) {
		return "", errors.New("cloudflare API token required")
	}
	return token, nil
}

func validCloudflareAPIToken(token string) bool {
	return len(strings.TrimSpace(token)) >= 20
}

func envSet(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
