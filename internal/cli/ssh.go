package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/setup"
	sshtransport "github.com/gongahkia/onibi/internal/transport/ssh"
)

type sshTarget struct {
	User string
	Host string
}

type sshTunnelHandle interface {
	URL() string
	Close() error
}

type sshRemoteClient interface {
	DetectArch() (sshtransport.Platform, error)
	InstallBinaries(sshtransport.Platform, sshtransport.InstallOptions) (sshtransport.InstallResult, error)
	InstallService(sshtransport.Platform, sshtransport.ServiceOptions) error
	StartTunnel(context.Context, sshtransport.TunnelOptions) (sshTunnelHandle, error)
	RunOutput(string) (string, error)
	ServiceStatus(sshtransport.Platform) (string, error)
	Teardown(sshtransport.Platform) error
	Close() error
}

type sshBootstrapTunnel struct {
	tunnel sshTunnelHandle
	client sshRemoteClient
}

type sshBootstrapResult struct {
	PairURL  string
	Platform sshtransport.Platform
}

var readSSHKey = readSSHPrivateKey
var connectSSHClient = func(target sshTarget, key []byte, _ *cobra.Command) (sshRemoteClient, error) {
	client, err := sshtransport.Connect(target.Host, target.User, key)
	if err != nil {
		return nil, err
	}
	return realSSHClient{Client: client}, nil
}

type realSSHClient struct {
	*sshtransport.Client
}

func (c realSSHClient) StartTunnel(ctx context.Context, opts sshtransport.TunnelOptions) (sshTunnelHandle, error) {
	return c.Client.StartTunnel(ctx, opts)
}

func (t sshBootstrapTunnel) URL() string {
	return t.tunnel.URL()
}

func (t sshBootstrapTunnel) Close() error {
	tunnelErr := t.tunnel.Close()
	clientErr := t.client.Close()
	if tunnelErr != nil {
		return tunnelErr
	}
	return clientErr
}

func sshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Manage SSH remote bootstrap",
	}
	addSSHKeyFlag(cmd)
	status := &cobra.Command{
		Use:   "status <user@host[:port]>",
		Short: "Show remote Onibi service status",
		Args:  cobra.ExactArgs(1),
		RunE:  runSSHStatus,
	}
	teardown := &cobra.Command{
		Use:   "teardown <user@host[:port]>",
		Short: "Stop remote Onibi service and remove binaries",
		Args:  cobra.ExactArgs(1),
		RunE:  runSSHTeardown,
	}
	cmd.AddCommand(status, teardown)
	return cmd
}

func addSSHKeyFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String("ssh-key", "", "SSH private key path")
}

func runSSHUp(cmd *cobra.Command, rawTarget string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	res, tunnel, err := bootstrapSSH(ctx, cmd, rawTarget)
	if err != nil {
		return err
	}
	defer tunnel.Close()
	style := styleFor(cmd)
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), res.PairURL)
	} else {
		printCLIHeader(cmd, "Onibi SSH")
		fmt.Fprintln(cmd.OutOrStdout(), style.bold("Pair from phone"))
		fmt.Fprintln(cmd.OutOrStdout(), res.PairURL)
		if noQR, _ := cmd.Flags().GetBool("no-qr"); !noQR {
			if err := setupPrintQR(cmd, res.PairURL); err != nil {
				return err
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SSH tunnel active. Press Ctrl-C to stop.")
	}
	<-ctx.Done()
	return nil
}

func runSSHStatus(cmd *cobra.Command, args []string) error {
	return withSSHClient(cmd, args[0], func(client sshRemoteClient, platform sshtransport.Platform) error {
		out, err := client.ServiceStatus(platform)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), out)
		if !strings.HasSuffix(out, "\n") {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return nil
	})
}

func runSSHTeardown(cmd *cobra.Command, args []string) error {
	return withSSHClient(cmd, args[0], func(client sshRemoteClient, platform sshtransport.Platform) error {
		if err := client.Teardown(platform); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "SSH remote torn down")
		return nil
	})
}

