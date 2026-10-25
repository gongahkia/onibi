//go:build browserfixture

// Test-only HTTPS host. Control travels over its parent's stdin, never an HTTP
// endpoint. No Ghostty client is constructed and no terminal is touched.
package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
	"golang.org/x/sys/unix"
)

type backend struct {
	mu                         sync.Mutex
	targets                    []ghostty.Target
	actions                    []ghostty.Action
	inventories, captures      int
	version                    string
	unavailable, captureDenied bool
	hold                       chan struct{}
}

func (b *backend) Targets(context.Context) ([]ghostty.Target, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inventories++
	if b.unavailable {
		return nil, ghostty.ErrUnresponsive
	}
	return append([]ghostty.Target{}, b.targets...), nil
}

func (b *backend) Capabilities() ghostty.Capabilities {
	b.mu.Lock()
	defer b.mu.Unlock()
	return ghostty.CapabilitiesForVersion(b.version)
}

func (b *backend) Snapshot(_ context.Context, id string) (ghostty.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.captures++
	if b.captureDenied {
		return ghostty.Snapshot{}, ghostty.ErrCapturePermission
	}
	for _, target := range b.targets {
		if target.ID != id {
			continue
		}
		if !target.Focused {
			return ghostty.Snapshot{}, ghostty.ErrFocusRequired
		}
		return ghostty.Snapshot{TargetID: id, Text: "$ fixture output\n<img src=x onerror=alert(1)>\nNo native terminal was opened.", CapturedAt: time.Now()}, nil
	}
	return ghostty.Snapshot{}, ghostty.ErrStaleTarget
}

func (b *backend) Execute(ctx context.Context, action ghostty.Action) (ghostty.Result, error) {
	b.mu.Lock()
	b.actions = append(b.actions, action)
	hold := b.hold
	b.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ghostty.Result{}, ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	result := ghostty.Result{TargetID: action.TargetID, Message: "Fixture accepted the action."}
	switch action.Kind {
	case "new_tab", "new_window", "split":
		id := fmt.Sprintf("created-%d", len(b.actions))
		window, tab := action.WindowID, "tab-"+id
		if action.Kind == "new_window" {
			window = "window-" + id
		}
		for i := range b.targets {
			if action.Kind == "split" && b.targets[i].ID == action.TargetID {
				window, tab = b.targets[i].WindowID, b.targets[i].TabID
			}
			b.targets[i].Focused = false
		}
		b.targets = append(b.targets, ghostty.Target{ID: id, WindowID: window, TabID: tab, WindowTitle: "Fixture Mac", TabTitle: "New tab", Title: "New shell", Focused: true})
		result.TargetID = id
	case "focus":
		for i := range b.targets {
			b.targets[i].Focused = b.targets[i].ID == action.TargetID
		}
	case "close_pane", "close_tab", "close_window":
		kept := b.targets[:0]
		for _, target := range b.targets {
			remove := action.Kind == "close_pane" && target.ID == action.TargetID || action.Kind == "close_tab" && target.WindowID == action.WindowID && target.TabID == action.TabID || action.Kind == "close_window" && target.WindowID == action.WindowID
			if !remove {
				kept = append(kept, target)
			}
		}
		b.targets = kept
	case "rename_tab", "rename_pane":
		for i := range b.targets {
			if b.targets[i].ID == action.TargetID {
				if action.Kind == "rename_tab" {
					b.targets[i].TabTitle = action.Name
				} else {
					b.targets[i].Title = action.Name
				}
			}
		}
	}
	return result, nil
}

func certificate() tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	must(err)
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Onibi browser fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	must(err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	state := flag.String("state", "", "isolated test state directory")
	port := flag.Int("port", 0, "loopback port, zero allocates one")
	flag.Parse()
	if *state == "" {
		panic("explicit isolated state directory required")
	}
	must(os.MkdirAll(*state, 0700))
	lock, err := os.OpenFile(filepath.Join(*state, "fixture.lock"), os.O_CREATE|os.O_RDWR, 0600)
	must(err)
	defer lock.Close()
	must(unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	db, err := store.Open(filepath.Join(*state, "web.sqlite"))
	must(err)
	defer db.Close()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	must(err)
	defer listener.Close()
	origin := "https://" + listener.Addr().String()
	b := &backend{version: "1.3.1", targets: []ghostty.Target{
		{ID: "pane-work", WindowID: "window-main", TabID: "tab-work", WindowTitle: "Fixture Mac", TabTitle: "Work", Title: "Work shell", Focused: true},
		{ID: "pane-logs", WindowID: "window-main", TabID: "tab-logs", WindowTitle: "Fixture Mac", TabTitle: "Logs", Title: "Server logs"},
	}}
	var offset atomic.Int64
	now := func() time.Time { return time.Now().Add(time.Duration(offset.Load()) * time.Second) }
	app, err := web.New(web.Options{Backend: b, DB: db, PublicOrigin: origin, Assets: web.Assets(), Now: now, Diagnostics: func(context.Context) ghostty.Diagnostics {
		return ghostty.Diagnostics{CheckedAt: now(), Capabilities: b.Capabilities(), Checks: []ghostty.Check{{ID: "process", State: "running", Detail: "Simulated Ghostty; no native automation."}}}
	}})
	must(err)
	server := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		_ = server.Serve(tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate()}}))
	}()
	encoder := json.NewEncoder(os.Stdout)
	must(encoder.Encode(map[string]any{"ready": true, "url": origin, "pairingURL": app.PairingURL()}))
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct {
			ID            int     `json:"id"`
			Op            string  `json:"op"`
			TargetID      string  `json:"targetId"`
			Unavailable   *bool   `json:"unavailable"`
			CaptureDenied *bool   `json:"captureDenied"`
			Version       *string `json:"version"`
			Hold          *bool   `json:"hold"`
			Advance       int64   `json:"advance"`
		}
		must(json.Unmarshal(scanner.Bytes(), &command))
		b.mu.Lock()
		if command.Op == "external_split" {
			for _, target := range b.targets {
				if target.ID == command.TargetID {
					pane := target
					pane.ID, pane.Title, pane.Focused = fmt.Sprintf("external-%d", len(b.targets)), "Local split", false
					b.targets = append(b.targets, pane)
					break
				}
			}
		}
		if command.Unavailable != nil {
			b.unavailable = *command.Unavailable
		}
		if command.CaptureDenied != nil {
			b.captureDenied = *command.CaptureDenied
		}
		if command.Version != nil {
			b.version = *command.Version
		}
		if command.Hold != nil {
			if *command.Hold && b.hold == nil {
				b.hold = make(chan struct{})
			}
			if !*command.Hold && b.hold != nil {
				close(b.hold)
				b.hold = nil
			}
		}
		offset.Add(command.Advance)
		must(encoder.Encode(map[string]any{"id": command.ID, "actions": b.actions, "targets": b.targets, "inventories": b.inventories, "captures": b.captures}))
		b.mu.Unlock()
	}
	must(scanner.Err())
}
