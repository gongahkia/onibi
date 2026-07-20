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

func ngrokCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ngrok",
		Short: "Manage ngrok auth",
		RunE:  runNgrokStatus,
	}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Store an ngrok authtoken",
		RunE:  runNgrokSetup,
	}
	setup.Flags().String("authtoken", "", "ngrok authtoken")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show ngrok setup state",
		RunE:  runNgrokStatus,
	}
	status.Flags().Bool("json", false, "print JSON")
	disable := &cobra.Command{
		Use:   "disable",
		Short: "Remove ngrok authtoken",
		RunE:  runNgrokDisable,
	}
	cmd.AddCommand(setup, status, disable)
	return cmd
}

func runNgrokSetup(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	token, _ := cmd.Flags().GetString("authtoken")
	if strings.TrimSpace(token) == "" {
		token, err = promptNgrokAuthtoken(cmd)
		if err != nil {
			return err
		}
	}
	if !validNgrokAuthtoken(token) {
		return errors.New("ngrok authtoken required")
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := st.Set(webtransport.NgrokSecretAuthtoken, strings.TrimSpace(token)); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Stored ngrok authtoken")
	fmt.Fprintf(cmd.OutOrStdout(), "Use: `onibi up --transport=ngrok`; optional reserved domain: %s.\n", webtransport.NgrokDomainEnv)
	return nil
}

func runNgrokStatus(cmd *cobra.Command, _ []string) error {
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
	_, tokenOK, tokenErr := st.Get(webtransport.NgrokSecretAuthtoken)
	secretBackend := string(st.Backend())
	if envSet(webtransport.NgrokAuthtokenEnv) {
		tokenOK = true
		secretBackend = "env"
		tokenErr = nil
	}
	report := ngrokStatusReport{
		Authtoken:     tokenOK,
		SecretBackend: secretBackend,
		TokenError:    errorString(tokenErr),
		Domain:        envSet(webtransport.NgrokDomainEnv),
		AgentAPI:      envSet(webtransport.NgrokAgentAPIEnv),
	}
	report.Next = ngrokStatusNext(report)
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	style := styleFor(cmd)
	rows := [][]string{
		{"authtoken", style.bool(report.Authtoken), report.SecretBackend},
		{"reserved_domain", style.bool(report.Domain), webtransport.NgrokDomainEnv},
		{"agent_api", style.bool(report.AgentAPI), webtransport.NgrokAgentAPIEnv},
	}
	if report.TokenError != "" {
		rows = append(rows, []string{"token_error", style.status("WARN"), report.TokenError})
	}
	if len(report.Next) > 0 {
		rows = append(rows, []string{"next", style.status("INFO"), strings.Join(report.Next, "; ")})
	}
	return renderTable(cmd.OutOrStdout(), rows)
}

type ngrokStatusReport struct {
	Authtoken     bool     `json:"authtoken"`
	SecretBackend string   `json:"secret_backend"`
	TokenError    string   `json:"token_error,omitempty"`
	Domain        bool     `json:"reserved_domain"`
	AgentAPI      bool     `json:"agent_api"`
	Next          []string `json:"next,omitempty"`
}

func ngrokStatusNext(report ngrokStatusReport) []string {
	if report.Authtoken {
		return nil
	}
	return []string{"onibi ngrok setup"}
}

func runNgrokDisable(cmd *cobra.Command, _ []string) error {
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
	if err := st.Delete(webtransport.NgrokSecretAuthtoken); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "ngrok disabled.")
	return nil
}

func promptNgrokAuthtoken(cmd *cobra.Command) (string, error) {
	if !inputIsTerminal(cmd.InOrStdin()) {
		return "", errors.New("ngrok authtoken required")
	}
	fmt.Fprint(cmd.OutOrStdout(), "Paste ngrok authtoken: ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return "", sc.Err()
	}
	token := strings.TrimSpace(sc.Text())
	if !validNgrokAuthtoken(token) {
		return "", errors.New("ngrok authtoken required")
	}
	return token, nil
}

func validNgrokAuthtoken(token string) bool {
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
