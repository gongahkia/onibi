package daemon

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/telegram"
)

type blockingCommandsBot struct {
	*telegram.Mock
	entered chan struct{}
	once    sync.Once
}

func (b *blockingCommandsBot) SetMyCommands(ctx context.Context, _ *tgbot.SetMyCommandsParams) (bool, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return false, ctx.Err()
}

func TestRunServesSocketBeforeCommandRegistration(t *testing.T) {
	d := newApprovalDaemon(t)
	dir, err := os.MkdirTemp("/tmp", "onibi-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "onibi.sock")
	bot := &blockingCommandsBot{Mock: telegram.NewMock(nil), entered: make(chan struct{})}
	d.Paths.Socket = sock
	d.Bot = bot
	d.Intake = intake.New(sock, d.handleEvent, d.Log)
	d.Intake.SetApprovalHandler(d.handleApprovalRequest)
	d.Intake.SetRPCHandler(d.handleRPCRequest)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	select {
	case <-bot.entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("command registration did not start")
	}
	if !waitForActiveSocket(sock, 2*time.Second) {
		cancel()
		t.Fatal("socket was not ready while command registration was blocked")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRunStartsWebServer(t *testing.T) {
	d := newApprovalDaemon(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")
	addr := freeTCPAddr(t)
	d.Paths.Socket = sock
	d.Paths.StateDir = dir
	d.WebAddr = addr
	d.WebCertDir = filepath.Join(dir, "web")
	d.Bot = telegram.NewMock(nil)
	d.Intake = intake.New(sock, d.handleEvent, d.Log)
	d.Intake.SetApprovalHandler(d.handleApprovalRequest)
	d.Intake.SetRPCHandler(d.handleRPCRequest)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if !waitForHealthz(client, "https://"+addr+"/healthz", 2*time.Second) {
		cancel()
		t.Fatal("web healthz did not become ready")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func waitForActiveSocket(sock string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if intake.SocketActive(sock, 100*time.Millisecond) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitForHealthz(client *http.Client, url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
