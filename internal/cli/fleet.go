package cli

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/fleetnode"
	"github.com/gongahkia/onibi/internal/store"
	fleettransport "github.com/gongahkia/onibi/internal/transport/fleet"
	"github.com/gongahkia/onibi/internal/web"
)

var newFleetHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type fleetEnrollmentResponse struct {
	Host     fleet.Host `json:"host"`
	HubProof string     `json:"hub_proof"`
}

func fleetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "fleet", Short: "Validate fleet enrollment endpoints"}
	endpoint := &cobra.Command{
		Use:   "endpoint <mesh|relay> <address>",
		Short: "Select a fleet enrollment endpoint",
		Args:  cobra.ExactArgs(2),
		RunE:  runFleetEndpoint,
	}
	endpoint.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(endpoint)
	enroll := &cobra.Command{Use: "enroll", Short: "Enroll this host into an existing fleet hub", Args: cobra.NoArgs, RunE: runFleetEnroll}
	enroll.Flags().String("hub", "", "HTTPS fleet hub URL")
	enroll.Flags().String("endpoint", "", "public relay endpoint URL")
	enroll.Flags().String("owner-session", "", "owner session value; prefer ONIBI_FLEET_OWNER_SESSION")
	enroll.Flags().String("display-name", "", "fleet host display name")
	cmd.AddCommand(enroll)
	return cmd
}

func runFleetEndpoint(cmd *cobra.Command, args []string) error {
	plan, err := fleettransport.NewEnrollmentPlan(args[0], args[1], time.Now().UTC())
	if err != nil {
		return err
	}
	endpoint, err := plan.Resolve(time.Now().UTC())
	if err != nil {
		return err
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Version  uint16 `json:"version"`
			Adapter  string `json:"adapter"`
			Endpoint any    `json:"endpoint"`
			Expires  string `json:"expires_at"`
		}{Version: plan.Version, Adapter: string(plan.Adapter), Endpoint: endpoint, Expires: plan.ExpiresAt.Format(time.RFC3339)})
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "adapter: %s\nendpoint: %s\n", plan.Adapter, endpoint.URL)
	return err
}

func runFleetEnroll(cmd *cobra.Command, _ []string) error {
	hub, _ := cmd.Flags().GetString("hub")
	if err := validateFleetHubURL(hub); err != nil {
		return err
	}
	hub = strings.TrimRight(strings.TrimSpace(hub), "/")
	endpointURL, _ := cmd.Flags().GetString("endpoint")
	endpoint := fleet.Endpoint{Kind: fleet.EndpointRelay, URL: strings.TrimSpace(endpointURL)}
	if err := endpoint.Validate(); err != nil {
		return err
	}
	ownerSession, _ := cmd.Flags().GetString("owner-session")
	if ownerSession == "" {
		ownerSession = os.Getenv("ONIBI_FLEET_OWNER_SESSION")
	}
	if strings.TrimSpace(ownerSession) == "" {
		return errors.New("fleet owner session required; set ONIBI_FLEET_OWNER_SESSION")
	}
	displayName, _ := cmd.Flags().GetString("display-name")
	if strings.TrimSpace(displayName) == "" {
		displayName = endpoint.URL
	}
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	identity, err := fleetnode.LoadOrCreateIdentity(cmd.Context(), db)
	if err != nil {
		return err
	}
	host, err := identity.Host(displayName, endpoint, buildinfo.Version, []string{"approval.write", "session.read", "session.write"})
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
	proof, err := identity.Sign(challenge, host)
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
	if _, err := fleetnode.Configure(cmd.Context(), db, identity, fleetnode.Enrollment{HubURL: hub, Challenge: challenge, Host: enrolled.Host, HubProof: enrolled.HubProof}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Enrolled relay fleet host %s; restart onibi up to connect.\n", enrolled.Host.ID)
	return err
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
	resp, err := newFleetHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet enrollment request failed: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}

func localFleetLink(ctx context.Context, db *store.DB) (*daemon.FleetLink, error) {
	config, found, err := fleetnode.LoadConfig(ctx, db)
	if err != nil || !found {
		return nil, err
	}
	identity, err := fleetnode.LoadOrCreateIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	private, err := identity.PrivateKeyBytes()
	if err != nil {
		return nil, err
	}
	hubPublic, err := base64.RawURLEncoding.DecodeString(config.HubPublic)
	if err != nil || len(hubPublic) != ed25519.PublicKeySize {
		return nil, errors.New("invalid fleet hub public key")
	}
	return daemon.NewFleetLink(daemon.FleetLinkOptions{HubURL: config.HubURL, OwnerID: config.OwnerID, HostID: config.HostID, PrivateKey: private, HubPublic: hubPublic, BinaryVersion: config.BinaryVersion, Capabilities: config.Capabilities})
}
