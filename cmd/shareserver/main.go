package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"shareserver/internal/app"
	"shareserver/internal/auth"
	"shareserver/internal/config"
	"shareserver/internal/db"
	httpx "shareserver/internal/http"
	"shareserver/internal/share"
	"shareserver/internal/storage"
	"time"
)

// main loads runtime config, opens storage, starts cleanup, and serves HTTP.
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(app.Version)
		return nil
	}
	c := config.Load()
	if len(os.Args) >= 2 && os.Args[1] == "--anubis" {
		execAnubis(c)
		return nil
	}
	d, err := db.Open(c.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			log.Printf("error: close database: %v", err)
		}
	}()
	if err := auth.EnsureAdmin(d, c.AdminUser, c.AdminPassword, c.Dev); err != nil {
		return err
	}
	store := share.NewStore(d)
	integrity := storage.NewIntegrity(c.BlobDir, store)
	a := &app.App{C: c, DB: d, T: template.New(""), Integrity: integrity}
	h := &httpx.Handler{A: a, Store: store, Integrity: integrity}
	h.StartCleanup()
	log.Println("shareserver", app.Version, "listening", c.Addr)
	server := &http.Server{
		Addr: c.Addr, Handler: httpx.New(a),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Minute,
	}
	return server.ListenAndServe()
}
