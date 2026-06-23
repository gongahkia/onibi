package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
	"golang.org/x/term"
)

func main() {
	duration := flag.Duration("duration", 5*time.Second, "smoke duration")
	cmdName := flag.String("cmd", "htop", "program to spawn")
	flag.Parse()

	bin, err := exec.LookPath(*cmdName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lookup %s: %v\n", *cmdName, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := pty.Spawn(ctx, pty.SpawnOptions{
		Name: bin,
		Env:  []string{"TERM=xterm-256color"},
		Rows: pty.DefaultRows,
		Cols: pty.DefaultCols,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spawn %s: %v\n", *cmdName, err)
		os.Exit(1)
	}
	defer host.Close()

	subCtx, stopSubs := context.WithCancel(context.Background())
	defer stopSubs()
	_, fast, fastUnsub := host.Subscribe(subCtx, 0)
	defer fastUnsub()
	_, slow, slowUnsub := host.Subscribe(subCtx, 0)
	defer slowUnsub()

	var fastBytes, slowBytes atomic.Uint64
	mirror := term.IsTerminal(int(os.Stdout.Fd()))
	go countFrames(fast, &fastBytes, 0, mirror)
	go countFrames(slow, &slowBytes, 100*time.Millisecond, false)

	time.Sleep(*duration)
	_, _ = host.Write([]byte("q"))
	time.Sleep(200 * time.Millisecond)
	_ = host.Close()
	_ = host.Wait()

	fastTotal := fastBytes.Load()
	slowTotal := slowBytes.Load()
	fmt.Fprintf(os.Stderr, "\nfast_bytes=%d slow_bytes=%d\n", fastTotal, slowTotal)
	if fastTotal == 0 {
		fmt.Fprintln(os.Stderr, "fast subscriber saw no output")
		os.Exit(1)
	}
}

func countFrames(ch <-chan []byte, total *atomic.Uint64, delay time.Duration, mirror bool) {
	for p := range ch {
		total.Add(uint64(len(p)))
		if mirror {
			_, _ = os.Stdout.Write(p)
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}
