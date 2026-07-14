package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

func TestWebSessionsAggregatesActiveRows(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db})
	started := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	if err := db.SessionUpsertStart(t.Context(), "s1", "main", "claude", "/tmp/repo", "claude", "pty", "", started); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(t.Context(), "s2", "shell", "shell", "/tmp/other", "zsh", "pty", "", started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.budgetCosts["s1"] = budget.CostEvent{
		SessionID:         "s1",
		Model:             "claude-sonnet-4-6",
		TotalInputTokens:  10,
		TotalOutputTokens: 5,
		TS:                time.Now().UTC(),
	}
	d.mu.Unlock()
	rows, err := d.WebSessions(t.Context(), web.SessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	first := rows[0]
	if first.ID != "s1" || first.Agent != "claude" || first.CWD != "/tmp/repo" || first.PendingApprovalsCount != 1 || first.RecoveryState != fleet.SessionRecoveryHealthy || first.TokensUsed != 15 || first.RoleRequired != "owner" {
		t.Fatalf("first = %#v", first)
	}
	if first.StartedAt == "" || first.LastActivity == "" || first.RecoveryUpdatedAt == "" {
		t.Fatalf("missing times: %#v", first)
	}
}

func TestWebSessionsIncludesTailnetPeers(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ownerID, err := db.FleetOwnerID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-work-mac",
		OwnerID:         ownerID,
		DisplayName:     "Work Mac",
		IdentityPublic:  base64.RawURLEncoding.EncodeToString(public),
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://peer.tail.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		State:           fleet.HostStateActive,
		RegisteredAt:    time.Now().UTC(),
	}
	if err := db.FleetHostUpsert(t.Context(), host); err != nil {
		t.Fatal(err)
	}
	status := `{"BackendState":"Running","Self":{"DNSName":"self.tail.ts.net."},"Peer":{"n1":{"DNSName":"peer.tail.ts.net.","HostName":"work-mac","Online":true},"n2":{"DNSName":"plain.tail.ts.net.","HostName":"no-daemon","Online":true}}}`
	var probed []string
	d := New(Options{
		DB: db,
		TailnetStatus: func(context.Context) ([]byte, error) {
			return []byte(status), nil
		},
		TailnetHealth: func(_ context.Context, url string, got fleet.Host) (bool, error) {
			probed = append(probed, url)
			return url == "https://peer.tail.ts.net/" && got.ID == host.ID, nil
		},
	})
	rows, err := d.WebSessions(t.Context(), web.SessionListOptions{IncludeRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	got := rows[0]
	if !got.Remote || got.PeerName != "work-mac" || got.RemoteURL != "https://peer.tail.ts.net/" || got.RoleRequired != "remote" {
		t.Fatalf("remote row = %#v", got)
	}
	if !slices.Contains(probed, "https://peer.tail.ts.net/") || slices.Contains(probed, "https://plain.tail.ts.net/") {
		t.Fatalf("probed = %#v", probed)
	}
}

func TestTailnetHealthResponseRequiresEnrolledIdentity(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	host := fleet.Host{
		ID:              "host-work-mac",
		OwnerID:         "owner-local",
		DisplayName:     "Work Mac",
		IdentityPublic:  base64.RawURLEncoding.EncodeToString(public),
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://peer.tail.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		State:           fleet.HostStateActive,
		RegisteredAt:    now,
	}
	request := fleet.MeshHealthRequest{Version: fleet.ProtocolVersion, Nonce: "mesh-health-nonce-0001", SentAt: now}
	response := fleet.MeshHealthResponse{Version: fleet.ProtocolVersion, HostID: host.ID, Nonce: request.Nonce, SentAt: now}
	response.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.MeshHealthSigningPayload(request, response)))
	healthy, err := validateTailnetHealthResponse(request, response, host)
	if err != nil || !healthy {
		t.Fatalf("healthy=%v err=%v", healthy, err)
	}
	response.Signature = "invalid"
	healthy, err = validateTailnetHealthResponse(request, response, host)
	if err != nil || healthy {
		t.Fatalf("invalid signature healthy=%v err=%v", healthy, err)
	}
}

func TestWebSessionsIncludeAllActiveSessions(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	alphaRoot := filepath.Join(t.TempDir(), "alpha")
	betaRoot := filepath.Join(t.TempDir(), "beta")
	if err := db.SessionUpsertStart(ctx, "s1", "main", "claude", filepath.Join(alphaRoot, "pkg"), "claude", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(ctx, "s2", "other", "codex", betaRoot, "codex", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db})
	rows, err := d.WebSessions(ctx, web.SessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "s1" || rows[1].ID != "s2" {
		t.Fatalf("rows = %#v", rows)
	}
}
