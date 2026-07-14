package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/fleetnode"
	"github.com/gongahkia/onibi/internal/setup"
	sshtransport "github.com/gongahkia/onibi/internal/transport/ssh"
	"github.com/gongahkia/onibi/internal/web"
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
	RestartService(sshtransport.Platform) error
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
var newFleetHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
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
	enroll := &cobra.Command{Use: "enroll <user@host[:port]>", Short: "Enroll an SSH host into an existing fleet hub", Args: cobra.ExactArgs(1), RunE: runSSHEnroll}
	enroll.Flags().String("hub", "", "HTTPS fleet hub URL")
	enroll.Flags().String("owner-session", "", "owner session value; prefer ONIBI_FLEET_OWNER_SESSION")
	enroll.Flags().String("display-name", "", "fleet host display name")
	cmd.AddCommand(enroll)
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

type fleetEnrollmentResponse struct {
	Host     fleet.Host `json:"host"`
	HubProof string     `json:"hub_proof"`
}

func runSSHEnroll(cmd *cobra.Command, args []string) error {
	hub, _ := cmd.Flags().GetString("hub")
	if err := validateFleetHubURL(hub); err != nil {
		return err
	}
	hub = strings.TrimRight(strings.TrimSpace(hub), "/")
	ownerSession, _ := cmd.Flags().GetString("owner-session")
	if ownerSession == "" {
		ownerSession = os.Getenv("ONIBI_FLEET_OWNER_SESSION")
	}
	if strings.TrimSpace(ownerSession) == "" {
		return errors.New("fleet owner session required; set ONIBI_FLEET_OWNER_SESSION")
	}
	displayName, _ := cmd.Flags().GetString("display-name")
	if strings.TrimSpace(displayName) == "" {
		displayName = args[0]
	}
	return withSSHClient(cmd, args[0], func(client sshRemoteClient, platform sshtransport.Platform) error {
		host, err := remoteFleetIdentity(client, args[0], displayName)
		if err != nil {
			return err
		}
		challenge, err := requestFleetChallenge(cmd.Context(), hub, ownerSession, host)
		if err != nil {
			return err
		}
		host.OwnerID = challenge.OwnerID
		host.State = fleet.HostStatePending
		host.RegisteredAt = time.Now().UTC()
		proof, err := remoteFleetProof(client, challenge, host)
		if err != nil {
			return err
		}
		enrolled, err := requestFleetProof(cmd.Context(), hub, proof)
		if err != nil {
			return err
		}
		if enrolled.Host.State != fleet.HostStateActive || enrolled.Host.ID != host.ID || enrolled.Host.OwnerID != challenge.OwnerID {
			return errors.New("invalid fleet enrollment response")
		}
		enrollment := fleetnode.Enrollment{HubURL: strings.TrimSpace(hub), Challenge: challenge, Host: enrolled.Host, HubProof: enrolled.HubProof}
		if err := remoteFleetConfigure(client, enrollment); err != nil {
			return err
		}
		if err := client.RestartService(platform); err != nil {
			return fmt.Errorf("fleet enrollment persisted but remote service restart failed: %w", err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Enrolled SSH fleet host %s\n", enrolled.Host.ID)
		return err
	})
}

func remoteFleetIdentity(client sshRemoteClient, endpoint, displayName string) (fleet.Host, error) {
	out, err := client.RunOutput("$HOME/.local/bin/onibi fleet identity --endpoint " + shellArg(endpoint) + " --display-name " + shellArg(displayName))
	if err != nil {
		return fleet.Host{}, err
	}
	var host fleet.Host
	if err := json.Unmarshal([]byte(out), &host); err != nil {
		return fleet.Host{}, errors.New("ssh: invalid remote fleet identity")
	}
	return host, nil
}

func remoteFleetProof(client sshRemoteClient, challenge fleet.EnrollmentChallenge, host fleet.Host) (fleet.EnrollmentProof, error) {
	challengeText, err := encodeFleetValue(challenge)
	if err != nil {
		return fleet.EnrollmentProof{}, err
	}
	hostText, err := encodeFleetValue(host)
	if err != nil {
		return fleet.EnrollmentProof{}, err
	}
	out, err := client.RunOutput("$HOME/.local/bin/onibi fleet proof --challenge " + shellArg(challengeText) + " --host " + shellArg(hostText))
	if err != nil {
		return fleet.EnrollmentProof{}, err
	}
	var proof fleet.EnrollmentProof
	if err := json.Unmarshal([]byte(out), &proof); err != nil || proof.Validate() != nil {
		return fleet.EnrollmentProof{}, errors.New("ssh: invalid remote fleet proof")
	}
	return proof, nil
}

func remoteFleetConfigure(client sshRemoteClient, enrollment fleetnode.Enrollment) error {
	encoded, err := encodeFleetValue(enrollment)
	if err != nil {
		return err
	}
	_, err = client.RunOutput("$HOME/.local/bin/onibi fleet configure --enrollment " + shellArg(encoded))
	return err
}

func encodeFleetValue(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validateFleetHubURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("fleet hub must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

func requestFleetChallenge(ctx context.Context, hub, ownerSession string, host fleet.Host) (fleet.EnrollmentChallenge, error) {
	var challenge fleet.EnrollmentChallenge
	if err := fleetPOST(ctx, hub+"/fleet/enroll/challenge", ownerSession, web.CSRFTokenForSession(ownerSession), struct {
		Host fleet.Host `json:"host"`
	}{Host: host}, &challenge); err != nil {
		return fleet.EnrollmentChallenge{}, err
	}
	if err := challenge.Validate(); err != nil {
		return fleet.EnrollmentChallenge{}, errors.New("invalid fleet enrollment challenge")
	}
	return challenge, nil
}

func requestFleetProof(ctx context.Context, hub string, proof fleet.EnrollmentProof) (fleetEnrollmentResponse, error) {
	var response fleetEnrollmentResponse
	if err := fleetPOST(ctx, hub+"/fleet/enroll/proof", "", "", proof, &response); err != nil {
		return fleetEnrollmentResponse{}, err
	}
	return response, nil
}

func fleetPOST(ctx context.Context, endpoint, session, csrf string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: web.OwnerCookieName, Value: session})
		req.Header.Set("X-Onibi-CSRF", csrf)
	}
	client := newFleetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet enrollment request failed: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
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
