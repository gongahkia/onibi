package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

func BenchmarkWSPTYThroughput(b *testing.B) {
	total := benchPTYBytes()
	chunkSize := 64 * 1024
	if total < chunkSize {
		chunkSize = total
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	b.Cleanup(cancel)
	host, err := pty.Spawn(ctx, pty.SpawnOptions{Name: "/bin/sh", Args: []string{"-c", "cat"}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = host.Close() })

	db, err := store.OpenEphemeral(filepath.Join(b.TempDir(), "onibi.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	srv := New(Options{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "bench")
	if err != nil {
		b.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	b.Cleanup(ts.Close)

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty?token=" + ownerSessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	c, _, err := websocket.Dial(dialCtx, u, &websocket.DialOptions{
		Subprotocols: []string{ptySubprotocol},
		HTTPHeader:   header,
	})
	dialCancel()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { c.CloseNow() })
	c.SetReadLimit(2 * int64(chunkSize))
	if err := wsjson.Write(context.Background(), c, ptyAttachFrame{Type: "attach", SessionID: "s1"}); err != nil {
		b.Fatal(err)
	}
	host.SeedReplay([]byte("bench-ready"))
	readPTYBenchBytes(b, c, len("bench-ready"))

	payload := make([]byte, chunkSize)
	for i := range payload {
		payload[i] = 'x'
	}
	var measured time.Duration
	b.SetBytes(int64(total))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		remaining := total
		for remaining > 0 {
			n := chunkSize
			if remaining < n {
				n = remaining
			}
			host.SeedReplay(payload[:n])
			readPTYBenchBytes(b, c, n)
			remaining -= n
		}
		measured += time.Since(start)
	}
	b.StopTimer()
	if measured > 0 {
		mbps := float64(total*b.N) / measured.Seconds() / 1024 / 1024
		b.ReportMetric(mbps, "MiB/s")
	}
}

func readPTYBenchBytes(b *testing.B, c *websocket.Conn, want int) {
	b.Helper()
	read := 0
	for read < want {
		readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Second)
		typ, p, err := c.Read(readCtx)
		readCancel()
		if err != nil {
			b.Fatal(err)
		}
		if typ == websocket.MessageBinary || typ == websocket.MessageText {
			read += len(p)
		}
	}
}

func benchPTYBytes() int {
	raw := strings.TrimSpace(os.Getenv("ONIBI_BENCH_PTY_BYTES"))
	if raw == "" {
		return 1 << 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 1 << 20
	}
	return n
}