func bootstrapSSH(ctx context.Context, cmd *cobra.Command, rawTarget string) (sshBootstrapResult, sshTunnelHandle, error) {
	target, err := parseSSHTarget(rawTarget)
	if err != nil {
		return sshBootstrapResult{}, nil, err
	}
	key, err := readSSHKey(cmd)
	if err != nil {
		return sshBootstrapResult{}, nil, err
	}
	client, err := connectSSHClient(target, key, cmd)
	if err != nil {
		return sshBootstrapResult{}, nil, err
	}
	closeClient := true
	defer func() {
		if closeClient {
			_ = client.Close()
		}
	}()
	platform, err := client.DetectArch()
	if err != nil {
		return sshBootstrapResult{}, nil, err
	}
	if _, err := client.InstallBinaries(platform, sshtransport.InstallOptions{}); err != nil {
		return sshBootstrapResult{}, nil, err
	}
	if err := client.InstallService(platform, sshtransport.ServiceOptions{}); err != nil {
		return sshBootstrapResult{}, nil, err
	}
	tunnel, err := client.StartTunnel(ctx, sshtransport.TunnelOptions{})
	if err != nil {
		return sshBootstrapResult{}, nil, err
	}
	pairURL, err := mintRemotePairURL(client, tunnel.URL())
	if err != nil {
		_ = tunnel.Close()
		return sshBootstrapResult{}, nil, err
	}
	closeClient = false
	return sshBootstrapResult{PairURL: pairURL, Platform: platform}, sshBootstrapTunnel{tunnel: tunnel, client: client}, nil
}

func withSSHClient(cmd *cobra.Command, rawTarget string, fn func(sshRemoteClient, sshtransport.Platform) error) error {
	target, err := parseSSHTarget(rawTarget)
	if err != nil {
		return err
	}
	key, err := readSSHKey(cmd)
	if err != nil {
		return err
	}
	client, err := connectSSHClient(target, key, cmd)
	if err != nil {
		return err
	}
	defer client.Close()
	platform, err := client.DetectArch()
	if err != nil {
		return err
	}
	return fn(client, platform)
}

func mintRemotePairURL(client sshRemoteClient, tunnelURL string) (string, error) {
	u, err := url.Parse(tunnelURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return "", fmt.Errorf("ssh: invalid tunnel URL port: %q", tunnelURL)
	}
	out, err := client.RunOutput(remotePairCommand(host, port))
	if err != nil {
		return "", err
	}
	pairURL := strings.TrimSpace(out)
	if pairURL == "" {
		return "", errors.New("ssh: remote pair command returned empty URL")
	}
	return pairURL, nil
}

func remotePairCommand(host string, port int) string {
	return `$HOME/.local/bin/onibi pair --host ` + shellArg(host) + ` --port ` + strconv.Itoa(port) + ` --no-qr --quiet`
}

func parseSSHTarget(raw string) (sshTarget, error) {
	raw = strings.TrimSpace(raw)
	userPart, hostPart, ok := strings.Cut(raw, "@")
	if !ok || strings.TrimSpace(userPart) == "" || strings.TrimSpace(hostPart) == "" || strings.Contains(hostPart, "@") {
		return sshTarget{}, errors.New("ssh target must be user@host[:port]")
	}
	return sshTarget{User: strings.TrimSpace(userPart), Host: strings.TrimSpace(hostPart)}, nil
}

func readSSHPrivateKey(cmd *cobra.Command) ([]byte, error) {
	path := ""
	if flag := cmd.Flags().Lookup("ssh-key"); flag != nil {
		path = strings.TrimSpace(flag.Value.String())
	}
	if path == "" {
		if flag := cmd.InheritedFlags().Lookup("ssh-key"); flag != nil {
			path = strings.TrimSpace(flag.Value.String())
		}
	}
	if path == "" {
		var err error
		path, err = defaultSSHKeyPath()
		if err != nil {
			return nil, err
		}
	}
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func defaultSSHKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("ssh private key not found; pass --ssh-key")
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func shellArg(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("/._:-", r))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func setupPrintQR(cmd *cobra.Command, rawurl string) error {
	return setup.PrintQR(cmd.OutOrStdout(), rawurl)
}
