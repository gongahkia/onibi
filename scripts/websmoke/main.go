package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18443", "HTTPS listen address")
	state := flag.String("state", "", "state dir")
	flag.Parse()

	if *state == "" {
		dir, err := os.MkdirTemp("", "onibi-websmoke-*")
		if err != nil {
			log.Fatal(err)
		}
		*state = dir
	}
	db, err := store.OpenEphemeral(filepath.Join(*state, "onibi.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	cert, err := web.GenerateOrLoadCert(filepath.Join(*state, "web"))
	if err != nil {
		log.Fatal(err)
	}
	srv := web.New(web.Options{TLSCert: cert, DB: db})
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "websmoke")
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Printf("health=https://%s/healthz\n", *addr)
	fmt.Printf("ws=wss://%s/ws/pty?token=%s\n", *addr, sessionID)
	fmt.Printf("cookie=%s=%s\n", web.OwnerCookieName, sessionID)
	if err := srv.StartContext(ctx, *addr); err != nil {
		log.Fatal(err)
	}
}
